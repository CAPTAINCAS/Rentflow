# Build, test, and deployment-check commands for RentFlow.
#
# Frontend tooling is pinned to pnpm 10.18.0. Use `corepack pnpm ...` (Node 22+)
# or override PNPM if you keep a local pnpm binary.
SHELL := /bin/sh
DATABASE_URL ?= postgres://rentflow:rentflow@localhost:5432/rentflow?sslmode=disable
PNPM ?= corepack pnpm

.PHONY: help install up down dev-api dev-web migrate-up migrate-down sqlc lint test build ci check-env verify-backend

help:
	@echo "install      Install frontend dependencies with the pinned pnpm"
	@echo "up / down    Start or stop the Docker Compose topology (api, web, postgres)"
	@echo "dev-api      Run the Go API with backend/.env loaded"
	@echo "dev-web      Run the Next.js dev server on port 3001"
	@echo "migrate-up   Apply migrations with the migrate CLI ($$(DATABASE_URL))"
	@echo "migrate-down Roll back the most recent migration"
	@echo "sqlc         Regenerate typed query code"
	@echo "lint         gofmt + go vet + eslint + tsc"
	@echo "test         go test ./... and frontend vitest"
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
	cd backend && go test ./...
	cd frontend && $(PNPM) test

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
