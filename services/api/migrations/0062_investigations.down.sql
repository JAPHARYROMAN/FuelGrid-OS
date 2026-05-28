-- Reverse of 0062_investigations.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('investigation.read', 'investigation.manage', 'investigation.close'));
DELETE FROM permissions WHERE code IN ('investigation.read', 'investigation.manage', 'investigation.close');

DROP TABLE IF EXISTS investigation_case_actions;
DROP TABLE IF EXISTS investigation_case_comments;
DROP TABLE IF EXISTS investigation_case_alerts;
DROP TABLE IF EXISTS investigation_cases;
