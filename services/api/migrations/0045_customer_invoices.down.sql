-- Reverse of 0045_customer_invoices.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('customer_invoice.manage', 'customer_invoice.issue'));
DELETE FROM permissions WHERE code IN ('customer_invoice.manage', 'customer_invoice.issue');

DROP TABLE IF EXISTS customer_invoice_lines;
DROP TABLE IF EXISTS customer_invoices;
