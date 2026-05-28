-- 0049_accounting_exports: reproducible accounting exports (Phase 7, Stage 14).
--
-- Each export run records what was generated (type, format, filters), a
-- content checksum, the actor, and whether the data was provisional (any
-- covered period still open) versus final (locked). Re-running the same
-- locked-period export reproduces the same checksum.

CREATE TABLE accounting_exports (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    export_type  text NOT NULL,
    format       text NOT NULL DEFAULT 'csv',
    filters      jsonb NOT NULL DEFAULT '{}'::jsonb,
    row_count    integer NOT NULL DEFAULT 0,
    checksum     text NOT NULL,
    provisional  boolean NOT NULL DEFAULT true,
    generated_by uuid NOT NULL,
    generated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_accounting_export_type
        CHECK (export_type IN ('journal_entries', 'trial_balance', 'ap_aging', 'ar_aging')),
    CONSTRAINT accounting_exports_generated_by_fk
        FOREIGN KEY (tenant_id, generated_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_accounting_exports_tenant ON accounting_exports(tenant_id);

ALTER TABLE accounting_exports ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON accounting_exports
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: finance.export (sensitive; tenant-wide).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('finance.export', 'Generate accounting exports', 'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'finance.export'
  AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
