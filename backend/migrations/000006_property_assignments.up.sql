-- Property assignments for agents.
--
-- Creates the property_assignments table that maps agents to specific
-- properties within an account. This is the single source of truth for
-- agent property isolation: every query that returns property-scoped data
-- must filter through this table for agents.
--
-- This migration is safe to reverse: it only drops a new table.

CREATE TABLE property_assignments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  agent_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  property_id uuid NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
  assigned_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  -- Prevent duplicate assignments of the same agent to the same property.
  UNIQUE(agent_id, property_id)
);

-- Index for looking up all properties assigned to an agent.
CREATE INDEX property_assignments_agent_idx ON property_assignments(agent_id, account_id);

-- Index for looking up all agents assigned to a property.
CREATE INDEX property_assignments_property_idx ON property_assignments(property_id, account_id);

-- Ensure users has a unique constraint on (account_id, id) for the composite FK.
CREATE UNIQUE INDEX IF NOT EXISTS users_account_id_id_unique ON users(account_id, id);

-- Composite FK to ensure the agent belongs to the same account as the assignment.
-- This prevents cross-account assignment leakage.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='property_assignments_agent_account_fk' AND conrelid='property_assignments'::regclass) THEN
    ALTER TABLE property_assignments ADD CONSTRAINT property_assignments_agent_account_fk FOREIGN KEY (account_id, agent_id) REFERENCES users(account_id, id) ON DELETE CASCADE;
  END IF;
END $$;

-- Composite FK to ensure the property belongs to the same account.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='property_assignments_property_account_fk' AND conrelid='property_assignments'::regclass) THEN
    ALTER TABLE property_assignments ADD CONSTRAINT property_assignments_property_account_fk FOREIGN KEY (account_id, property_id) REFERENCES properties(account_id, id) ON DELETE CASCADE;
  END IF;
END $$;
