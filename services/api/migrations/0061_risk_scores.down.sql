-- Reverse of 0061_risk_scores.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'risk_score.admin');
DELETE FROM permissions WHERE code = 'risk_score.admin';

DROP TABLE IF EXISTS risk_scores;
