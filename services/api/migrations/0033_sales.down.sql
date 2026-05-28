-- Reverse of 0033_sales.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'revenue.read');
DELETE FROM permissions WHERE code = 'revenue.read';

DROP TABLE IF EXISTS sales;
