-- 0051_customer_credit_profiles: enforceable credit terms per customer
-- (Phase 8, Stage 2). The enforced credit limit stays on customers.credit_limit
-- (Phase-6 sales already check it); the profile adds terms, a soft warning
-- threshold, risk category, and a manual hold. Available credit is computed
-- against the Phase-7 AR balance plus active fuel authorizations.

CREATE TABLE customer_credit_profiles (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id           uuid NOT NULL,
    payment_terms_days    integer NOT NULL DEFAULT 0,
    grace_days            integer NOT NULL DEFAULT 0,
    statement_cycle_days  integer NOT NULL DEFAULT 30,
    risk_category         text NOT NULL DEFAULT 'standard',
    warning_threshold_pct numeric(5, 2) NOT NULL DEFAULT 80,
    hold                  boolean NOT NULL DEFAULT false,
    hold_reason           text,
    review_date           date,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_ccp_warning CHECK (warning_threshold_pct >= 0 AND warning_threshold_pct <= 100),
    CONSTRAINT ccp_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE CASCADE
);

-- One profile per customer.
CREATE UNIQUE INDEX uq_ccp_customer ON customer_credit_profiles(tenant_id, customer_id);
CREATE INDEX idx_ccp_tenant ON customer_credit_profiles(tenant_id);

CREATE TRIGGER customer_credit_profiles_set_updated_at
    BEFORE UPDATE ON customer_credit_profiles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE customer_credit_profiles ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_credit_profiles
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: customer_credit.manage / .override / .read (tenant-wide).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('customer_credit.manage',   'Manage customer credit terms and holds', 'finance', false),
    ('customer_credit.override', 'Override credit limit / hold on a sale',  'finance', false),
    ('customer_credit.read',     'View customer credit position',           'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('customer_credit.manage', 'customer_credit.override', 'customer_credit.read')
  AND r.code IN ('system_admin', 'regional_manager', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
