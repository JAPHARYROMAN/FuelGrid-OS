-- 0062_investigations: turning alerts into accountable investigation cases
-- (Phase 10, Stages 11-13). Cases group alerts with a comment trail and
-- recommended actions; closing records a resolution. Evidence references are
-- immutable — the case timeline pulls case-scoped events in order.

CREATE TABLE investigation_cases (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    title       text NOT NULL,
    case_type   text NOT NULL DEFAULT 'other',
    status      text NOT NULL DEFAULT 'open',
    severity    text NOT NULL DEFAULT 'medium',
    assigned_to uuid,
    resolution  text,
    opened_by   uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_case_status CHECK (status IN ('open', 'assigned', 'in_review', 'action_required', 'resolved', 'closed')),
    CONSTRAINT chk_case_severity CHECK (severity IN ('info', 'low', 'medium', 'high', 'critical')),
    CONSTRAINT case_opened_by_fk FOREIGN KEY (tenant_id, opened_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_cases_tenant ON investigation_cases(tenant_id, status);
ALTER TABLE investigation_cases ADD CONSTRAINT uq_cases_tenant_id UNIQUE (tenant_id, id);
CREATE TRIGGER investigation_cases_set_updated_at BEFORE UPDATE ON investigation_cases FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE investigation_cases ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON investigation_cases
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE investigation_case_alerts (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    case_id    uuid NOT NULL,
    alert_id   uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ica_case_fk FOREIGN KEY (tenant_id, case_id) REFERENCES investigation_cases(tenant_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX uq_ica_link ON investigation_case_alerts(tenant_id, case_id, alert_id);
CREATE INDEX idx_ica_tenant ON investigation_case_alerts(tenant_id);
ALTER TABLE investigation_case_alerts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON investigation_case_alerts
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE investigation_case_comments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    case_id    uuid NOT NULL,
    body       text NOT NULL,
    author_id  uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT icc_case_fk FOREIGN KEY (tenant_id, case_id) REFERENCES investigation_cases(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_icc_case ON investigation_case_comments(tenant_id, case_id);
ALTER TABLE investigation_case_comments ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON investigation_case_comments
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE investigation_case_actions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    case_id     uuid NOT NULL,
    action_type text NOT NULL,
    status      text NOT NULL DEFAULT 'suggested',
    detail      text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_action_status CHECK (status IN ('suggested', 'accepted', 'completed', 'dismissed')),
    CONSTRAINT ica_action_case_fk FOREIGN KEY (tenant_id, case_id) REFERENCES investigation_cases(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_ica_action_case ON investigation_case_actions(tenant_id, case_id);
CREATE TRIGGER investigation_case_actions_set_updated_at BEFORE UPDATE ON investigation_case_actions FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE investigation_case_actions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON investigation_case_actions
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('investigation.read',   'View investigation cases',   'risk', false),
    ('investigation.manage', 'Manage investigation cases', 'risk', false),
    ('investigation.close',  'Close investigation cases',  'risk', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('investigation.read', 'investigation.manage', 'investigation.close')
  AND r.code IN ('system_admin', 'regional_manager', 'executive', 'auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
