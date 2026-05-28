-- Reverse of 0042_cash_reconciliations.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('cash_reconciliation.manage', 'cash_reconciliation.approve'));
DELETE FROM permissions WHERE code IN ('cash_reconciliation.manage', 'cash_reconciliation.approve');

DROP TABLE IF EXISTS cash_reconciliation_lines;
DROP TABLE IF EXISTS cash_reconciliations;
