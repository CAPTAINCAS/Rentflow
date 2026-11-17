-- Revert subscription management.
--
-- WARNING: This drops subscription_payments and all subscription data on
-- accounts. Export any needed data before rolling back.

DROP TABLE IF EXISTS subscription_payments;
DROP INDEX IF EXISTS accounts_subscription_status_idx;

ALTER TABLE accounts
  DROP COLUMN IF EXISTS subscription_status,
  DROP COLUMN IF EXISTS subscription_plan_id,
  DROP COLUMN IF EXISTS subscription_started_at,
  DROP COLUMN IF EXISTS subscription_renews_at,
  DROP COLUMN IF EXISTS subscription_expires_at,
  DROP COLUMN IF EXISTS suspended_at,
  DROP COLUMN IF EXISTS updated_at;
