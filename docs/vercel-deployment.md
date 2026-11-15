# Vercel deployment for RentFlow

## Why the Go backend cannot run on Vercel as-is

The API in [`backend/cmd/api/main.go`](../backend/cmd/api/main.go) is a **long-running HTTP server**: it calls `srv.ListenAndServe()`, holds a `pgxpool` connection pool, runs `golang-migrate` at startup, and starts a 24-hour billing scheduler goroutine.

Vercel's Go runtime does not run long-lived servers. It builds a `vercel.go`-style entry point into a **single AWS Lambda handler** (`func(w http.ResponseWriter, r *http.Request)`) that is invoked per request, then frozen. That produces exactly the failures in the runtime log:

| Log line | Cause |
|---|---|
| `JWT_SECRET must be configured with at least 32 characters` | The old `main()` read `JWT_SECRET` and called `os.Exit(1)` during the Lambda *init/bootstrap* phase. The variable was missing (or shorter than 32 chars) in the Vercel project's environment for that target. |
| `Expected a long-running server process, but the user server exited during startup: exit status 1` | Vercel detected a process that binds a port and stays alive; the process exited with status 1 during bootstrap (the `os.Exit(1)` above, or a failed DB ping/migration). |
| `500 INTERNAL_SERVER_ERROR` / `This page is unavailable. A function needed by this page temporarily failed.` | The function never finished booting, so every route — including the frontend's `/api/v1/*` calls — failed. |

Even with the environment fixed, this architecture is a poor fit for Vercel Functions:

- `ListenAndServe()` is never used; the port binding is what Vercel rejects.
- Connection pools leak across frozen invocations and exhaust PostgreSQL backends.
- `migrate.New(...).Up()` on every cold start races with concurrent instances.
- `RunBillingScheduler` (a 24-hour ticker) never runs in a request-scoped function, so invoices and SMS reminders would silently stop.
- The reverse proxy to Next.js on port 3001 has no equivalent on Vercel, where the frontend is a separate deployment.

**Recommendation:** keep the Go service as a long-running container and deploy it on a platform that runs Docker or a persistent process (Railway, Render, Fly.io, a VPS, or Cloud Run). Deploy the Next.js frontend on Vercel. This repository already contains everything needed for that split; no rewrite of the API is required.

## Supported topology

```
Browser
  ├── https://rentflow.vercel.app          → Next.js (Vercel)
  │       └── /api/v1/*  ─┐
  └───────────────────────┴──► https://rentflow-api.up.railway.app  → Go API + PostgreSQL
```

Two ways to connect them:

**A. Same-origin rewrite (recommended).** Keeps session cookies first-party, so `SameSite=None; Secure` and CSRF behave exactly as in local development. Add to [`frontend/next.config.ts`](../frontend/next.config.ts):

```ts
async rewrites() {
  return [{ source: "/api/v1/:path*", destination: `${process.env.API_ORIGIN}/api/v1/:path*` }];
}
```

Set `API_ORIGIN=https://rentflow-api.up.railway.app` on Vercel and leave `NEXT_PUBLIC_API_BASE_URL` empty.

**B. Direct cross-origin API.** Set `NEXT_PUBLIC_API_BASE_URL=https://rentflow-api.up.railway.app` on Vercel (no trailing slash, no `/api/v1`). The API must then send `Access-Control-Allow-Origin: https://rentflow.vercel.app` and `Access-Control-Allow-Credentials: true`; without those headers the browser blocks the credentialed requests. Option A avoids this entirely.

## Required environment variables

### API service (Railway / Render / Fly / Cloud Run)

