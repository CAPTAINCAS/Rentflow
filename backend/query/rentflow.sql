-- name: ListProperties :many
SELECT id, account_id, name, address, description, created_at, updated_at FROM properties WHERE account_id=$1 ORDER BY created_at DESC;

-- name: GetProperty :one
SELECT id, account_id, name, address, description, created_at, updated_at FROM properties WHERE account_id=$1 AND id=$2;

-- name: ListUnits :many
SELECT id, account_id, property_id, code, bedrooms, rent_amount, status, created_at, updated_at FROM units WHERE account_id=$1 ORDER BY created_at DESC;

-- name: ListLeasesForAccount :many
SELECT id, account_id, unit_id, tenant_id, start_date, end_date, rent_amount, due_day, status, created_at, updated_at FROM leases WHERE account_id=$1 ORDER BY created_at DESC;

-- name: CreateMonthlyInvoice :exec
INSERT INTO invoices(account_id,lease_id,tenant_id,period,due_date,amount) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(lease_id,period) DO NOTHING;

-- name: ListInvoicesForAccount :many
SELECT id, account_id, lease_id, tenant_id, period, due_date, amount, paid_amount, status, created_at FROM invoices WHERE account_id=$1 ORDER BY due_date DESC;

-- name: CountUnitsByAccount :one
SELECT count(*)::bigint FROM units WHERE account_id = $1;
