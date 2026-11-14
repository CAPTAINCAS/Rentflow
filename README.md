# RentFlow

RentFlow is a mobile-first rent collection and tenant-management MVP for Kenyan landlords and property managers. It uses a Go/chi API, PostgreSQL, a Next.js App Router frontend, and Safaricom Daraja sandbox support. It is designed as a working product, not a static mockup.

## Stack and layout

- `backend/` — Go API, PostgreSQL migrations, sqlc query sources, auth, ledger, M-Pesa, SMS providers, and scheduled jobs.
- `frontend/` — Next.js, TypeScript, Tailwind CSS, forms/queries, landlord workspace, tenant portal, and PWA assets.
- `docker-compose.yml` — `api`, `web`, and `postgres` services for local development.
- `Dockerfile` / `entrypoint.sh` — single-container build for the managed web host. It exposes the Go gateway on port 3000 and serves the Next.js process behind it.
- `.github/workflows/ci.yml` — Go and frontend lint/static checks, tests, and builds.
- `docs/` — implementation references for external integrations and framework pinning.

## Development

Requirements: Go 1.22+, Node.js 22+, pnpm 10.18.0, PostgreSQL 16+, and Docker Compose v2 for the container workflow.

Copy `backend/.env.example` to `backend/.env`, then set private credentials there only. Keep `backend/.env` out of version control. The included sample values are placeholders; do not use them for live payments. The default SMS provider is `mock`, so local development does not send messages.

For the full Compose topology, run:

```sh
make up
```

The API/gateway is on `http://localhost:3000`; Next.js is an internal web service on port 3001; PostgreSQL is on port 5432. To run services directly, start PostgreSQL, then use separate terminals for `make dev-api` and `make dev-web`. Apply checked-in migrations with `make migrate-up` when the backend startup does not already apply them. Generate Go query code with `make sqlc` after editing sqlc query sources.

Common checks are `make lint`, `make test`, `make build`, or `make ci`. Compose data is stored in the named `rentflow-postgres` volume; `make down` preserves it. Do not remove the volume if it contains records you need.

## M-Pesa and SMS

Daraja defaults to the sandbox. The STK Push and C2B callback URLs must be public HTTPS URLs before Safaricom can reach the application. The included callback URLs are placeholders until an ngrok URL or other HTTPS callback origin is supplied; C2B validation and confirmation URLs must be registered with Daraja after configuring that origin. A successful STK request is not a confirmed payment; the callback is authoritative and deduplicated by M-Pesa transaction code.

The Africa's Talking adapter is environment-configured. Keep `SMS_PROVIDER=mock` for local development. Provider credentials are not committed; see [integration notes](docs/integrations.md) for endpoint details and sandbox caveats.

## Deployment boundary

The application deliberately uses PostgreSQL rather than the platform's MySQL-compatible managed database. The root production Dockerfile builds the Go API and standalone Next.js frontend into one container, reads `DATABASE_URL` at runtime, and serves `/healthz` on port 3000. A production PostgreSQL connection URL and public callback URLs are not part of this repository; publication should wait until those are configured. The application has no billing implementation and does not enable live M-Pesa payments.
