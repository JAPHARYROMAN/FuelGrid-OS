-- 0057_enterprise_governance: the structure to govern many stations from one
-- tenant (Phase 9, Stages 1-3) — optional station groups, delegated enterprise
-- scopes, and a generic policy-driven approval engine that workflows call
-- before finalizing high-value actions.

CREATE TABLE station_groups (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    name       text NOT NULL,
    kind       text,
    status     text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_station_group_status CHECK (status IN ('active', 'archived'))
);
CREATE INDEX idx_station_groups_tenant ON station_groups(tenant_id);
ALTER TABLE station_groups ADD CONSTRAINT uq_station_groups_tenant_id UNIQUE (tenant_id, id);
CREATE TRIGGER station_groups_set_updated_at BEFORE UPDATE ON station_groups FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE station_groups ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON station_groups
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE station_group_memberships (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_group_id uuid NOT NULL,
    station_id       uuid NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT sgm_group_fk   FOREIGN KEY (tenant_id, station_group_id) REFERENCES station_groups(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT sgm_station_fk FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX uq_sgm_member ON station_group_memberships(tenant_id, station_group_id, station_id);
CREATE INDEX idx_sgm_tenant ON station_group_memberships(tenant_id);
ALTER TABLE station_group_memberships ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON station_group_memberships
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Delegated enterprise scopes: a user is granted authority at tenant, company,
-- region, group, or station level. The effective allowed-station set is
-- resolved from these grants.
CREATE TABLE enterprise_scope_grants (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    user_id    uuid NOT NULL,
    scope_type text NOT NULL,
    scope_id   uuid,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_scope_type CHECK (scope_type IN ('tenant', 'company', 'region', 'group', 'station')),
    CONSTRAINT esg_user_fk FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_esg_tenant ON enterprise_scope_grants(tenant_id);
CREATE INDEX idx_esg_user   ON enterprise_scope_grants(user_id);
ALTER TABLE enterprise_scope_grants ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON enterprise_scope_grants
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Generic approval engine.
CREATE TABLE approval_policies (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    workflow_type      text NOT NULL,
    min_amount         numeric(14, 2) NOT NULL DEFAULT 0,
    required_approvals integer NOT NULL DEFAULT 1,
    required_role      text,
    scope_type         text,
    scope_id           uuid,
    status             text NOT NULL DEFAULT 'active',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_approval_policy_status CHECK (status IN ('active', 'archived')),
    CONSTRAINT chk_approval_required CHECK (required_approvals >= 1)
);
CREATE INDEX idx_approval_policies_tenant ON approval_policies(tenant_id);
CREATE INDEX idx_approval_policies_wf     ON approval_policies(tenant_id, workflow_type);
CREATE TRIGGER approval_policies_set_updated_at BEFORE UPDATE ON approval_policies FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE approval_policies ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON approval_policies
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE approval_requests (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    workflow_type      text NOT NULL,
    reference_type     text,
    reference_id       uuid,
    amount             numeric(14, 2) NOT NULL DEFAULT 0,
    required_approvals integer NOT NULL DEFAULT 1,
    approvals_count    integer NOT NULL DEFAULT 0,
    status             text NOT NULL DEFAULT 'requested',
    station_id         uuid,
    requested_by       uuid NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_approval_request_status CHECK (status IN ('requested', 'approved', 'rejected', 'cancelled', 'expired')),
    CONSTRAINT approval_request_requested_by_fk FOREIGN KEY (tenant_id, requested_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_approval_requests_tenant ON approval_requests(tenant_id);
CREATE INDEX idx_approval_requests_status ON approval_requests(tenant_id, status);
ALTER TABLE approval_requests ADD CONSTRAINT uq_approval_requests_tenant_id UNIQUE (tenant_id, id);
CREATE TRIGGER approval_requests_set_updated_at BEFORE UPDATE ON approval_requests FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE approval_requests ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON approval_requests
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE approval_decisions (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    approval_request_id uuid NOT NULL,
    decision            text NOT NULL,
    comment             text,
    decided_by          uuid NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_approval_decision CHECK (decision IN ('approve', 'reject')),
    CONSTRAINT approval_decision_request_fk FOREIGN KEY (tenant_id, approval_request_id) REFERENCES approval_requests(tenant_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX uq_approval_decision_one ON approval_decisions(tenant_id, approval_request_id, decided_by);
CREATE INDEX idx_approval_decisions_tenant ON approval_decisions(tenant_id);
ALTER TABLE approval_decisions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON approval_decisions
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('enterprise_structure.manage', 'Manage station groups and hierarchy', 'enterprise', false),
    ('enterprise_access.manage',    'Manage enterprise scope grants',      'enterprise', false),
    ('enterprise_access.read',      'View enterprise access',              'enterprise', false),
    ('approval_policy.manage',      'Manage approval policies',            'enterprise', false),
    ('approval_request.decide',     'Decide approval requests',            'enterprise', false),
    ('enterprise.read',             'View enterprise dashboards',          'enterprise', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('enterprise_structure.manage', 'enterprise_access.manage', 'enterprise_access.read',
                                 'approval_policy.manage', 'approval_request.decide', 'enterprise.read')
  AND r.code IN ('system_admin', 'regional_manager', 'executive')
ON CONFLICT (role_id, permission_id) DO NOTHING;
