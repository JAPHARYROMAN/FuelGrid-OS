-- 0095_enterprise_scope_switch: the permission that gates the enterprise
-- context scope-switcher (Feature 13.1). A user holding this permission may
-- list the company / region / station scopes their enterprise grants resolve
-- to and switch the active reporting scope they view the chain through.
--
-- The switch is a read-time lens only: scoped reads continue to enforce the
-- user's station access server-side (Postgres RLS + policy.Can), so picking a
-- scope can never widen what a user may see — it only narrows the view to a
-- subset they are already entitled to. The permission therefore guards the
-- scope-listing endpoint (which enumerates the user's own grants), not data
-- access itself.
--
-- Seeding mirrors 0057_enterprise_governance: tenant-wide (station_scoped =
-- false), category 'enterprise', granted to the same enterprise-capable system
-- roles. ON CONFLICT keeps the migration idempotent against re-runs.

INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('enterprise.scope.switch', 'Switch the active enterprise reporting scope', 'enterprise', false)
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'enterprise.scope.switch'
  AND r.code IN ('system_admin', 'regional_manager', 'executive')
ON CONFLICT (role_id, permission_id) DO NOTHING;
