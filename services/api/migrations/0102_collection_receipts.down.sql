DELETE FROM role_permissions rp
USING permissions p
WHERE rp.permission_id = p.id AND p.code = 'cash.confirm';
DELETE FROM permissions WHERE code = 'cash.confirm';
DROP TABLE IF EXISTS collection_receipts;
ALTER TABLE cash_submissions DROP CONSTRAINT IF EXISTS uq_cash_submissions_tenant_id;
