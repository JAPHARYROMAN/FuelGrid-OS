-- Reverse of 0028_suppliers.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'supplier.manage');
DELETE FROM permissions WHERE code = 'supplier.manage';

DROP TABLE IF EXISTS supplier_products;
DROP TABLE IF EXISTS suppliers;
