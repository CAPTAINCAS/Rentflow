SHELL := /bin/sh
DATABASE_URL ?= postgres://rentflow:rentflow@localhost:5432/rentflow?sslmode=disable

.PHONY: install up down dev-api dev-web migrate-up migrate-down sqlc lint test build ci

install:
	corepack prepare pnpm@10.18.0 --activate
	cd frontend && pnpm install

up:
	docker compose up --build

down:
	docker compose down

dev-api:
	cd backend && set -a && . ./.env && set +a && go run ./cmd/api

dev-web:
	cd frontend && pnpm dev --hostname 0.0.0.0 --port 3001

migrate-up:
	cd backend && migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	cd backend && migrate -path migrations -database "$(DATABASE_URL)" down 1

sqlc:
	cd backend && sqlc generate

lint:
	cd backend && test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))" && go vet ./...
	cd frontend && pnpm lint && pnpm typecheck

test:
	cd backend && go test ./...
	cd frontend && pnpm test

build:
	cd backend && go build -o /tmp/rentflow-api ./cmd/api
	cd frontend && pnpm build

ci: lint test build
