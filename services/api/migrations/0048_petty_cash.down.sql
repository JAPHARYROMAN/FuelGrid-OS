-- Reverse of 0048_petty_cash.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('petty_cash.manage', 'petty_cash.reconcile'));
DELETE FROM permissions WHERE code IN ('petty_cash.manage', 'petty_cash.reconcile');

DROP TABLE IF EXISTS petty_cash_reconciliations;
DROP TABLE IF EXISTS petty_cash_transactions;
DROP TABLE IF EXISTS petty_cash_floats;
