package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rentflow/backend/internal/config"
	"github.com/rentflow/backend/internal/httpapi"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("RentFlow API cannot start", "error", err.Error())
		os.Exit(1)
	}
}

// run performs startup and blocking server supervision. It returns an error
// instead of calling os.Exit so the failure reason is always logged once and
// the process exits with a non-zero status that hosts can report.
func run(log *slog.Logger) error {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return err
	}

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("DATABASE_URL could not be parsed: %w", err)
	}
	// Serverless and container hosts restart processes frequently; keep idle
	// connections low so PostgreSQL does not accumulate backends.
	poolConfig.MaxConns = 5
	poolConfig.MinConns = 0
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 5 * time.Minute
	poolConfig.HealthCheckPeriod = time.Minute

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStartup()
	pool, err := pgxpool.NewWithConfig(startupCtx, poolConfig)
	if err != nil {
		return fmt.Errorf("database connection pool could not be created: %w", err)
	}
	defer pool.Close()
	if err = pool.Ping(startupCtx); err != nil {
		return fmt.Errorf("database is unreachable at %s: %w", redactDatabaseURL(cfg.DatabaseURL), err)
	}

	if err = runMigrations(cfg.DatabaseURL, log); err != nil {
		return err
	}

	api := httpapi.New(cfg, pool, log)
	if err := api.BootstrapSuperAdmin(); err != nil {
		log.Error("super_admin bootstrap failed", "error", err.Error())
	}
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	go api.RunBillingScheduler(schedulerCtx)

	port := strings.TrimSpace(cfg.Port)
	if port == "" {
		port = "3000"
	}
	// WEB_ORIGIN selects the deployment shape:
	//   * empty  -> API-only service (managed platform routes /api/v1 here and
	//               page requests to the Next.js frontend).
	//   * set    -> single-container gateway that also reverse-proxies page
	//               requests to the Next.js process (Docker/Compose image).
	handler := api.Handler()
	if strings.TrimSpace(cfg.WebOrigin) != "" {
		handler, err = api.ProxyWeb()
		if err != nil {
			return err
		}
	}
	srv := &http.Server{
		Addr:              ":" + strings.TrimPrefix(port, ":"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      40 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("RentFlow gateway listening", "port", port, "env", cfg.Env, "web_origin", cfg.WebOrigin)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)

	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("HTTP server failed: %w", err)
		}
		return nil
	case sig := <-stop:
		log.Info("shutdown requested", "signal", sig.String())
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}
	schedulerCancel()
	log.Info("RentFlow API stopped")
	return nil
}

// runMigrations applies the checked-in PostgreSQL migrations. The directory can
// be overridden for hosts that do not keep the working directory next to the
// binary; the default matches the Docker image layout (/app/migrations).
func runMigrations(databaseURL string, log *slog.Logger) error {
	directory := strings.TrimSpace(os.Getenv("MIGRATIONS_DIR"))
	if directory == "" {
		directory = defaultMigrationsDir()
	}
	source := "file://" + filepath.ToSlash(directory)
	migrator, err := migrate.New(source, databaseURL)
	if err != nil {
		return fmt.Errorf("migration setup failed for %s: %w", directory, err)
	}
	defer func() {
		if sourceErr, dbErr := migrator.Close(); sourceErr != nil || dbErr != nil {
			log.Warn("migration connection close reported an error", "source_error", sourceErr, "database_error", dbErr)
		}
	}()
	if err = migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("database migration failed: %w", err)
	}
	log.Info("database migrations are up to date", "directory", directory)
	return nil
}

func defaultMigrationsDir() string {
	if _, err := os.Stat("/app/migrations"); err == nil {
		return "/app/migrations"
	}
	return "migrations"
}

// redactDatabaseURL keeps credentials out of structured logs.
func redactDatabaseURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return "DATABASE_URL"
	}
	parsed.User = nil
	return parsed.String()
}
