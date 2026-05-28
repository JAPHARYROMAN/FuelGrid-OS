-- 0056_statements_alerts: customer statements + deterministic credit alerts
-- (Phase 8, Stages 12-13). Statements summarize a period's AR activity from the
-- Phase-7 ledger; alerts flag limit utilization, overdue exposure, and other
-- deterministic risk signals and can place a customer on hold.

CREATE TABLE customer_statements (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id     uuid NOT NULL,
    period_start    date NOT NULL,
    period_end      date NOT NULL,
    opening_balance numeric(14, 2) NOT NULL DEFAULT 0,
    charges         numeric(14, 2) NOT NULL DEFAULT 0,
    payments        numeric(14, 2) NOT NULL DEFAULT 0,
    closing_balance numeric(14, 2) NOT NULL DEFAULT 0,
    status          text NOT NULL DEFAULT 'draft',
    generated_by    uuid NOT NULL,
    generated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_statement_status CHECK (status IN ('draft', 'issued', 'superseded')),
    CONSTRAINT statement_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_statements_tenant   ON customer_statements(tenant_id);
CREATE INDEX idx_statements_customer ON customer_statements(customer_id);

ALTER TABLE customer_statements ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_statements
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE customer_credit_alerts (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id       uuid NOT NULL,
    alert_type        text NOT NULL,
    severity          text NOT NULL DEFAULT 'medium',
    status            text NOT NULL DEFAULT 'open',
    detail            text,
    assigned_to       uuid,
    resolution_reason text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_credit_alert_status CHECK (status IN ('open', 'acknowledged', 'resolved', 'dismissed')),
    CONSTRAINT chk_credit_alert_severity CHECK (severity IN ('info', 'low', 'medium', 'high', 'critical')),
    CONSTRAINT credit_alert_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE RESTRICT
);

-- One open alert per (customer, type) keeps the scan idempotent.
CREATE UNIQUE INDEX uq_credit_alert_open ON customer_credit_alerts(tenant_id, customer_id, alert_type) WHERE status IN ('open', 'acknowledged');
CREATE INDEX idx_credit_alerts_tenant ON customer_credit_alerts(tenant_id);

CREATE TRIGGER customer_credit_alerts_set_updated_at
    BEFORE UPDATE ON customer_credit_alerts FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE customer_credit_alerts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_credit_alerts
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: customer_statement.issue / customer_credit_alert.manage.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('customer_statement.issue',    'Generate and issue customer statements', 'finance', false),
    ('customer_credit_alert.manage', 'Manage customer credit alerts',         'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('customer_statement.issue', 'customer_credit_alert.manage')
  AND r.code IN ('system_admin', 'regional_manager', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
