-- Fix users_role_check and users_check to allow super_admin.
-- The previous migration (000004) used IF NOT EXISTS, so if the old
-- constraints were still present they were never replaced.

-- Drop existing role check constraint (by name or by definition).
DO $$
DECLARE
    c text;
BEGIN
    SELECT conname INTO c FROM pg_constraint
    WHERE conrelid = 'users'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%role IN (%'
    LIMIT 1;
    IF c IS NOT NULL THEN
        EXECUTE format('ALTER TABLE users DROP CONSTRAINT %I', c);
    END IF;
END $$;

-- Drop existing consistency check constraint.
DO $$
DECLARE
    c text;
BEGIN
    SELECT conname INTO c FROM pg_constraint
    WHERE conrelid = 'users'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%tenant_id IS NOT NULL AND account_id IS NULL%'
    LIMIT 1;
    IF c IS NOT NULL THEN
        EXECUTE format('ALTER TABLE users DROP CONSTRAINT %I', c);
    END IF;
END $$;

-- Re-add role check including super_admin.
ALTER TABLE users ADD CONSTRAINT users_role_check
  CHECK (role IN ('landlord','agent','tenant','super_admin'));

-- Re-add consistency check including super_admin.
ALTER TABLE users ADD CONSTRAINT users_check
  CHECK (
    (role = 'tenant'      AND tenant_id IS NOT NULL AND account_id IS NULL) OR
    (role IN ('landlord','agent') AND account_id IS NOT NULL AND tenant_id IS NULL) OR
    (role = 'super_admin' AND account_id IS NULL AND tenant_id IS NULL)
  );