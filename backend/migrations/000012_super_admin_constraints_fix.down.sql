ALTER TABLE users DROP CONSTRAINT IF EXISTS users_check;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;

ALTER TABLE users ADD CONSTRAINT users_role_check
  CHECK (role IN ('landlord','agent','tenant'));

ALTER TABLE users ADD CONSTRAINT users_check
  CHECK (
    (role = 'tenant' AND tenant_id IS NOT NULL AND account_id IS NULL) OR
    (role IN ('landlord','agent') AND account_id IS NOT NULL AND tenant_id IS NULL)
  );