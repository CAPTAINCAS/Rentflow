-- Subscription management for platform accounts.
--
-- Adds subscription lifecycle fields to accounts and creates a dedicated
-- subscription_payments table. Subscription payments are completely separate
-- from rent payments (payments table) to prevent interference between the
-- two M-Pesa money flows.
--
-- This migration is safe to reverse: it only drops new columns and tables.

-- Add subscription fields to accounts.
ALTER TABLE accounts
  ADD COLUMN subscription_status text NOT NULL DEFAULT 'trialing'
    CHECK (subscription_status IN ('trialing','active','past_due','suspended','cancelled')),
  ADD COLUMN subscription_plan_id text REFERENCES plans(id),
  ADD COLUMN subscription_started_at timestamptz,
  ADD COLUMN subscription_renews_at timestamptz,
  ADD COLUMN subscription_expires_at timestamptz,
  ADD COLUMN suspended_at timestamptz,
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- Index for admin queries filtering by subscription status.
CREATE INDEX accounts_subscription_status_idx ON accounts(subscription_status);

-- Subscription payments table. Completely separate from the payments table
-- used for tenant rent payments. Uses the platform M-Pesa shortcode and
-- callback token, never the landlord's.
CREATE TABLE subscription_payments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  amount numeric(12,2) NOT NULL CHECK(amount > 0),
  method text NOT NULL DEFAULT 'mpesa' CHECK(method IN ('mpesa','bank','other')),
  status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed','failed')),
  external_ref text UNIQUE,
  phone text NOT NULL DEFAULT '',
  checkout_request_id text,
  transaction_date timestamptz,
  notes text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Index for looking up subscription payments by account.
CREATE INDEX subscription_payments_account_idx ON subscription_payments(account_id, created_at DESC);

-- Index for callback idempotency lookups by checkout_request_id.
CREATE INDEX subscription_payments_checkout_idx ON subscription_payments(checkout_request_id) WHERE checkout_request_id IS NOT NULL;
