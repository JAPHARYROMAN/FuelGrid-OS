-- Reverse of 0009_products.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'products.manage');
DELETE FROM permissions WHERE code = 'products.manage';

-- Dropping the table also drops its policy, trigger, indexes, and the
-- uq_products_tenant_id constraint.
DROP TABLE IF EXISTS products;
