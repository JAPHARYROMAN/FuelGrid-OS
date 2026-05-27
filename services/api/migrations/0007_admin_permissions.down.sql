DELETE FROM permissions WHERE code IN (
    'companies.manage',
    'regions.manage',
    'users.invite',
    'sessions.revoke'
);
