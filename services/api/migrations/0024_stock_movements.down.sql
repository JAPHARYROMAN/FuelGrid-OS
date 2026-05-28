-- Reverse of 0024_stock_movements.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'inventory.read');
DELETE FROM permissions WHERE code = 'inventory.read';

DROP TABLE IF EXISTS stock_movements;
