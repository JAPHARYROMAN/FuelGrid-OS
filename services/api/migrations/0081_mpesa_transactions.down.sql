-- Reverse 0081_mpesa_transactions.

DROP TABLE IF EXISTS mpesa_transactions;

-- role_permissions rows cascade off the permission delete via FK.
DELETE FROM permissions WHERE code = 'payment.mpesa.manage';
