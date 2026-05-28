-- Reverse of 0046_customer_payments.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('customer_payment.manage', 'customer_payment.post'));
DELETE FROM permissions WHERE code IN ('customer_payment.manage', 'customer_payment.post');

DROP TABLE IF EXISTS customer_payment_allocations;
DROP TABLE IF EXISTS customer_payments;
