-- Maintenance requests and comments.
--
-- Tenants submit maintenance requests; landlords and agents manage them.
-- Each request has a human-readable reference, status, priority, and
-- optional image URL. Comments allow back-and-forth communication.
--
-- This migration is safe to reverse: it only drops new tables.

-- Sequence for generating human-readable maintenance references (MR-2026-0001).
CREATE SEQUENCE IF NOT EXISTS maintenance_ref_seq;

CREATE TABLE maintenance_requests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  lease_id uuid REFERENCES leases(id) ON DELETE SET NULL,
  unit_id uuid REFERENCES units(id) ON DELETE SET NULL,
  property_id uuid REFERENCES properties(id) ON DELETE SET NULL,
  reference text NOT NULL UNIQUE,
  category text NOT NULL DEFAULT 'general'
    CHECK (category IN ('plumbing','electrical','structural','appliance','general','other')),
  description text NOT NULL,
  image_url text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'open'
    CHECK (status IN ('open','in_progress','resolved','closed')),
  priority text NOT NULL DEFAULT 'medium'
    CHECK (priority IN ('low','medium','high','urgent')),
  assigned_to uuid REFERENCES users(id) ON DELETE SET NULL,
  resolved_at timestamptz,
  resolution_notes text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- Index for tenant-scoped queries (tenant views own requests).
CREATE INDEX maintenance_requests_tenant_idx ON maintenance_requests(tenant_id, created_at DESC);

-- Index for account-scoped queries (landlord/agent views).
CREATE INDEX maintenance_requests_account_idx ON maintenance_requests(account_id, status, created_at DESC);

-- Index for agent assignment queries.
CREATE INDEX maintenance_requests_assigned_idx ON maintenance_requests(assigned_to, status) WHERE assigned_to IS NOT NULL;

-- Trigger to auto-update updated_at on every UPDATE.
CREATE OR REPLACE FUNCTION maintenance_requests_update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER maintenance_requests_updated_at
  BEFORE UPDATE ON maintenance_requests
  FOR EACH ROW EXECUTE FUNCTION maintenance_requests_update_updated_at();

-- Maintenance comments for back-and-forth communication.
CREATE TABLE maintenance_comments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  request_id uuid NOT NULL REFERENCES maintenance_requests(id) ON DELETE CASCADE,
  author_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Index for listing comments on a request.
CREATE INDEX maintenance_comments_request_idx ON maintenance_comments(request_id, created_at);
