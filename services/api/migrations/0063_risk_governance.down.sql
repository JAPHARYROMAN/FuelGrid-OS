-- Reverse of 0063_risk_governance.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('risk_rule.tune', 'risk_alert.suppress', 'risk_governance.admin'));
DELETE FROM permissions WHERE code IN ('risk_rule.tune', 'risk_alert.suppress', 'risk_governance.admin');

DROP TABLE IF EXISTS risk_feedback;
DROP TABLE IF EXISTS risk_suppressions;
