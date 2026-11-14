package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if len(cfg.JWTSecret) < 32 {
		log.Error("JWT_SECRET must be configured with at least 32 characters")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, cfg.DatabaseURL)
	if e != nil {
		log.Error("database configuration failed", "error", e)
		os.Exit(1)
	}
	defer pool.Close()
	if e = pool.Ping(ctx); e != nil {
		log.Error("database unavailable", "error", e)
		os.Exit(1)
	}
	m, e := migrate.New("file://migrations", cfg.DatabaseURL)
	if e != nil {
		log.Error("migration setup failed", "error", e)
		os.Exit(1)
	}
	if e = m.Up(); e != nil && !errors.Is(e, migrate.ErrNoChange) {
		log.Error("database migration failed", "error", e)
		os.Exit(1)
	}
	_, _ = m.Close()
	api := httpapi.New(cfg, pool, log)
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	go api.RunBillingScheduler(schedulerCtx)
	srv := &http.Server{Addr: ":" + strings.TrimPrefix(cfg.Port, ":"), Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		log.Info("RentFlow gateway listening", "port", cfg.Port, "web_origin", cfg.WebOrigin)
		if e := srv.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Error("HTTP server failed", "error", e)
			os.Exit(1)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdown, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	if e := srv.Shutdown(shutdown); e != nil {
		log.Error("graceful shutdown failed", "error", e)
	}
}
