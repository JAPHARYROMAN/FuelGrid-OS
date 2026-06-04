-- 0090_retention: data lifecycle / retention governance (Feature 13.2).
--
-- Two tenant-scoped governance surfaces:
--
--   1. retention_policies — a tenant's declared retention window per data scope
--      (audit | session | export). One row per scope (unique per tenant). A
--      policy says "keep <scope> data for retention_days days"; the retention
--      sweep job reads these. The job in this slice LOGS its intent and the
--      candidate row count — it does NOT purge yet (the audit ledger and other
--      sources are append-only/immutable and purging them needs its own
--      hardening pass); the policy + job-run ledger are the durable half.
--
--   2. closed_period_change_requests — a maker-checker workflow to reopen or
--      relock a CLOSED/LOCKED accounting period (migration 0038). Re-using a
--      closed period is a controlled, audited event: someone REQUESTS the change
--      with a reason, a DIFFERENT person approves or rejects it (separation of
--      duties, enforced in the repo under a row lock). Approving the request does
--      NOT itself transition the period — it authorizes a finance officer to run
--      the existing period reopen/lock endpoint; this row is the governance
--      record of the decision.
--
-- New permissions retention.manage (manage retention policies) and
-- closed_period.change (request/decide closed-period change requests) are seeded
-- and granted below, mirroring migration 0004 / 0038's grant pattern: the
-- finance-equivalent real system roles are system_admin and finance_officer.

-- ---------------------------------------------------------------------------
-- retention_policies — per-scope retention window for a tenant.
-- ---------------------------------------------------------------------------
CREATE TABLE retention_policies (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    scope          text NOT NULL,
    -- How many days of <scope> data to keep. Positive integer; the sweep treats
    -- rows older than now() - retention_days as purge candidates.
    retention_days integer NOT NULL,
    status         text NOT NULL DEFAULT 'active',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_retention_scope  CHECK (scope IN ('audit', 'session', 'export')),
    CONSTRAINT chk_retention_status CHECK (status IN ('active', 'disabled')),
    CONSTRAINT chk_retention_days   CHECK (retention_days > 0),
    -- One policy per scope per tenant.
    CONSTRAINT uq_retention_tenant_scope UNIQUE (tenant_id, scope)
);

CREATE INDEX idx_retention_policies_tenant ON retention_policies(tenant_id);

CREATE TRIGGER retention_policies_set_updated_at
    BEFORE UPDATE ON retention_policies
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE retention_policies ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON retention_policies
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- closed_period_change_requests — maker-checker to reopen/relock a closed
-- accounting period.
-- ---------------------------------------------------------------------------
CREATE TABLE closed_period_change_requests (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    period_id    uuid NOT NULL,
    -- What change the requester wants applied to the closed period.
    change_type  text NOT NULL,
    reason       text NOT NULL,
    status       text NOT NULL DEFAULT 'requested',
    requested_by uuid NOT NULL,
    decided_by   uuid,
    decision_note text,
    requested_at timestamptz NOT NULL DEFAULT now(),
    decided_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_cpcr_change_type CHECK (change_type IN ('reopen', 'relock')),
    CONSTRAINT chk_cpcr_status      CHECK (status IN ('requested', 'approved', 'rejected')),
    -- The period must belong to the same tenant (FK rides accounting_periods'
    -- (tenant_id, id) unique key from migration 0038).
    CONSTRAINT cpcr_period_fk
        FOREIGN KEY (tenant_id, period_id) REFERENCES accounting_periods(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT cpcr_requested_by_fk
        FOREIGN KEY (tenant_id, requested_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT cpcr_decided_by_fk
        FOREIGN KEY (tenant_id, decided_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_cpcr_tenant ON closed_period_change_requests(tenant_id);
CREATE INDEX idx_cpcr_period ON closed_period_change_requests(period_id);
CREATE INDEX idx_cpcr_status ON closed_period_change_requests(tenant_id, status);

-- A period may carry at most ONE pending (requested) change request at a time:
-- this partial unique index makes "no duplicate pending request" a hard DB
-- invariant. A decided (approved/rejected) request leaves the period free to be
-- requested again.
CREATE UNIQUE INDEX uq_cpcr_pending
    ON closed_period_change_requests(tenant_id, period_id) WHERE status = 'requested';

CREATE TRIGGER cpcr_set_updated_at
    BEFORE UPDATE ON closed_period_change_requests
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE closed_period_change_requests ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON closed_period_change_requests
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: retention.manage (manage retention policies) and
-- closed_period.change (request/decide closed-period change requests). Both
-- tenant-wide (station_scoped=false). NEW in this migration — granted to the
-- finance-equivalent real system roles (system_admin, finance_officer) seeded
-- in 0004, mirroring 0038's period.* grant.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('retention.manage',     'Manage data retention policies',                 'admin',   false),
    ('closed_period.change', 'Request or decide closed-period change requests', 'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('retention.manage', 'closed_period.change')
  AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
