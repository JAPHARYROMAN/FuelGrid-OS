-- 0037_accounts: the chart of accounts (Phase 7, Stage 1).
--
-- Every finance posting references an account. system_key tags the accounts the
-- posting service maps to (cash on hand, AR, AP, sales revenue, COGS, …); the
-- default fuel-retail chart is seeded per tenant by the accounting repo
-- (SeedDefaultChart), not here, since it is tenant data.

CREATE TABLE accounts (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    code           text NOT NULL,
    name           text NOT NULL,
    type           text NOT NULL,
    normal_balance text NOT NULL,
    parent_id      uuid,
    system_key     text,
    status         text NOT NULL DEFAULT 'active',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_accounts_type CHECK (
        type IN ('asset', 'liability', 'equity', 'income', 'expense', 'contra_asset', 'contra_income')
    ),
    CONSTRAINT chk_accounts_normal CHECK (normal_balance IN ('debit', 'credit')),
    CONSTRAINT chk_accounts_status CHECK (status IN ('active', 'inactive'))
);

CREATE INDEX idx_accounts_tenant_id ON accounts(tenant_id);
CREATE UNIQUE INDEX idx_accounts_tenant_code ON accounts(tenant_id, lower(code));
CREATE UNIQUE INDEX idx_accounts_system_key
    ON accounts(tenant_id, system_key) WHERE system_key IS NOT NULL;

-- Composite tenant key first, then the self-referencing parent FK onto it.
ALTER TABLE accounts ADD CONSTRAINT uq_accounts_tenant_id UNIQUE (tenant_id, id);
ALTER TABLE accounts ADD CONSTRAINT accounts_parent_fk
    FOREIGN KEY (tenant_id, parent_id) REFERENCES accounts(tenant_id, id) ON DELETE RESTRICT;

CREATE TRIGGER accounts_set_updated_at
    BEFORE UPDATE ON accounts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE accounts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON accounts
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: account.manage (set up the chart) and finance.read (all
-- finance views), both tenant-wide.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('account.manage', 'Manage the chart of accounts', 'finance', false),
    ('finance.read',   'View finance ledgers, documents, and reports', 'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'account.manage' AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'finance.read'
  AND r.code IN ('system_admin', 'finance_officer', 'regional_manager', 'executive', 'auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
