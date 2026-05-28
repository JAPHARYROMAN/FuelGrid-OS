-- 0060_risk_foundation: the risk signal layer, rule registry, and alert
-- lifecycle (Phase 10, Stages 1-3). Signals are idempotent typed facts linked
-- to source events; rules are versioned and explainable; alerts carry severity,
-- evidence, routing, status history, and a disposition on close. Risk never
-- rewrites source operational data — closing an alert records a disposition.

CREATE TABLE risk_signals (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    signal_type     text NOT NULL,
    source_event_id uuid NOT NULL,
    station_id      uuid,
    actor_id        uuid,
    customer_id     uuid,
    supplier_id     uuid,
    amount          numeric(14, 2),
    litres          numeric(14, 3),
    occurred_at     timestamptz NOT NULL DEFAULT now(),
    metadata        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now()
);
-- Idempotent ingestion: one signal per (type, source event).
CREATE UNIQUE INDEX uq_risk_signal_source ON risk_signals(tenant_id, signal_type, source_event_id);
CREATE INDEX idx_risk_signals_tenant ON risk_signals(tenant_id, occurred_at);
ALTER TABLE risk_signals ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON risk_signals
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE risk_rules (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    code          text NOT NULL,
    name          text NOT NULL,
    rule_type     text NOT NULL DEFAULT 'threshold',
    status        text NOT NULL DEFAULT 'draft',
    threshold     numeric(14, 2),
    lookback_days integer NOT NULL DEFAULT 30,
    severity      text NOT NULL DEFAULT 'medium',
    description   text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_risk_rule_status CHECK (status IN ('draft', 'active', 'paused', 'retired')),
    CONSTRAINT chk_risk_rule_severity CHECK (severity IN ('info', 'low', 'medium', 'high', 'critical'))
);
CREATE UNIQUE INDEX uq_risk_rule_code ON risk_rules(tenant_id, code);
CREATE INDEX idx_risk_rules_tenant ON risk_rules(tenant_id);
CREATE TRIGGER risk_rules_set_updated_at BEFORE UPDATE ON risk_rules FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE risk_rules ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON risk_rules
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE risk_alerts (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    rule_code    text,
    alert_type   text NOT NULL,
    severity     text NOT NULL DEFAULT 'medium',
    status       text NOT NULL DEFAULT 'open',
    station_id   uuid,
    subject_type text,
    subject_id   uuid,
    detail       text,
    amount       numeric(14, 2),
    evidence     jsonb NOT NULL DEFAULT '{}'::jsonb,
    assigned_to  uuid,
    disposition  text,
    score        integer NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_risk_alert_status CHECK (status IN ('open', 'acknowledged', 'investigating', 'resolved', 'dismissed', 'escalated')),
    CONSTRAINT chk_risk_alert_severity CHECK (severity IN ('info', 'low', 'medium', 'high', 'critical'))
);
-- One open alert per (type, subject) keeps detection idempotent.
CREATE UNIQUE INDEX uq_risk_alert_open ON risk_alerts(tenant_id, alert_type, subject_id)
    WHERE status IN ('open', 'acknowledged', 'investigating', 'escalated');
CREATE INDEX idx_risk_alerts_tenant ON risk_alerts(tenant_id, status);
ALTER TABLE risk_alerts ADD CONSTRAINT uq_risk_alerts_tenant_id UNIQUE (tenant_id, id);
CREATE TRIGGER risk_alerts_set_updated_at BEFORE UPDATE ON risk_alerts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE risk_alerts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON risk_alerts
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('risk_signal.admin', 'Ingest and rebuild risk signals', 'risk', false),
    ('risk_rule.manage',  'Manage risk rules',               'risk', false),
    ('risk_alert.read',   'View risk alerts',                'risk', false),
    ('risk_alert.manage', 'Manage and resolve risk alerts',  'risk', false),
    ('risk.read',         'View risk dashboards',            'risk', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('risk_signal.admin', 'risk_rule.manage', 'risk_alert.read', 'risk_alert.manage', 'risk.read')
  AND r.code IN ('system_admin', 'regional_manager', 'executive', 'auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
