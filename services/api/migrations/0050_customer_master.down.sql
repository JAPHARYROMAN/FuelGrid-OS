-- Reverse of 0050_customer_master.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'customer.manage');
DELETE FROM permissions WHERE code = 'customer.manage';

DROP TABLE IF EXISTS customer_contacts;

ALTER TABLE customers DROP CONSTRAINT chk_customers_status;
ALTER TABLE customers ADD CONSTRAINT chk_customers_status
    CHECK (status IN ('active', 'inactive', 'deleted'));

ALTER TABLE customers
    DROP COLUMN legal_name,
    DROP COLUMN trading_name,
    DROP COLUMN tax_id,
    DROP COLUMN billing_address,
    DROP COLUMN account_type,
    DROP COLUMN default_terms_days,
    DROP COLUMN notes;
