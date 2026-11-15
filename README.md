# RentFlow

RentFlow is a mobile-first rent collection and tenant-management MVP for Kenyan landlords and property managers. It uses a Go/chi API, PostgreSQL, a Next.js App Router frontend, and Safaricom Daraja sandbox support. It is designed as a working product, not a static mockup.

## Stack and layout

- `backend/` — Go API, PostgreSQL migrations, sqlc query sources, auth, ledger, M-Pesa, SMS providers, and scheduled jobs.
- `frontend/` — Next.js, TypeScript, Tailwind CSS, forms/queries, landlord workspace, tenant portal, and PWA assets.
- `docker-compose.yml` — `api`, `web`, and `postgres` services for local development.
- `Dockerfile` / `entrypoint.sh` — single-container build that runs the Go gateway on port 3000 and serves the Next.js process behind it.
- `backend/Dockerfile` — API-only container for platforms that host the frontend separately (Vercel + Railway/Render/Fly).
- `.github/workflows/ci.yml` — Go and frontend lint/static checks, tests, and builds.
- `docs/` — deployment and integration references.

## Deployment shapes

The API supports two shapes, selected by `WEB_ORIGIN`:

| Shape | `WEB_ORIGIN` | Use |
|---|---|---|
| API-only service | empty (default) | Managed hosting where the platform routes `/api/v1/*` to the API and page requests to a separate Next.js deployment (Vercel + Railway/Render/Fly). |
| Single-container gateway | e.g. `http://127.0.0.1:3001` | The bundled `Dockerfile`, where the Go process also reverse-proxies page requests to the Next.js server. |

The Go API is a long-running server (`ListenAndServe`, connection pool, migrations at startup, daily billing scheduler). It is **not** compatible with Vercel Functions, which invoke a per-request Lambda and reject a process that binds a port. See [`docs/vercel-deployment.md`](docs/vercel-deployment.md) for the supported topology, the exact environment variables, and verification steps.

## Development

Requirements: Go 1.22+, Node.js 22+, pnpm 10.18.0, PostgreSQL 16+, and Docker Compose v2 for the container workflow.

Copy `backend/.env.example` to `backend/.env`, then set private credentials there only. Keep `backend/.env` out of version control. The included sample values are placeholders; do not use them for live payments. The default SMS provider is `mock`, so local development does not send messages.

For the full Compose topology, run:

```sh
make up
```

The API/gateway is on `http://localhost:3000`; Next.js is an internal web service on port 3001; PostgreSQL is on port 5432. To run services directly, start PostgreSQL, then use separate terminals for `make dev-api` and `make dev-web`. Apply checked-in migrations with `make migrate-up` when the backend startup does not already apply them. Generate Go query code with `make sqlc` after editing sqlc query sources.

Common checks are `make lint`, `make test`, `make build`, or `make ci`. `make check-env` fails early with a readable message when `JWT_SECRET` or `DATABASE_URL` is missing or malformed, which is the failure mode that previously appeared only in the host's runtime log. Compose data is stored in the named `rentflow-postgres` volume; `make down` preserves it. Do not remove the volume if it contains records you need.

## Configuration contract

`config.Load()` reads the environment and `config.Validate()` returns every problem at once, so startup failures name the variable to fix instead of exiting silently:

- `JWT_SECRET` — at least 32 characters; never hardcoded, never defaulted.
- `DATABASE_URL` — a `postgres://` or `postgresql://` URL with host and database name; `sslmode=require` or stricter outside development.
- `COOKIE_SECURE` — must be `true` outside development.
- `MPESA_CALLBACK_TOKEN` — at least 32 characters outside development; callbacks fail closed without it.
- `WEB_ORIGIN` / `PUBLIC_APP_URL` — absolute http(s) origins when set.
- `ACCESS_TOKEN_TTL` must be shorter than `REFRESH_TOKEN_TTL`.

Database credentials are redacted from startup logs. Migrations run from `MIGRATIONS_DIR`, `/app/migrations` when present, otherwise `./migrations`.

## M-Pesa and SMS

Daraja defaults to the sandbox. The STK Push and C2B callback URLs must be public HTTPS URLs before Safaricom can reach the application. The included callback URLs are placeholders until an ngrok URL or other HTTPS callback origin is supplied; C2B validation and confirmation URLs must be registered with Daraja after configuring that origin. A successful STK request is not a confirmed payment; the callback is authoritative and deduplicated by M-Pesa transaction code.

The Africa's Talking adapter is environment-configured. Keep `SMS_PROVIDER=mock` for local development. Provider credentials are not committed; see [integration notes](docs/integrations.md) for endpoint details and sandbox caveats.

## Deployment boundary

The application deliberately uses PostgreSQL rather than the platform's MySQL-compatible managed database. Two build paths exist: the root `Dockerfile` builds the Go API and standalone Next.js frontend into one container, and `backend/Dockerfile` builds the API alone for a split deployment. Both read `DATABASE_URL` at runtime and serve `/healthz` on the injected `PORT`. A production PostgreSQL connection URL and public callback URLs are not part of this repository; publication should wait until those are configured. The application has no billing implementation and does not enable live M-Pesa payments.
