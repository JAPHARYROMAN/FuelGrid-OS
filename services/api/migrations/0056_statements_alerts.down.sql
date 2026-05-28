-- Reverse of 0056_statements_alerts.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('customer_statement.issue', 'customer_credit_alert.manage'));
DELETE FROM permissions WHERE code IN ('customer_statement.issue', 'customer_credit_alert.manage');

DROP TABLE IF EXISTS customer_credit_alerts;
DROP TABLE IF EXISTS customer_statements;
