-- Reverse 0081_mpesa_transactions.

DROP TABLE IF EXISTS mpesa_transactions;

-- role_permissions FK to permissions does NOT cascade, so unassign the
-- permission from every role before deleting it.
DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'payment.mpesa.manage');
DELETE FROM permissions WHERE code = 'payment.mpesa.manage';
