-- Audit logs and platform settings.
--
-- audit_logs records every administrative and managerial action for
-- accountability. platform_settings stores key-value configuration that
-- can be changed at runtime without redeploying.
--
-- This migration is safe to reverse: it only drops new tables and seed data.

CREATE TABLE audit_logs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  -- actor_id is nullable: system actions (scheduler, callbacks) have no user.
  actor_id uuid REFERENCES users(id) ON DELETE SET NULL,
  actor_role text NOT NULL DEFAULT '',
  actor_email text NOT NULL DEFAULT '',
  action text NOT NULL,
  resource_type text NOT NULL DEFAULT '',
  resource_id text NOT NULL DEFAULT '',
  account_id uuid REFERENCES accounts(id) ON DELETE SET NULL,
  ip_address inet,
  user_agent text NOT NULL DEFAULT '',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Index for account-scoped audit queries.
CREATE INDEX audit_logs_account_idx ON audit_logs(account_id, created_at DESC);

-- Index for actor-scoped audit queries.
CREATE INDEX audit_logs_actor_idx ON audit_logs(actor_id, created_at DESC) WHERE actor_id IS NOT NULL;

-- Index for resource-scoped audit queries.
CREATE INDEX audit_logs_resource_idx ON audit_logs(resource_type, resource_id, created_at DESC);

-- Platform settings: key-value store for runtime configuration.
CREATE TABLE platform_settings (
  key text PRIMARY KEY,
  value text NOT NULL,
  description text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by uuid REFERENCES users(id) ON DELETE SET NULL
);

-- Seed default platform settings.
INSERT INTO platform_settings(key, value, description) VALUES
  ('trial_period_days', '14', 'Number of days a new account is in trial before first billing'),
  ('grace_period_days', '7', 'Days after subscription expiry before account is suspended'),
  ('suspension_notice_days', '3', 'Days of advance notice before suspension takes effect'),
  ('cancellation_after_suspension_days', '30', 'Days after suspension before account is cancelled'),
  ('max_maintenance_image_mb', '5', 'Maximum size in MB for maintenance request images');
