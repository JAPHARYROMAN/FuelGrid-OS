-- Reverse of 0060_risk_foundation.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('risk_signal.admin', 'risk_rule.manage', 'risk_alert.read', 'risk_alert.manage', 'risk.read'));
DELETE FROM permissions WHERE code IN ('risk_signal.admin', 'risk_rule.manage', 'risk_alert.read', 'risk_alert.manage', 'risk.read');

DROP TABLE IF EXISTS risk_alerts;
DROP TABLE IF EXISTS risk_rules;
DROP TABLE IF EXISTS risk_signals;
