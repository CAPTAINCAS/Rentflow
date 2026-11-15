# RentFlow Go API

Go 1.22+ / chi API and gateway. It serves `/api/v1/*` and `/healthz` on port 3000 and proxies non-API requests to Next.js on port 3001. PostgreSQL is the durable application store; migrations run at startup. `sqlc.yaml` and `query/rentflow.sql` generate typed Go query code, currently used by Free-plan unit-limit enforcement; the remaining transactional handlers use pgx.

## Local run

Configure `DATABASE_URL`, a `JWT_SECRET` of at least 32 characters, and optional runtime settings. `make dev-api` loads ignored `backend/.env`; `.env.example` contains placeholders only. Apply migrations from this directory with `migrate -path migrations -database "$DATABASE_URL" up` if running outside the API entrypoint. Generate typed queries with `sqlc generate`.

Startup validates the whole environment before it binds a port and reports every problem at once, for example a short `JWT_SECRET`, a non-PostgreSQL or `sslmode=disable` `DATABASE_URL` outside development, `COOKIE_SECURE=false` outside development, or a missing `MPESA_CALLBACK_TOKEN`. Database credentials are redacted from logs. Migrations are read from `MIGRATIONS_DIR`, then `/app/migrations`, then `./migrations`.

`WEB_ORIGIN` selects the runtime shape. Leave it empty for an API-only service (the managed deployment where the platform routes `/api/v1/*` here and pages to a separate Next.js deployment); set it to the Next.js origin, such as `http://127.0.0.1:3001`, only for the single-container gateway image that also proxies page requests.

Daraja is sandbox-only at `https://sandbox.safaricom.co.ke`; no live-money mode is implemented. Configure the supplied sandbox values only in ignored `backend/.env`. The STK callback URL must eventually be the operator's public HTTPS origin plus `/api/v1/mpesa/stk-callback`; C2B validation and confirmation URLs must also be public HTTPS before registration. In non-development environments, set a random `MPESA_CALLBACK_TOKEN` of at least 32 characters and append `?token=<same-value>` to all three callback URLs. Callbacks fail closed without this token and C2B callbacks must also match `MPESA_SHORTCODE`; never register an unprotected public callback. The Africa's Talking adapter is optional; `SMS_PROVIDER=mock` logs metadata only and sends nothing.

## Response and security contracts

API success is `{"data": ...}`; errors are `{"error":{"code":"...","message":"..."}}`. Lists are arrays in `data`; CSV endpoints stream CSV attachments. Auth returns a user profile and expiry and sets `rf_access`/`rf_refresh` HttpOnly cookies. Refresh tokens are hashed, single-use and rotated. In Cloud Preview, set `COOKIE_SECURE=true` for `SameSite=None; Secure`; use `false` only for plain local HTTP. Cookie-authenticated mutations require a matching `X-CSRF-Token` from the browser-readable `rf_csrf` cookie.

Every landlord/agent query is scoped to the authenticated `account_id`; tenant endpoints are scoped to the authenticated `tenant_id`. No manager-supplied account ID is trusted. Passwords are bcrypt-hashed. Free accounts have a five-unit limit; no billing is implemented.

## Routes

All routes below are relative to `/api/v1` except `/healthz`.

| Method / path | Access and behavior |
|---|---|
| `POST /auth/register` | Public; `{name,email,phone?,password,role?}`. Creates a landlord account (or an agent account) and signs in. Password must be at least 10 characters. |
| `POST /auth/login` | Public; `{email,password}`. |
| `POST /auth/refresh` | Refresh cookie or `{refreshToken}`; rotates and revokes the prior token. |
| `POST /auth/logout` | Revokes the refresh token and clears cookies. |
| `POST /auth/forgot-password` | Public, privacy-preserving response; reset link is logged in development and sent by SMS when a phone number and provider are available. |
| `POST /auth/reset-password` | Public; `{token,password}`; consumes an unexpired one-use token and revokes sessions. |
| `GET /auth/me` | Authenticated profile and role. |
| `GET, POST /properties`; `GET, PATCH, DELETE /properties/{id}` | Landlord/agent CRUD, account-scoped. |
| `GET, POST /units`; `PATCH, DELETE /units/{id}` | Landlord/agent, account-scoped. Unit codes are at most 12 characters; status is vacant, occupied, or maintenance and must agree with active lease occupancy. Free plan allows five units. |
| `GET, POST /tenants`; `PATCH, DELETE /tenants/{id}` | Managers create a tenant and login together with `{full_name,email,phone?,national_id?,password}`; password is at least 10 characters. Tenants can read their own identity. |
| `GET, POST /leases`; `PATCH /leases/{id}` | Managers create a lease with unit, tenant, start date, rent, due day (1–31, clamped to month end), and deposit. A unit may have only one active lease. |
| `GET /invoices` | Creates missing monthly invoices idempotently; tenants see only their own invoice rows. DTOs include outstanding `balance`. |
| `GET /arrears` | Account- or tenant-scoped overdue invoices with unit code, balance, and days overdue. |
| `GET /dashboard` | Manager response includes `expected_this_month`, `collected_this_month`, `collection_rate`, `arrears_total`, `vacant_units`, and recent completed payments. Tenant response is limited to its own summary. |
| `GET, POST /payments` | Scoped payment history; manager POST records a cash/bank/other payment, allocates it to outstanding invoices, carries excess as tenant credit, and sends a receipt through the configured SMS provider when a phone is available. |
| `GET /payments/unmatched` | Account-linked unmatched queue for managers. |
| `POST /payments/{id}/assign` | Manager body `{unit_id}`; assigns only an unmatched payment in that manager's account to the selected unit's active lease tenant and allocates it oldest-invoice-first. |
| `POST /payments/stk-push` | Tenant or manager; `{invoice_id? ,lease_id?,phone,amount}`. Starts sandbox STK; callback is authoritative. |
| `POST /mpesa/stk-callback` | Daraja callback URL must carry the configured shared token outside development; amount and phone must match a stored pending checkout before allocation, and unknown checkouts are ignored. |
| `POST /mpesa/c2b-validation` | Token-protected outside development; accepts only the configured `MPESA_SHORTCODE`. |
| `POST /mpesa/c2b-confirmation` | Token-protected outside development, shortcode-validated and idempotent by `TransID`; auto-matches only a unique active unit-code reference. |
| `POST /mpesa/c2b/register` | Manager-triggered Daraja sandbox C2B URL registration. |
| `GET /reports/collections.csv?month=YYYY-MM`; `GET /reports/arrears.csv` | Manager-only, account-scoped CSV downloads. |
| `GET /healthz` | Public readiness probe; returns success only when PostgreSQL is reachable. |

C2B references that are ambiguous across landlords, or have no unique active unit match, are stored without an account association to prevent cross-landlord exposure. They cannot appear in a landlord's review queue until the incoming Paybill can be safely mapped to one account; this requires an account-specific Paybill mapping that is not part of the current single-shortcode configuration.

## Reconciliation and reminders

Payments are allocated transactionally oldest-invoice-first, support partial payments, and carry overpayment as tenant credit. A daily billing cycle creates due invoices and sends reminders three days before due, on the due date, and while overdue. Notification records deduplicate by lease, period, kind, and scheduled date; receipts use the same provider interface. SMS provider defaults to `mock`; explicitly set `SMS_PROVIDER=africastalking` to use the configured sandbox adapter, which checks Africa's Talking recipient-level delivery status as well as the HTTP response.
