-- Revert the super_admin role addition.
--
-- SAFETY: This migration will FAIL if any super_admin users exist.
-- The rollback restricts the users.role CHECK constraint to
-- ('landlord','agent','tenant'), which would reject super_admin rows.
-- Delete or reassign all super_admin users before running this down migration.
-- No user data is ever silently deleted or downgraded.

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM users WHERE role = 'super_admin') THEN
    RAISE EXCEPTION
      'Cannot roll back 000004_super_admin_role: super_admin users exist. Delete or reassign all super_admin users before rolling back.';
  END IF;
END $$;

-- Restore the original role CHECK constraint (without super_admin).
--
-- NOTE: Constraint names users_role_check and users_check are referenced
-- by name here. The original constraints in 000001_init were inline
-- (auto-named by PostgreSQL). Verify the actual auto-generated constraint
-- names against a running database before relying on this rollback.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
  CHECK (role IN ('landlord','agent','tenant'));

-- Restore the original role/account/tenant consistency check.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_check;
ALTER TABLE users ADD CONSTRAINT users_check
  CHECK ((role='tenant' AND tenant_id IS NOT NULL AND account_id IS NULL) OR (role IN ('landlord','agent') AND account_id IS NOT NULL AND tenant_id IS NULL));
