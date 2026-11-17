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

-- name: CreatePasswordReset :exec
INSERT INTO password_resets(user_id, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, now());

-- name: GetPasswordResetByToken :one
SELECT id, user_id, expires_at, used_at, created_at
FROM password_resets
WHERE token_hash = $1;

-- name: MarkPasswordResetUsed :exec
UPDATE password_resets
SET used_at = now()
WHERE id = $1 AND used_at IS NULL;

-- name: ListPasswordResetsByUser :many
SELECT id, user_id, expires_at, used_at, created_at
FROM password_resets
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CreateBillingNotification :exec
INSERT INTO billing_notifications(
  account_id, tenant_id, lease_id, invoice_id, period,
  kind, scheduled_for, status, attempts, last_error, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', 0, '', now());

-- name: ListPendingBillingNotifications :many
SELECT id, account_id, tenant_id, lease_id, invoice_id, period,
       kind, scheduled_for, status, attempts, last_error, created_at, sent_at
FROM billing_notifications
WHERE status = 'pending'
  AND account_id = $1
ORDER BY scheduled_for ASC
LIMIT $2 OFFSET $3;

-- name: MarkBillingNotificationSent :exec
UPDATE billing_notifications
SET status = 'sent', sent_at = now()
WHERE id = $1 AND account_id = $2;

-- name: MarkBillingNotificationFailed :exec
UPDATE billing_notifications
SET status = 'failed', attempts = attempts + 1, last_error = $3
WHERE id = $1 AND account_id = $2;

-- name: CreateSubscriptionPayment :exec
INSERT INTO subscription_payments(
  account_id, amount, method, status, external_ref,
  phone, checkout_request_id, transaction_date, notes, created_at
)
VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7, $8, now());

-- name: GetSubscriptionPaymentByCheckoutID :one
SELECT id, account_id, amount, method, status, external_ref,
       phone, checkout_request_id, transaction_date, notes, created_at
FROM subscription_payments
WHERE checkout_request_id = $1;

-- name: ListSubscriptionPaymentsByAccount :many
SELECT id, account_id, amount, method, status, external_ref,
       phone, checkout_request_id, transaction_date, notes, created_at
FROM subscription_payments
WHERE account_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateSubscriptionPaymentStatus :exec
UPDATE subscription_payments
SET status = $2, transaction_date = $3
WHERE id = $1 AND account_id = $4;

-- name: CreatePropertyAssignment :exec
INSERT INTO property_assignments(
  account_id, agent_id, property_id, assigned_by, created_at
)
VALUES ($1, $2, $3, $4, now());

-- name: ListPropertyAssignmentsByAgent :many
SELECT id, account_id, agent_id, property_id, assigned_by, created_at
FROM property_assignments
WHERE agent_id = $1 AND account_id = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: ListPropertyAssignmentsByProperty :many
SELECT id, account_id, agent_id, property_id, assigned_by, created_at
FROM property_assignments
WHERE property_id = $1 AND account_id = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: DeletePropertyAssignment :exec
DELETE FROM property_assignments
WHERE agent_id = $1 AND property_id = $2 AND account_id = $3;

-- name: CreateMaintenanceRequest :exec
INSERT INTO maintenance_requests(
  account_id, tenant_id, lease_id, unit_id, property_id,
  reference, category, description, image_url, status, priority,
  assigned_to, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'open', $10, $11, now(), now());

-- name: GetMaintenanceRequestByReference :one
SELECT id, account_id, tenant_id, lease_id, unit_id, property_id,
       reference, category, description, image_url, status, priority,
       assigned_to, resolved_at, resolution_notes, created_at, updated_at
FROM maintenance_requests
WHERE reference = $1 AND account_id = $2;

-- name: ListMaintenanceRequestsByTenant :many
SELECT id, account_id, tenant_id, lease_id, unit_id, property_id,
       reference, category, description, image_url, status, priority,
       assigned_to, resolved_at, resolution_notes, created_at, updated_at
FROM maintenance_requests
WHERE tenant_id = $1 AND account_id = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: ListMaintenanceRequestsByAccount :many
SELECT id, account_id, tenant_id, lease_id, unit_id, property_id,
       reference, category, description, image_url, status, priority,
       assigned_to, resolved_at, resolution_notes, created_at, updated_at
FROM maintenance_requests
WHERE account_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListMaintenanceRequestsByStatus :many
SELECT id, account_id, tenant_id, lease_id, unit_id, property_id,
       reference, category, description, image_url, status, priority,
       assigned_to, resolved_at, resolution_notes, created_at, updated_at
FROM maintenance_requests
WHERE account_id = $1 AND status = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: UpdateMaintenanceRequestStatus :exec
UPDATE maintenance_requests
SET status = $2, updated_at = now()
WHERE id = $1 AND account_id = $3;

-- name: AssignMaintenanceRequest :exec
UPDATE maintenance_requests
SET assigned_to = $2, updated_at = now()
WHERE id = $1 AND account_id = $3;

-- name: CreateMaintenanceComment :exec
INSERT INTO maintenance_comments(
  request_id, author_id, body, created_at
)
VALUES ($1, $2, $3, now());

-- name: ListMaintenanceCommentsByRequest :many
SELECT id, request_id, author_id, body, created_at
FROM maintenance_comments
WHERE request_id = $1
ORDER BY created_at ASC;

-- name: CreateAuditLog :exec
INSERT INTO audit_logs(
  actor_id, actor_role, actor_email, action, resource_type,
  resource_id, account_id, ip_address, user_agent, metadata, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now());

-- name: ListAuditLogsByAccount :many
SELECT id, actor_id, actor_role, actor_email, action, resource_type,
       resource_id, account_id, ip_address, user_agent, metadata, created_at
FROM audit_logs
WHERE account_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetPlatformSetting :one
SELECT key, value, description, updated_at, updated_by
FROM platform_settings
WHERE key = $1;

-- name: ListPlatformSettings :many
SELECT key, value, description, updated_at, updated_by
FROM platform_settings
ORDER BY key;

-- name: UpdatePlatformSetting :exec
UPDATE platform_settings
SET value = $2, updated_at = now(), updated_by = $3
WHERE key = $1;
