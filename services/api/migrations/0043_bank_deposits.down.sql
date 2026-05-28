-- Reverse of 0043_bank_deposits.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('bank_account.manage', 'bank_deposit.manage', 'bank_deposit.confirm'));
DELETE FROM permissions WHERE code IN ('bank_account.manage', 'bank_deposit.manage', 'bank_deposit.confirm');

DROP TABLE IF EXISTS bank_deposit_lines;
DROP TABLE IF EXISTS bank_deposits;
DROP TABLE IF EXISTS bank_accounts;
