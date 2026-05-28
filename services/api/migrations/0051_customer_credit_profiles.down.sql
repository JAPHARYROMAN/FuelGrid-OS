-- Reverse of 0051_customer_credit_profiles.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('customer_credit.manage', 'customer_credit.override', 'customer_credit.read'));
DELETE FROM permissions WHERE code IN ('customer_credit.manage', 'customer_credit.override', 'customer_credit.read');

DROP TABLE IF EXISTS customer_credit_profiles;
