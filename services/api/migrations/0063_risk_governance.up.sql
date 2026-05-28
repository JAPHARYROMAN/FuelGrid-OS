-- 0063_risk_governance: rule tuning, alert suppression, and feedback (Phase 10,
-- Stages 14-15). Suppressions silence a noisy alert type (optionally scoped to
-- an entity) with a reason and expiry, while keeping a full audit trail.
-- Feedback from dispositions feeds rule tuning.

CREATE TABLE risk_suppressions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    alert_type  text NOT NULL,
    entity_id   uuid,
    reason      text NOT NULL,
    expires_at  timestamptz,
    created_by  uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT risk_suppression_created_by_fk FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_risk_suppressions_tenant ON risk_suppressions(tenant_id, alert_type);
ALTER TABLE risk_suppressions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON risk_suppressions
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE risk_feedback (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    alert_id    uuid,
    disposition text NOT NULL,
    note        text,
    created_by  uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_risk_feedback_tenant ON risk_feedback(tenant_id);
ALTER TABLE risk_feedback ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON risk_feedback
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('risk_rule.tune',       'Tune risk rule thresholds',     'risk', false),
    ('risk_alert.suppress',  'Suppress risk alerts',          'risk', false),
    ('risk_governance.admin','Risk governance and data quality', 'risk', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('risk_rule.tune', 'risk_alert.suppress', 'risk_governance.admin')
  AND r.code IN ('system_admin', 'executive', 'auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
