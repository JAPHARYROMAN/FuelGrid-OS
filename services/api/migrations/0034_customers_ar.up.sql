-- 0034_customers_ar: credit customers and the accounts-receivable ledger
-- (Phase 6, Stage 6).
--
-- Customers are tenant-wide (a customer fuels across stations). The AR ledger
-- is append-only (the Phase-4 ledger discipline): a customer's balance is the
-- sum of its entries. A credit sale posts a 'charge' (+, increases what they
-- owe); a customer payment posts a 'payment' (−). balance_after snapshots the
-- running balance. Phase 7 layers customer invoices/aging on top of this.

CREATE TABLE customers (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    code          text NOT NULL,
    name          text NOT NULL,
    contact_name  text,
    contact_phone text,
    contact_email text,
    credit_limit  numeric(14, 2) NOT NULL DEFAULT 0,
    status        text NOT NULL DEFAULT 'active',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_customers_status CHECK (status IN ('active', 'inactive', 'deleted')),
    CONSTRAINT chk_customers_limit  CHECK (credit_limit >= 0)
);

CREATE INDEX idx_customers_tenant_id ON customers(tenant_id);
CREATE UNIQUE INDEX idx_customers_tenant_code
    ON customers(tenant_id, lower(code)) WHERE status <> 'deleted';

ALTER TABLE customers ADD CONSTRAINT uq_customers_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER customers_set_updated_at
    BEFORE UPDATE ON customers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE customers ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customers
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE ar_entries (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id     uuid NOT NULL,
    entry_type      text NOT NULL,
    amount          numeric(14, 2) NOT NULL,
    balance_after   numeric(14, 2) NOT NULL,
    source_ref_type text,
    source_ref_id   uuid,
    recorded_by     uuid NOT NULL,
    recorded_at     timestamptz NOT NULL DEFAULT now(),
    notes           text,
    created_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_ar_entry_type CHECK (entry_type IN ('charge', 'payment', 'adjustment')),

    CONSTRAINT ar_entries_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT ar_entries_recorded_by_fk
        FOREIGN KEY (tenant_id, recorded_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_ar_entries_tenant_id ON ar_entries(tenant_id);
CREATE INDEX idx_ar_entries_customer  ON ar_entries(customer_id, recorded_at);

ALTER TABLE ar_entries ADD CONSTRAINT uq_ar_entries_tenant_id UNIQUE (tenant_id, id);

ALTER TABLE ar_entries ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ar_entries
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: customer.read (tenant-wide). Customer writes ride the existing
-- credit.manage (0004); exceeding a credit limit needs credit.override_limit.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('customer.read', 'View credit customers and statements', 'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'customer.read'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor',
                 'finance_officer', 'executive', 'auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
