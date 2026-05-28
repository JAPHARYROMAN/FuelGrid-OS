-- Delete the dependent grants first; role_permissions.permission_id has
-- ON DELETE RESTRICT, so the permissions can only be removed once no
-- role_permissions row references them.
DELETE FROM role_permissions rp
USING permissions p
WHERE rp.permission_id = p.id
  AND p.code IN (
      'companies.manage',
      'regions.manage',
      'users.invite',
      'sessions.revoke'
  );

DELETE FROM permissions WHERE code IN (
    'companies.manage',
    'regions.manage',
    'users.invite',
    'sessions.revoke'
);