| Variable | Required | Notes |
|---|---|---|
| `DATABASE_URL` | yes | Managed PostgreSQL, `sslmode=require` or stricter. The API refuses to start with `sslmode=disable` outside development. |
| `JWT_SECRET` | yes | ≥ 32 characters. `openssl rand -base64 48`. Never commit the real value. |
| `APP_ENV` | yes | `production` — enables fail-closed checks for TLS, cookies and callback tokens. |
| `PORT` | usually injected | The platform sets it; the API listens on it. |
| `COOKIE_SECURE` | yes | `true` in production (`SameSite=None; Secure`). |
| `WEB_ORIGIN` | no | **Leave empty** for an API-only service. Set only for the single-container gateway image. |
| `PUBLIC_APP_URL` | yes | Public frontend origin, e.g. `https://rentflow.vercel.app`. Used for password-reset links. |
| `COOKIE_DOMAIN` | no | Only for a shared parent domain, e.g. `.rentflow.co.ke`. |
| `MIGRATIONS_DIR` | no | Defaults to `/app/migrations` when present, else `./migrations`. |
| `MPESA_CALLBACK_TOKEN` | yes outside development | ≥ 32 random characters; append `?token=<same-value>` to all three Daraja callback URLs. |
| `MPESA_CONSUMER_KEY`, `MPESA_CONSUMER_SECRET`, `MPESA_SHORTCODE`, `MPESA_PASSKEY`, `MPESA_CALLBACK_URL`, `MPESA_C2B_VALIDATION_URL`, `MPESA_C2B_CONFIRMATION_URL` | for M-Pesa | Sandbox values only. |
| `SMS_PROVIDER` | no | `mock` (default) or `africastalking`. |
| `AFRICASTALKING_USERNAME`, `AFRICASTALKING_API_KEY`, `AFRICASTALKING_SENDER_ID`, `AFRICASTALKING_BASE_URL` | with Africa's Talking | Sandbox credentials only. |
| `ACCESS_TOKEN_TTL`, `REFRESH_TOKEN_TTL` | no | Defaults `15m` / `720h`; access TTL must be shorter than refresh TTL. |

See [`backend/.env.example`](../backend/.env.example) for the annotated list.

### Frontend (Vercel)

| Variable | Required | Notes |
|---|---|---|
| `NEXT_PUBLIC_API_BASE_URL` | with option B | Empty for option A. Baked in at build time. |
| `NEXT_PUBLIC_SITE_URL` | no | Public origin of the frontend. |

See [`frontend/.env.example`](../frontend/.env.example).

## Deploying the API (Railway example)

1. Create a PostgreSQL service and copy its connection URL; append `?sslmode=require`.
2. Create a service from this repository:
   - Root directory: `.`
   - Dockerfile path: `Dockerfile` (single container) **or** `backend/Dockerfile` (API only).
   - For an API-only service use `backend/Dockerfile`; its build context is `backend/`, and it copies `migrations/` to `/app/migrations`.
3. Set the environment variables from the table above.
4. Expose the service; Railway/Render inject `PORT` and TLS.
5. Confirm the deploy log contains `RentFlow gateway listening` and `database migrations are up to date`.

Render equivalent: **New → Web Service → Docker**, Dockerfile path `backend/Dockerfile`, Docker context `backend`, health check path `/healthz`.

## Deploying the frontend on Vercel

1. Import the repository; set **Root Directory** to `frontend`.
2. Framework preset: Next.js. Build command `pnpm build` (pnpm 10.18.0 is pinned in `package.json`).
3. Add the environment variables above.
4. Deploy, then verify `/login` renders and a login round-trip sets `rf_access`.

## Verification

```sh
# 1. API process is alive and PostgreSQL is reachable
curl -fsS https://YOUR_API_ORIGIN/healthz
# {"status":"ok"}

# 2. Authentication is enforced (401, not 404/500)
curl -sS -o /dev/null -w '%{http_code}\n' https://YOUR_API_ORIGIN/api/v1/auth/me
# 401

# 3. Registration issues HttpOnly cookies
curl -sS -D - -o /dev/null -X POST https://YOUR_API_ORIGIN/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"name":"Test Landlord","email":"verify-'$RANDOM'@example.com","password":"check-me-please"}' \
  | grep -i '^set-cookie'
# rf_access=...; Path=/api/v1; HttpOnly; Secure; SameSite=None

# 4. Unknown API routes return the JSON error envelope, not an HTML error page
curl -sS https://YOUR_API_ORIGIN/api/v1/nope
# {"error":{"code":"not_found","message":"route not found"}}

# 5. Frontend
curl -sS -o /dev/null -w '%{http_code}\n' https://YOUR_PROJECT.vercel.app/login
# 200
```

In the browser: open DevTools → Network, sign in, and confirm `POST /api/v1/auth/login` returns 200 with `Set-Cookie`, and `/api/v1/dashboard` returns account data rather than an HTML error page.

## Remaining manual configuration

- Provisioning the managed PostgreSQL instance and its credentials.
- Choosing the API host and setting `PUBLIC_APP_URL` / `NEXT_PUBLIC_API_BASE_URL` to match.
- Registering the public Daraja callback URLs (they must be HTTPS and reachable from Safaricom).
- Deciding between the same-origin rewrite (option A) and cross-origin API (option B).
- Rotating `JWT_SECRET` invalidates all existing sessions by design.