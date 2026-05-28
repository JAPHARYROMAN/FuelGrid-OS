-- Reverse of 0037_accounts.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('account.manage', 'finance.read'));
DELETE FROM permissions WHERE code IN ('account.manage', 'finance.read');

DROP TABLE IF EXISTS accounts;
