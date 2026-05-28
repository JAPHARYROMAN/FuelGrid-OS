-- Revert 0021: drop the override permissions and their role grants.
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE code IN ('reading.override', 'cash.override')
);
DELETE FROM permissions WHERE code IN ('reading.override', 'cash.override');
