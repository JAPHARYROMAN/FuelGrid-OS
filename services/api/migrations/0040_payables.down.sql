-- Reverse of 0040_payables.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('payable.read', 'payable.manage'));
DELETE FROM permissions WHERE code IN ('payable.read', 'payable.manage');

DROP TABLE IF EXISTS payables;
