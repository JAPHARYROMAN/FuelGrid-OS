-- Reverse 0108_customer_credit_report_live: remove the Customer Credit report
-- row and revert the Customer Credit category to its 0105 seed (live, pointing
-- at the borrowed credit-cashflow route).
DELETE FROM reports WHERE tenant_id IS NULL AND key = 'customer-credit';

UPDATE report_categories
SET availability = 'live',
    target_route = '/reports/credit-cashflow'
WHERE tenant_id IS NULL AND key = 'customer-credit';
