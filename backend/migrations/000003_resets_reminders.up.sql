CREATE TABLE password_resets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash text NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX password_resets_active_idx ON password_resets(user_id,expires_at) WHERE used_at IS NULL;
CREATE TABLE billing_notifications (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  lease_id uuid NOT NULL,
  invoice_id uuid NOT NULL,
  period date NOT NULL,
  kind text NOT NULL CHECK(kind IN ('before_due','due','overdue')),
  scheduled_for date NOT NULL,
  status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sent','failed')),
  attempts integer NOT NULL DEFAULT 0,
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  sent_at timestamptz,
  UNIQUE(lease_id,period,kind,scheduled_for),
  FOREIGN KEY(account_id,tenant_id) REFERENCES tenants(account_id,id) ON DELETE CASCADE,
  FOREIGN KEY(account_id,lease_id) REFERENCES leases(account_id,id) ON DELETE CASCADE,
  FOREIGN KEY(account_id,invoice_id) REFERENCES invoices(account_id,id) ON DELETE CASCADE
);
CREATE INDEX billing_notifications_pending_idx ON billing_notifications(status,created_at) WHERE status<>'sent';
