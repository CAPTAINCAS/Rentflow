-- Revert to original constraints (without super_admin).
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

ALTER TABLE users ADD CONSTRAINT users_role_check
  CHECK (role IN ('landlord','agent','tenant'));

ALTER TABLE users ADD CONSTRAINT users_check
  CHECK (
    (role = 'tenant' AND tenant_id IS NOT NULL AND account_id IS NULL) OR
    (role IN ('landlord','agent') AND account_id IS NOT NULL AND tenant_id IS NULL)
  );