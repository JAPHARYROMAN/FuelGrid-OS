-- Reverse 0086_setup_checklist.

DROP TABLE IF EXISTS setup_steps;

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('setup.read', 'setup.manage'));
DELETE FROM permissions WHERE code IN ('setup.read', 'setup.manage');
