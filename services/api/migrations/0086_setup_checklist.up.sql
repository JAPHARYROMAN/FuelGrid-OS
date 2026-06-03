-- 0086_setup_checklist: persisted tenant setup checklist state.
--
-- The checklist combines live readiness facts (companies, stations, tanks,
-- opening stock, workforce rotation, ...) with an explicit per-step review
-- state. Live data answers "is the prerequisite present"; this table records
-- whether an operator has acknowledged/completed/skipped the step in the setup
-- workflow so onboarding progress survives reloads and sessions.

CREATE TABLE setup_steps (
    tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    code         text NOT NULL,
    status       text NOT NULL DEFAULT 'pending',
    completed_by uuid,
    completed_at timestamptz,
    updated_by   uuid,
    notes        text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, code),

    CONSTRAINT chk_setup_steps_status
        CHECK (status IN ('pending', 'completed', 'skipped')),
    CONSTRAINT setup_steps_completed_by_fk
        FOREIGN KEY (tenant_id, completed_by) REFERENCES users(tenant_id, id) ON DELETE SET NULL,
    CONSTRAINT setup_steps_updated_by_fk
        FOREIGN KEY (tenant_id, updated_by) REFERENCES users(tenant_id, id) ON DELETE SET NULL
);

CREATE INDEX idx_setup_steps_tenant_id ON setup_steps(tenant_id);

CREATE TRIGGER setup_steps_set_updated_at
    BEFORE UPDATE ON setup_steps
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE setup_steps ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON setup_steps
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Setup permissions are tenant-wide because the checklist contains tenant
-- catalogue steps and station-scoped readiness summaries. Row visibility for
-- station-restricted users is still enforced by the API's station filter when
-- computing physical setup counts.
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('setup.read',   'View tenant setup checklist and readiness blockers', 'setup', false),
    ('setup.manage', 'Update tenant setup checklist completion state',      'setup', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system
  AND (
    (p.code = 'setup.read' AND r.code IN (
        'system_admin', 'regional_manager', 'station_manager', 'supervisor',
        'finance_officer', 'procurement_officer', 'executive', 'auditor'
    ))
    OR
    (p.code = 'setup.manage' AND r.code IN (
        'system_admin', 'regional_manager', 'station_manager', 'executive'
    ))
  )
ON CONFLICT (role_id, permission_id) DO NOTHING;
