-- Reverse of 0041_supplier_payments.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('supplier_payment.manage', 'supplier_payment.post'));
DELETE FROM permissions WHERE code IN ('supplier_payment.manage', 'supplier_payment.post');

DROP TABLE IF EXISTS supplier_payment_allocations;
DROP TABLE IF EXISTS supplier_payments;
