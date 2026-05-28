-- Reverse of 0025_deliveries.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'delivery.receive');
DELETE FROM permissions WHERE code = 'delivery.receive';

DROP TABLE IF EXISTS deliveries;
