-- 0007_admin_permissions: permissions and grants for the admin console.

INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('companies.manage', 'Create, edit, soft-delete companies', 'admin', false),
    ('regions.manage',   'Create, edit, soft-delete regions',   'admin', false),
    ('users.invite',     'Invite new users to the tenant',       'admin', false),
    ('sessions.revoke',  'Revoke another user''s sessions',      'admin', false);

-- Grants.
-- system_admin gets everything (already covered by the catch-all in 0004).
-- We still need explicit grants for the other relevant roles.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND (
    (r.code = 'system_admin'     AND p.code IN ('companies.manage', 'regions.manage', 'users.invite', 'sessions.revoke'))
    OR (r.code = 'executive'     AND p.code IN ('companies.manage', 'regions.manage'))
    OR (r.code = 'regional_manager' AND p.code = 'regions.manage')
);
