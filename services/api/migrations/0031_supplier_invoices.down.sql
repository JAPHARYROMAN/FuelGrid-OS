-- Reverse of 0031_supplier_invoices.

DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('invoice.manage', 'invoice.approve')
);
DELETE FROM permissions WHERE code IN ('invoice.manage', 'invoice.approve');

DROP TABLE IF EXISTS procurement_discrepancies;
DROP TABLE IF EXISTS supplier_invoice_lines;
DROP TABLE IF EXISTS supplier_invoices;
