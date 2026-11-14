# RentFlow — Implementation Plan

## Product scope

Build a working, mobile-first rent collection and tenant-management MVP for Kenyan landlords and property managers. The application has separate Landlord/Agent and Tenant experiences, account-scoped records, tenant ledger and receipts, M-Pesa Daraja collection and reconciliation, SMS provider adapters, operational dashboard, CSV reports, and a Free plan limit of five units. Do not implement subscription billing. Implement the specified backend in Go 1.22+ with chi, PostgreSQL, sqlc, golang-migrate, bcrypt, access/refresh JWTs, and slog; implement the web app with Next.js App Router, TypeScript, Tailwind, shadcn-style components, TanStack Query, React Hook Form and Zod. Include Docker Compose, Make targets, CI, and an installable PWA.

## Implementation approach and material constraints

Keep the explicitly requested stack; do not silently replace PostgreSQL with the platform's managed database (which is MySQL-compatible), or replace the specified email/password/JWT authentication with platform OAuth. The project was initialized with managed Server enabled and managed Database disabled. Its initial Preview port is 3000. Node.js and a Go toolchain are available in the Cloud sandbox; Docker is unavailable, while local PostgreSQL is available for disposable smoke tests.

Use a monorepo with three local Compose services (`api`, `web`, and `postgres`). During development, expose one Preview listener on port 3000 through a small Go HTTP gateway: serve `/api/v1/*` and `/healthz` in the Go API and proxy browser page/assets to the Next.js process on an internal port. Keep API calls relative from the browser so they work through the Preview proxy. PostgreSQL is the sole source of durable application data; migrations are explicit and versioned. Bootstrap Go and a PostgreSQL runtime/toolchain for this Cloud workspace after plan approval, and keep Docker Compose as the reproducible deployment/local-development path.

For a published single-container deployment, build the Go API and Next.js app in a multi-stage Dockerfile and run both behind the same port-3000 gateway. Connect to PostgreSQL through a runtime `DATABASE_URL`; do not put a database or credentials into the image. The Manus-managed database is not suitable for this requested PostgreSQL stack. No external production PostgreSQL URL has been supplied, so production publication is out of scope until the user configures one. Keep the initial project configuration's database capability disabled. Daraja must use sandbox settings only. Store the supplied M-Pesa values only in ignored `backend/.env`; never copy them into plans, examples, code, frontend bundles, Git history, or project configuration. `.env.example` contains placeholders only. The user's future ngrok HTTPS URL remains an operator-supplied callback setting. Require a random `MPESA_CALLBACK_TOKEN` of at least 32 characters on every callback URL outside development, appended as `?token=<same-value>`; fail closed when it is missing. C2B callbacks must match the configured shortcode, while successful STK callback amount and phone must match the stored pending request. Africa's Talking is behind an interface; local development uses a mock because its credentials were not supplied.

## Application design and data flow

The Go API owns authentication, authorization, business rules, PostgreSQL transactions, M-Pesa callbacks and STK requests, SMS dispatch, monthly invoicing, reports, and CSV downloads. The Next.js app owns responsive landlord and tenant interfaces and talks to the API over same-origin relative `/api/v1` paths. A landlord account is the tenant boundary: every landlord-owned query and mutation is scoped to the authenticated landlord. Tenant JWTs are tied to the tenant identity and active lease(s), and may access only their own balance, history, receipts, and payment initiation.

Use migrations for accounts/users/roles, plans, properties, units, tenants, leases, invoices, payments, refresh/reset tokens, and notification records. Model partial payments and credit carry-forward in a transactionally consistent per-tenant ledger. Enforce a unique M-Pesa receipt/transaction code and record callbacks idempotently. Match direct Paybill payments using the unit code account reference; send ambiguous or unmatched payments to an explicit landlord review queue. Provide dated reports from ledger-backed records rather than dashboard-only mock data.

Authentication includes registration, login, refresh, logout, and a real token-based password reset flow whose delivery can be stubbed to a development log. Passwords are bcrypt-hashed; refresh/reset tokens are stored hashed and rotated/expired. Monthly invoice generation is idempotent per lease and billing period, including month-end due dates. M-Pesa code is sandbox-first and environment-driven; implement STK push, STK callback, C2B validation and confirmation endpoints. A callback URL cannot be made reachable to Safaricom until the operator provides an HTTPS tunnel/domain. SMS reminders and receipts use a provider interface, mock locally, and Africa's Talking adapter when its runtime credentials are configured.

## Project structure

