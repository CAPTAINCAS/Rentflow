-- Add the super_admin role to the users table.
--
-- The existing CHECK constraints on users.role and the role/account/tenant
-- consistency check must be replaced to allow super_admin users, who belong
-- to no account and no tenant (platform-level access).
--
-- CONSTRAINT NAME RESOLUTION:
-- The original constraints in 000001_init are inline (no explicit name).
-- PostgreSQL auto-generates constraint names, which cannot be verified
-- without a database connection. This migration uses pg_constraint catalog
-- lookups to find and drop the original constraints by their definition
-- pattern, then adds new constraints with explicit, known names.
--
-- This migration is safe to reverse: it only restores the previous CHECK
-- constraints. No data is deleted.

-- Drop the existing role CHECK constraint (auto-generated name in 000001).
-- Found via pg_constraint by definition pattern rather than by name.
DO $$
DECLARE
    constraint_name text;
BEGIN
    SELECT conname INTO constraint_name
    FROM pg_constraint
    WHERE conrelid = 'users'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%role IN (%'
    LIMIT 1;

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE users DROP CONSTRAINT %I', constraint_name);
    END IF;
END $$;

-- Add the new role CHECK constraint including super_admin.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='users_role_check' AND conrelid='users'::regclass) THEN
    ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('landlord','agent','tenant','super_admin'));
  END IF;
END $$;

-- Drop the existing role/account/tenant consistency CHECK constraint.
-- Found via pg_constraint by definition pattern rather than by name.
DO $$
DECLARE
    constraint_name text;
BEGIN
    SELECT conname INTO constraint_name
    FROM pg_constraint
    WHERE conrelid = 'users'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%tenant_id IS NOT NULL AND account_id IS NULL%'
    LIMIT 1;

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE users DROP CONSTRAINT %I', constraint_name);
    END IF;
END $$;

-- Add the updated consistency check:
--   * tenant      -> tenant_id NOT NULL, account_id NULL
--   * landlord    -> account_id NOT NULL, tenant_id NULL
--   * agent       -> account_id NOT NULL, tenant_id NULL
--   * super_admin -> account_id NULL, tenant_id NULL (platform-level)
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='users_check' AND conrelid='users'::regclass) THEN
    ALTER TABLE users ADD CONSTRAINT users_check
  CHECK (
    (role = 'tenant'      AND tenant_id IS NOT NULL AND account_id IS NULL) OR
    (role IN ('landlord','agent') AND account_id IS NOT NULL AND tenant_id IS NULL) OR
    (role = 'super_admin' AND account_id IS NULL AND tenant_id IS NULL)
  );
  END IF;
END $$;
