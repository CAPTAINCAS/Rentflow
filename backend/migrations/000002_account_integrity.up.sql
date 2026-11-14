CREATE TABLE plans (
  id text PRIMARY KEY,
  name text NOT NULL,
  unit_limit integer,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (unit_limit IS NULL OR unit_limit >= 0)
);
INSERT INTO plans(id,name,unit_limit) VALUES ('free','Free',5),('pro','Pro',NULL);
ALTER TABLE accounts ADD COLUMN plan_id text NOT NULL DEFAULT 'free';
UPDATE accounts SET plan_id=CASE WHEN plan='pro' THEN 'pro' ELSE 'free' END;
ALTER TABLE accounts ADD CONSTRAINT accounts_plan_fk FOREIGN KEY(plan_id) REFERENCES plans(id);

ALTER TABLE properties ADD CONSTRAINT properties_account_id_unique UNIQUE(account_id,id);
ALTER TABLE units ADD CONSTRAINT units_account_id_unique UNIQUE(account_id,id);
ALTER TABLE tenants ADD CONSTRAINT tenants_account_id_unique UNIQUE(account_id,id);
ALTER TABLE leases ADD COLUMN deposit_amount numeric(12,2) NOT NULL DEFAULT 0 CHECK(deposit_amount >= 0);
ALTER TABLE leases DROP CONSTRAINT IF EXISTS leases_due_day_check;
ALTER TABLE leases ADD CONSTRAINT leases_due_day_check CHECK(due_day BETWEEN 1 AND 31);
ALTER TABLE leases ADD CONSTRAINT leases_account_id_unique UNIQUE(account_id,id);
ALTER TABLE invoices ADD CONSTRAINT invoices_account_id_unique UNIQUE(account_id,id);
ALTER TABLE payments ADD CONSTRAINT payments_account_id_unique UNIQUE(account_id,id);

ALTER TABLE units ADD CONSTRAINT units_property_account_fk FOREIGN KEY(account_id,property_id) REFERENCES properties(account_id,id) ON DELETE CASCADE;
ALTER TABLE leases ADD CONSTRAINT leases_unit_account_fk FOREIGN KEY(account_id,unit_id) REFERENCES units(account_id,id);
ALTER TABLE leases ADD CONSTRAINT leases_tenant_account_fk FOREIGN KEY(account_id,tenant_id) REFERENCES tenants(account_id,id);
ALTER TABLE invoices ADD CONSTRAINT invoices_lease_account_fk FOREIGN KEY(account_id,lease_id) REFERENCES leases(account_id,id) ON DELETE CASCADE;
ALTER TABLE invoices ADD CONSTRAINT invoices_tenant_account_fk FOREIGN KEY(account_id,tenant_id) REFERENCES tenants(account_id,id);
ALTER TABLE payments ADD CONSTRAINT payments_tenant_account_fk FOREIGN KEY(account_id,tenant_id) REFERENCES tenants(account_id,id);
ALTER TABLE payments ADD CONSTRAINT payments_invoice_account_fk FOREIGN KEY(account_id,invoice_id) REFERENCES invoices(account_id,id);
ALTER TABLE tenant_credits ADD CONSTRAINT tenant_credits_account_fk FOREIGN KEY(account_id,tenant_id) REFERENCES tenants(account_id,id) ON DELETE CASCADE;

ALTER TABLE payment_allocations ADD COLUMN account_id uuid;
UPDATE payment_allocations pa SET account_id=p.account_id FROM payments p WHERE p.id=pa.payment_id;
ALTER TABLE payment_allocations ALTER COLUMN account_id SET NOT NULL;
ALTER TABLE payment_allocations DROP CONSTRAINT payment_allocations_pkey;
ALTER TABLE payment_allocations ADD CONSTRAINT payment_allocations_pkey PRIMARY KEY(account_id,payment_id,invoice_id);
ALTER TABLE payment_allocations ADD CONSTRAINT allocations_payment_account_fk FOREIGN KEY(account_id,payment_id) REFERENCES payments(account_id,id) ON DELETE CASCADE;
ALTER TABLE payment_allocations ADD CONSTRAINT allocations_invoice_account_fk FOREIGN KEY(account_id,invoice_id) REFERENCES invoices(account_id,id) ON DELETE CASCADE;
CREATE INDEX plans_id_idx ON plans(id);
