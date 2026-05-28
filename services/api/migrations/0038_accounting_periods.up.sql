-- 0038_accounting_periods: posting-control periods (Phase 7, Stage 2).
--
-- Finance writes resolve the period covering their entry date and refuse to
-- post into closed/locked periods. Periods may not overlap within a tenant —
-- enforced by a GiST exclusion over the date range.

CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE accounting_periods (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    start_date date NOT NULL,
    end_date   date NOT NULL,
    status     text NOT NULL DEFAULT 'open',
    closed_by  uuid,
    closed_at  timestamptz,
    locked_by  uuid,
    locked_at  timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_period_status CHECK (status IN ('open', 'closing', 'closed', 'locked')),
    CONSTRAINT chk_period_dates  CHECK (end_date >= start_date),

    -- No two periods for a tenant may cover overlapping dates.
    CONSTRAINT accounting_periods_no_overlap
        EXCLUDE USING gist (
            tenant_id WITH =,
            daterange(start_date, end_date, '[]') WITH &&
        )
);

CREATE INDEX idx_accounting_periods_tenant_id ON accounting_periods(tenant_id);
ALTER TABLE accounting_periods ADD CONSTRAINT uq_accounting_periods_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER accounting_periods_set_updated_at
    BEFORE UPDATE ON accounting_periods
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE accounting_periods ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON accounting_periods
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: period.manage (create/reopen), period.close, period.reopen.
-- period.lock already exists (0004).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('period.manage', 'Create and manage accounting periods', 'finance', false),
    ('period.close',  'Close accounting periods',             'finance', false),
    ('period.reopen', 'Reopen closed accounting periods',     'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('period.manage', 'period.close', 'period.reopen')
  AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
