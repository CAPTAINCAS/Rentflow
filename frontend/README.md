# RentFlow frontend

Mobile-first Next.js App Router and TypeScript interface for Kenyan landlords/property managers and tenants. Uses Tailwind CSS, TanStack Query, React Hook Form, and Zod. Next listens on port 3001; the Go gateway owns public/Preview port 3000 and proxies page requests. Browser API requests use relative same-origin `/api/v1/*` paths.

## Screens

Public pages: home, sign-in, account registration, forgot password, and reset password. Managers have dashboard, properties, units, tenants, leases, invoice ledger/arrears, payment history, unmatched-payment review, and CSV reports. Tenants see their own invoice balances, payment history/receipts and M-Pesa STK form. Data and CRUD forms are API-backed; no demonstration account or sample financial records are included.

Forms expose relationship selectors for properties, vacant units, tenants, and payment-assignment units. Edit forms omit fields the backend does not patch. The payment review form sends `{unit_id}`; the API verifies an active lease and account ownership. Tenant STK requests send `{invoice_id,phone,amount}`. Completed payments and invoices feed the per-tenant running ledger; pending/unmatched entries are not presented as collected cash.

The API client uses `credentials: include`, reads the browser-readable `rf_csrf` cookie, and sends `X-CSRF-Token` on mutations. Session cookies are HttpOnly and must be `SameSite=None; Secure` in HTTPS Preview; configure plain local HTTP separately with `COOKIE_SECURE=false`.

## Run and verify

With Node 22 and pnpm 10.18.0: `corepack pnpm install`, `corepack pnpm dev`. Available checks are `corepack pnpm lint`, `corepack pnpm typecheck`, `corepack pnpm test`, and `corepack pnpm build`. The app includes a web manifest, app icon, and service worker that caches only the public offline fallback and static assets, never authenticated pages or `/api/` responses. `public/manus-routes.json` lists all application page routes for Preview.
