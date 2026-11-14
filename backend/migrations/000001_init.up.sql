CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE accounts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  plan text NOT NULL DEFAULT 'free' CHECK (plan IN ('free','pro')),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,
  tenant_id uuid,
  email text NOT NULL UNIQUE,
  full_name text NOT NULL,
  phone text NOT NULL DEFAULT '',
  password_hash text NOT NULL,
  role text NOT NULL CHECK (role IN ('landlord','agent','tenant')),
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((role='tenant' AND tenant_id IS NOT NULL AND account_id IS NULL) OR (role IN ('landlord','agent') AND account_id IS NOT NULL AND tenant_id IS NULL))
);
CREATE TABLE properties (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name text NOT NULL, address text NOT NULL DEFAULT '', description text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE units (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  property_id uuid NOT NULL REFERENCES properties(id) ON DELETE CASCADE, code text NOT NULL, bedrooms integer NOT NULL DEFAULT 0,
  rent_amount numeric(12,2) NOT NULL CHECK (rent_amount >= 0), status text NOT NULL DEFAULT 'vacant' CHECK (status IN ('vacant','occupied','maintenance')),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(property_id,code)
);
CREATE TABLE tenants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  full_name text NOT NULL, email text NOT NULL DEFAULT '', phone text NOT NULL DEFAULT '', national_id text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE users ADD CONSTRAINT users_tenant_fk FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX users_tenant_id_unique ON users(tenant_id) WHERE tenant_id IS NOT NULL;
CREATE TABLE leases (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  unit_id uuid NOT NULL REFERENCES units(id), tenant_id uuid NOT NULL REFERENCES tenants(id),
  start_date date NOT NULL, end_date date, rent_amount numeric(12,2) NOT NULL CHECK(rent_amount >= 0),
  due_day integer NOT NULL DEFAULT 1 CHECK(due_day BETWEEN 1 AND 28), status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','ended')),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX one_active_lease_per_unit ON leases(unit_id) WHERE status='active';
CREATE TABLE invoices (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  lease_id uuid NOT NULL REFERENCES leases(id) ON DELETE CASCADE, tenant_id uuid NOT NULL REFERENCES tenants(id),
  period date NOT NULL, due_date date NOT NULL, amount numeric(12,2) NOT NULL CHECK(amount >= 0), paid_amount numeric(12,2) NOT NULL DEFAULT 0 CHECK(paid_amount >= 0),
  status text NOT NULL DEFAULT 'unpaid' CHECK(status IN ('unpaid','partial','paid')), created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(lease_id,period)
);
CREATE TABLE payments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,
  tenant_id uuid REFERENCES tenants(id), invoice_id uuid REFERENCES invoices(id), amount numeric(12,2) NOT NULL CHECK(amount > 0),
  method text NOT NULL CHECK(method IN ('mpesa','cash','bank','other')), status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed','failed','unmatched')),
  external_ref text UNIQUE, phone text NOT NULL DEFAULT '', checkout_request_id text UNIQUE, transaction_date timestamptz,
  notes text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE payment_allocations (
  payment_id uuid NOT NULL REFERENCES payments(id) ON DELETE CASCADE, invoice_id uuid NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  amount numeric(12,2) NOT NULL CHECK(amount > 0), PRIMARY KEY(payment_id,invoice_id)
);
CREATE TABLE tenant_credits (
  tenant_id uuid PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE, account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  balance numeric(12,2) NOT NULL DEFAULT 0 CHECK(balance >= 0), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE refresh_tokens (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash text NOT NULL UNIQUE, expires_at timestamptz NOT NULL, revoked_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX properties_account_idx ON properties(account_id);
CREATE INDEX units_account_idx ON units(account_id);
CREATE INDEX tenants_account_idx ON tenants(account_id);
CREATE INDEX leases_account_idx ON leases(account_id);
CREATE INDEX invoices_account_due_idx ON invoices(account_id,due_date);
CREATE INDEX payments_account_created_idx ON payments(account_id,created_at DESC);
CREATE INDEX payments_unmatched_idx ON payments(account_id,status) WHERE status='unmatched';
CREATE INDEX users_email_idx ON users(lower(email));
