-- Reverse 0106_sales_report_live: remove the Sales report catalog row and revert
-- the Sales category to its pre-Phase-3 `partial` state pointing at the old
-- sales-summary route (the 0105 seed values).
DELETE FROM reports WHERE tenant_id IS NULL AND key = 'sales';

UPDATE report_categories
SET availability = 'partial',
    target_route = '/reports/sales-summary'
WHERE tenant_id IS NULL AND key = 'sales';
