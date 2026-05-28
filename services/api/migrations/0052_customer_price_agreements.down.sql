-- Reverse of 0052_customer_price_agreements.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('customer_pricing.manage', 'customer_pricing.approve'));
DELETE FROM permissions WHERE code IN ('customer_pricing.manage', 'customer_pricing.approve');

DROP TABLE IF EXISTS customer_price_agreements;
