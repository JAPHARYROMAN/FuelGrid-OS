-- Reverse of 0038_accounting_periods.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('period.manage', 'period.close', 'period.reopen'));
DELETE FROM permissions WHERE code IN ('period.manage', 'period.close', 'period.reopen');

DROP TABLE IF EXISTS accounting_periods;
