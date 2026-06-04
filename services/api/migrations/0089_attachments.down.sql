-- Reverse of 0089_attachments.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('attachment.read', 'attachment.manage'));
DELETE FROM permissions WHERE code IN ('attachment.read', 'attachment.manage');

DROP TABLE IF EXISTS attachments;
