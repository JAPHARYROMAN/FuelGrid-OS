-- Reverse 0109_finance_report_live: remove the Finance report row and the four
-- financial-statement sub-report rows, and revert the Finance category to its
-- 0105 seed (live, pointing at the profitability route).
DELETE FROM reports
WHERE tenant_id IS NULL
  AND key IN ('finance', 'trial-balance', 'profit-loss', 'balance-sheet', 'general-ledger');

UPDATE report_categories
SET availability = 'live',
    target_route = '/reports/profitability'
WHERE tenant_id IS NULL AND key = 'finance';
