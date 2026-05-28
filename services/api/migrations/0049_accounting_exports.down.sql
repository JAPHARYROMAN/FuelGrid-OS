-- Reverse of 0049_accounting_exports.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'finance.export');
DELETE FROM permissions WHERE code = 'finance.export';

DROP TABLE IF EXISTS accounting_exports;
