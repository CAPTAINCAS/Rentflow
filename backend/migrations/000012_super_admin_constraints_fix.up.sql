-- Explicitly drop and recreate users_check and users_role_check.
-- Previous migrations used pattern matching which may not have matched
-- the actual constraint names in production.

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_check;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;

ALTER TABLE users ADD CONSTRAINT users_role_check
  CHECK (role IN ('landlord','agent','tenant','super_admin'));

ALTER TABLE users ADD CONSTRAINT users_check
  CHECK (
    (role = 'tenant'      AND tenant_id IS NOT NULL AND account_id IS NULL) OR
    (role IN ('landlord','agent') AND account_id IS NOT NULL AND tenant_id IS NULL) OR
    (role = 'super_admin' AND account_id IS NULL AND tenant_id IS NULL)
  );