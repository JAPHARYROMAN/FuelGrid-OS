-- Reverse of 0044_bank_statements.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'bank_statement.manage');
DELETE FROM permissions WHERE code = 'bank_statement.manage';

DROP TABLE IF EXISTS bank_statement_lines;
DROP TABLE IF EXISTS bank_statement_imports;
