-- Reverse of 0034_customers_ar.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'customer.read');
DELETE FROM permissions WHERE code = 'customer.read';

DROP TABLE IF EXISTS ar_entries;
DROP TABLE IF EXISTS customers;
