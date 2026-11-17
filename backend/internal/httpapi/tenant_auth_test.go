package httpapi

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTenantAuthSkippedWithoutDB(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping tenant authorization integration tests")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("db connect: %v", err)
	}
	defer pool.Close()
	// Placeholder: real tenant isolation tests require seeded data and JWT issuance.
	// This test confirms the test database is reachable.
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("db ping: %v", err)
	}
}
