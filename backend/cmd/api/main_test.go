package main

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactDatabaseURLHidesCredentials(t *testing.T) {
	got := redactDatabaseURL("postgres://rentflow:sup3r-secret@db.example.com:5432/rentflow?sslmode=require")
	if strings.Contains(got, "sup3r-secret") {
		t.Fatalf("redactDatabaseURL leaked the password: %s", got)
	}
	if !strings.Contains(got, "db.example.com:5432/rentflow") {
		t.Fatalf("redactDatabaseURL removed too much: %s", got)
	}
	if got := redactDatabaseURL("not a url"); got != "DATABASE_URL" {
		t.Fatalf("redactDatabaseURL(%q) = %q, want DATABASE_URL", "not a url", got)
	}
}

func TestDefaultMigrationsDirPrefersImageLayout(t *testing.T) {
	// Locally there is no /app/migrations, so the relative path used by
	// `go run ./cmd/api` from the backend directory must be returned.
	if got := defaultMigrationsDir(); got != "migrations" {
		t.Logf("defaultMigrationsDir() = %q (image layout present)", got)
	}
}

func TestRunFailsFastOnInvalidConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "short")
	t.Setenv("DATABASE_URL", "postgres://user:pass@db.example.com:5432/rentflow?sslmode=require")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("MPESA_CALLBACK_TOKEN", strings.Repeat("c", 40))

	err := run(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("run() = nil, want a configuration error")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("run() = %v, want a JWT_SECRET error", err)
	}
}
