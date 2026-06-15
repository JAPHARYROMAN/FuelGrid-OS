-- 0116_report_templates: the Custom Report Builder's saved TEMPLATES (Reports
-- Center Phase 11 — blueprint §6 "Custom Report Builder", §22 report_templates).
--
-- A report_template is a SAVED, validated builder spec: a dataset key + the chosen
-- dimensions / measures / filters / sort / visualization, persisted as jsonb so it
-- can be re-run, exported, snapshotted, scheduled and shared. The spec is ALWAYS
-- validated against the curated dataset registry (internal/reportbuilder) before
-- it is saved — the column stores only allowlisted identifiers, never free SQL.
--
-- The builder composes queries ONLY from the whitelisted registry; there is NO
-- raw SQL anywhere in this feature. This table holds the (safe) spec metadata, the
-- derived required_permission (the dataset's permission, plus the sensitive
-- permission when the spec selects a sensitive measure), the creator, and the
-- share scope. Running a template re-checks the permission at run time, so a
-- shared template never lets a viewer read data they could not read live.

-- ---------------------------------------------------------------------------
-- reports.builder — the tenant-wide permission to manage (save/share/delete)
-- custom report templates. Running a template additionally requires the
-- underlying dataset's own permission, re-checked at run time. Granted to the
-- management/reporting roles that already hold reports.read + a reason to build.
-- (Previewing a spec rides reports.builder too; the dataset permission is the
-- authoritative data gate either way.)
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('reports.builder', 'Build, save and share custom report templates', 'reports', false)
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'reports.builder'
  AND r.code IN (
      'system_admin', 'finance_officer', 'regional_manager', 'executive', 'station_manager'
  )
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- report_templates — one saved builder spec.
--   dataset_key          the registry dataset the spec targets (validated in app).
--   spec                 jsonb: the validated builder spec (dims / measures /
--                        filters / sort / viz). Only allowlisted identifiers.
--   required_permission  the dataset's own permission — the run-time gate.
--   sensitive_permission the gating permission for sensitive columns the spec
--                        selects (margin.view), NULL when the spec selects none.
--   shared_scope         private | tenant | role. A private template is visible
--                        only to its creator; tenant is visible to every reports
--                        user in the tenant; role limits visibility to the role
--                        codes in shared_roles.
--   shared_roles         text[] of role codes for a 'role'-scoped share.
--   is_system            a seeded/platform template (reserved; none seeded today).
-- ---------------------------------------------------------------------------
CREATE TABLE report_templates (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,

    name                 text NOT NULL,
    description          text,

    dataset_key          text NOT NULL,
    spec                 jsonb NOT NULL DEFAULT '{}'::jsonb,

    required_permission  text NOT NULL,
    sensitive_permission text,

    shared_scope         text NOT NULL DEFAULT 'private',
    shared_roles         text[] NOT NULL DEFAULT '{}',

    created_by           uuid REFERENCES users(id) ON DELETE SET NULL,
    is_system            boolean NOT NULL DEFAULT false,

    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_report_templates_tenant_name UNIQUE (tenant_id, name),
    CONSTRAINT chk_report_templates_scope
        CHECK (shared_scope IN ('private', 'tenant', 'role'))
);

-- The management list (newest first) and the per-creator "my templates" filter.
CREATE INDEX idx_report_templates_tenant ON report_templates (tenant_id, created_at DESC);
CREATE INDEX idx_report_templates_creator ON report_templates (tenant_id, created_by);

CREATE TRIGGER report_templates_set_updated_at
    BEFORE UPDATE ON report_templates
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- RLS: strict tenant isolation (mirrors report_rules). A template is only ever
-- visible/writable within its own tenant; the SHARE scope (private/tenant/role) is
-- enforced in the application layer on top of this, since it is a per-actor
-- decision RLS cannot express.
ALTER TABLE report_templates ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON report_templates
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Flip the Custom category from 'placeholder' to 'live' now that the builder
-- exists, and register its catalog entries (datasets surface + saved templates).
-- The category was seeded in 0105 as a placeholder system row (tenant_id IS NULL).
-- ---------------------------------------------------------------------------
UPDATE report_categories
   SET availability = 'live',
       required_permission = 'reports.builder',
       target_route = '/reports/builder',
       updated_at = now()
 WHERE tenant_id IS NULL AND key = 'custom';

INSERT INTO reports
    (tenant_id, category_key, key, name, description, endpoint, required_permission, availability, is_system)
VALUES
    (NULL, 'custom', 'builder-datasets', 'Dataset Registry', 'The curated datasets you can build a report from, with their dimensions, measures and filters.', '/api/v1/reports/builder/datasets', 'reports.builder', 'live', true),
    (NULL, 'custom', 'builder-templates', 'Saved Reports', 'Your saved custom report templates: run, export, schedule or share them.', '/api/v1/reports/builder/templates', 'reports.builder', 'live', true)
ON CONFLICT (key) WHERE tenant_id IS NULL DO NOTHING;
