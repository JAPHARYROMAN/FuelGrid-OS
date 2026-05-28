-- Reverse of 0032_price_changes.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'pricing.read');
DELETE FROM permissions WHERE code = 'pricing.read';

DROP TABLE IF EXISTS price_changes;
