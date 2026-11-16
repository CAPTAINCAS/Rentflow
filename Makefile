# Build, test, and deployment-check commands for RentFlow.
#
# Frontend tooling is pinned to pnpm 10.18.0. Use `corepack pnpm ...` (Node 22+)
# or override PNPM if you keep a local pnpm binary.
SHELL := /bin/sh
DATABASE_URL ?= postgres://rentflow:rentflow@localhost:5432/rentflow?sslmode=disable
# Isolated test database. Tests MUST use this, never DATABASE_URL, so a local
# test run can never touch the development or production database. The name
# matches the CI service (rentflow_test) so local and CI behaviour align.
TEST_DATABASE_URL ?= postgres://rentflow:rentflow@localhost:5432/rentflow_test?sslmode=disable
PNPM ?= corepack pnpm

.PHONY: help install up down dev-api dev-web migrate-up migrate-down sqlc lint test test-db test-integration build ci check-env verify-backend

help:
	@echo "install      Install frontend dependencies with the pinned pnpm"
	@echo "up / down    Start or stop the Docker Compose topology (api, web, postgres)"
	@echo "dev-api      Run the Go API with backend/.env loaded"
	@echo "dev-web      Run the Next.js dev server on port 3001"
	@echo "migrate-up   Apply migrations with the migrate CLI ($$(DATABASE_URL))"
	@echo "migrate-down Roll back the most recent migration"
	@echo "sqlc         Regenerate typed query code"
	@echo "lint         gofmt + go vet + eslint + tsc"
	@echo "test         go test ./... (isolated TEST_DATABASE_URL) and frontend vitest"
	@echo "test-db      Create the isolated test database if it does not exist"
	@echo "test-integration  Run backend integration tests against TEST_DATABASE_URL"
	@echo "build        Build the API binary and the Next.js standalone bundle"
	@echo "ci           lint + test + build"
	@echo "check-env    Fail with a clear message when required API env vars are missing"
	@echo "verify-backend  Print the health check request for a running API"

install:
	cd frontend && $(PNPM) install --frozen-lockfile

up:
	docker compose up --build

down:
	docker compose down

dev-api:
	cd backend && set -a && . ./.env && set +a && go run ./cmd/api

dev-web:
	cd frontend && $(PNPM) dev --hostname 0.0.0.0 --port 3001

migrate-up:
	cd backend && migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	cd backend && migrate -path migrations -database "$(DATABASE_URL)" down 1

sqlc:
	cd backend && sqlc generate

lint:
	cd backend && test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))" && go vet ./...
	cd frontend && $(PNPM) lint && $(PNPM) typecheck

test:
	cd backend && DATABASE_URL="$(TEST_DATABASE_URL)" APP_ENV=test COOKIE_SECURE=false SMS_PROVIDER=mock go test ./...
	cd frontend && $(PNPM) test

# Create the isolated test database. Uses the postgres superuser on the local
# Compose service; safe to run repeatedly. Never points at production.
test-db:
	@psql "$$(TEST_DATABASE_URL)" -c 'SELECT 1' >/dev/null 2>&1 && { echo "test database already exists"; exit 0; } || \
	{ echo "creating test database..."; \
	  createdb -h localhost -U rentflow rentflow_test 2>/dev/null || \
	  psql -h localhost -U rentflow -d postgres -c 'CREATE DATABASE rentflow_test' || \
	  { echo "could not create rentflow_test; start the Compose postgres service first (make up)"; exit 1; }; }

# Integration tests run against the isolated test database only. They are added
# in Phase 1; until then this target is a no-op placeholder.
test-integration:
	@echo "No integration tests yet (added in Phase 1)"
	cd backend && DATABASE_URL="$(TEST_DATABASE_URL)" APP_ENV=test COOKIE_SECURE=false SMS_PROVIDER=mock go test ./... -run TestIntegration -count=1

build:
	cd backend && go build -trimpath -o /tmp/rentflow-api ./cmd/api
	cd frontend && $(PNPM) build

ci: lint test build

# Deployment guard: the API refuses to start without these, so fail early here
# with the same expectations instead of discovering it in a runtime log.
check-env:
	@test -n "$$JWT_SECRET" || { echo "JWT_SECRET is not set (needs at least 32 characters)"; exit 1; }
	@test "$${#JWT_SECRET}" -ge 32 || { echo "JWT_SECRET is shorter than 32 characters"; exit 1; }
	@test -n "$$DATABASE_URL" || { echo "DATABASE_URL is not set"; exit 1; }
	@case "$$DATABASE_URL" in postgres://*|postgresql://*) ;; *) echo "DATABASE_URL must use the postgres:// scheme"; exit 1;; esac
	@echo "API environment looks complete (JWT_SECRET, DATABASE_URL)"

verify-backend:
	@echo "curl -fsS https://YOUR_API_ORIGIN/healthz   # expect {\"status\":\"ok\"}"
	@echo "curl -fsS -o /dev/null -w '%{http_code}\\n' https://YOUR_API_ORIGIN/api/v1/auth/me   # expect 401"