```text
backend/
  cmd/api/                 Go entry point, migrations, gateway and scheduler startup
  internal/config/         Runtime configuration from environment variables
  internal/dbgen/          Generated sqlc queries and PostgreSQL models
  internal/httpapi/        Auth, account-scoped CRUD, ledger, callbacks, reports, SMS and scheduler logic
  migrations/              Versioned PostgreSQL up/down migrations
  query/                   sqlc typed-query source
  sqlc.yaml                sqlc generation configuration
  .env                     Local secrets only; gitignored
  .env.example             Placeholder values only
frontend/
  app/                      Next.js App Router pages and route layouts
  components/               Shared UI, auth, role navigation, forms and data screens
  lib/                      API client, ledger calculations, formatters and tests
  public/                   PWA manifest, icon, service worker and route manifest
  package.json              Pinned toolchain and frontend checks
Dockerfile                  Multi-stage production image
docker-compose.yml          api, web and postgres local services
entrypoint.sh               Starts web and API behind the port-3000 gateway
.github/workflows/ci.yml    Backend and frontend CI
Makefile                    Common install, migrate, generate, test, lint and run targets
plan.md                     This approved implementation/design plan
TODO.md                     Outcome tracker created after plan approval
```

## Design direction

Three style possibilities (low-probability options): Kenyan editorial operations (0.0348), soft brutalist ledger (0.0231), or coastal modernist utility (0.0365). Choose **Kenyan editorial operations**: a warm, composed administrative interface with precise financial typography and a distinctive deep-green signal color, avoiding generic fintech gradients and decorative stock art.

- **Design Movement:** Contemporary editorial design for practical financial operations, adapted to a Kenyan property-management context without relying on clichés.
- **Core Principles:** Financial clarity first; dense records remain scannable; actions are explicit and reversible; tenant and landlord views feel related but role-appropriate.
- **Color Philosophy:** Warm paper-like neutrals reduce glare and make long ledgers comfortable; deep forest green conveys settled/paid states and is the signature brand color; muted rust/amber signals due or review states without confusing them with errors; high-contrast ink anchors money and dates.
- **Layout Paradigm:** A narrow persistent left rail on desktop opens into a broad ledger workspace; on mobile the rail becomes a compact bottom navigation and actions remain thumb-reachable. Lead with a time-aware collection summary, then a long-form operational feed and ledger rather than a uniform card grid.
- **Signature Elements:** A fine “rent-flow” rule that links invoice and payment rows; small receipt-stub corners on transaction artifacts; tabular numerals with date and status aligned to a consistent baseline.
- **Interaction Philosophy:** Confirm consequential money actions with amount, unit, and recipient visible; keep filters and row actions close to the records they affect; show pending, successful, unmatched, and failed payment states distinctly.
- **Animation:** Short 140–200 ms fades/slides only for navigation, status changes, and drawers; no animated dashboards or motion that delays a payment response; respect reduced-motion settings.
- **Typography System:** Use a sturdy sans for navigation and body copy paired with a high-legibility editorial serif only for key page headings; use tabular numerals for currency, balances, dates, and collection metrics. Keep labels compact and body text comfortably readable on small screens.
- **Brand Essence:** RentFlow gives Kenyan landlords and property managers a dependable view of rent owed, received, and still needing attention; **clear, steady, accountable**.
- **Brand Voice:** Direct, calm, and specific. Example lines: “KES 84,500 received this month.” “This payment needs a unit reference before it can be allocated.”
- **Wordmark & Logo:** A custom compact mark combining a roofline with a receipt edge and a single flowing ledger stroke; use a purpose-built SVG, not a default-font wordmark.
- **Signature Brand Color:** Deep forest green.

## Build workflow

After approval, create `TODO.md` with the full, distinct source requirements grouped by delivered outcomes. Configure TypeScript and Go diagnostics before the first application-code batch. Install/pin dependencies and bootstrap Go/PostgreSQL tooling; keep the supplied sandbox credentials out of tracked files. Implement the migrations and API/domain behavior, then the responsive role-aware Next.js flows and PWA assets. Add Make targets and CI for Go formatting/lint/tests/build and frontend lint/typecheck/build, using PostgreSQL in integration CI. Build and run the Compose topology where available; run Preview on the configured port 3000 and keep its route manifest synchronized. Resolve diagnostics and run existing checks before delivery. Do not publish until a production PostgreSQL `DATABASE_URL` is available and the callback/tunnel settings are configured; the user has not authorized any live-money mode, and Daraja remains sandbox-only.
