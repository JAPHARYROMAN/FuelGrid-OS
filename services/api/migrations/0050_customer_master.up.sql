-- 0050_customer_master: evolve the Phase-6/7 minimal customer into a full
-- credit/fleet master with contacts and a richer account lifecycle (Phase 8,
-- Stage 1). Existing columns and legacy statuses are preserved so Phase-6/7
-- code keeps working; new columns are additive with safe defaults.

ALTER TABLE customers
    ADD COLUMN legal_name         text,
    ADD COLUMN trading_name       text,
    ADD COLUMN tax_id             text,
    ADD COLUMN billing_address    text,
    ADD COLUMN account_type       text NOT NULL DEFAULT 'standard',
    ADD COLUMN default_terms_days integer NOT NULL DEFAULT 0,
    ADD COLUMN notes              text;

-- Broaden the lifecycle: prospect -> active -> on_hold -> suspended -> closed.
-- Legacy 'inactive'/'deleted' are retained so existing rows and the Phase-6/7
-- soft-delete index/queries continue to work.
ALTER TABLE customers DROP CONSTRAINT chk_customers_status;
ALTER TABLE customers ADD CONSTRAINT chk_customers_status
    CHECK (status IN ('prospect', 'active', 'on_hold', 'suspended', 'closed', 'inactive', 'deleted'));

CREATE TABLE customer_contacts (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id             uuid NOT NULL,
    name                    text NOT NULL,
    role                    text,
    email                   text,
    phone                   text,
    statement_preference    text NOT NULL DEFAULT 'email',
    notification_preference text NOT NULL DEFAULT 'email',
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT customer_contacts_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_customer_contacts_tenant   ON customer_contacts(tenant_id);
CREATE INDEX idx_customer_contacts_customer ON customer_contacts(customer_id);

CREATE TRIGGER customer_contacts_set_updated_at
    BEFORE UPDATE ON customer_contacts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE customer_contacts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_contacts
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: customer.manage (tenant-wide customer administration).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('customer.manage', 'Manage customer master, contacts, and lifecycle', 'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'customer.manage'
  AND r.code IN ('system_admin', 'regional_manager', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
