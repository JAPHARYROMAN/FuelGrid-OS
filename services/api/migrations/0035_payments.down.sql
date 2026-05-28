-- Reverse of 0035_payments.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'payment.record');
DELETE FROM permissions WHERE code = 'payment.record';

DROP TABLE IF EXISTS payments;
