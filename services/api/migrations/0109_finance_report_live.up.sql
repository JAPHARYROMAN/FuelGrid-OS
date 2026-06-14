-- 0109_finance_report_live: point the Finance catalog category at the signature
-- Finance P&L report and register its report row + the embedded financial
-- statement sub-reports (Reports §5.8).
--
-- Phase 1 (migration 0105) seeded the Finance category as `live` pointing at
-- `/reports/profitability` (the station P&L by product) and registered the
-- profitability / cash-reconciliation / financials catalog rows. Phase 8 ships
-- the signature Finance report: a station-scoped structured endpoint
-- (GET /api/v1/reports/finance, gated by finance.read) returning the §5.8
-- envelope — a revenue / gross-margin / net-margin / expenses / cash-position
-- KPI hero, the P&L WATERFALL (revenue → COGS → gross margin → expenses → net
-- operating result), a per-product breakdown, settlement / accounting-period
-- status chips, the embedded financial statements and a drilldown to source
-- (journals / expenses / sales) — plus its premium two-column page at
-- /reports/finance. COGS / gross margin / net margin are sensitive: they are
-- margin.view-gated in-handler and OMITTED for non-holders.
--
-- This migration makes the catalog HONEST in two ways:
--   1. the Finance category now targets the real /reports/finance page;
--   2. the existing finance JSON statements (trial balance / P&L / balance sheet
--      / general ledger) — which already ship as /api/v1/finance/reports/*,
--      gated by finance.read — are registered as catalog rows under the Finance
--      category so they surface as accessible finance sub-reports instead of
--      being invisible. The profitability / cash-reconciliation / financials
--      rows from 0105 stay. No money figure is stored here — catalog is metadata.

UPDATE report_categories
SET availability = 'live',
    target_route = '/reports/finance'
WHERE tenant_id IS NULL AND key = 'finance';

INSERT INTO reports
    (tenant_id, category_key, key, name, description, endpoint, required_permission, availability, is_system)
VALUES
    (NULL, 'finance', 'finance', 'Finance (P&L)',
     'Premium profit-and-loss for a station over a period: net revenue, COGS, gross margin, operating expenses and net operating result as a money waterfall, with period comparison, cash position, accounting-period settlement status and the underlying financial statements. COGS, gross margin and net margin require the margin permission.',
     '/api/v1/reports/finance', 'finance.read', 'live', true),
    (NULL, 'finance', 'trial-balance', 'Trial Balance',
     'Debit and credit balances per ledger account as of a date, with a balanced check.',
     '/api/v1/finance/reports/trial-balance', 'finance.read', 'live', true),
    (NULL, 'finance', 'profit-loss', 'Profit & Loss',
     'Revenue, expenses and net profit over a date range from posted journal lines.',
     '/api/v1/finance/reports/profit-loss', 'finance.read', 'live', true),
    (NULL, 'finance', 'balance-sheet', 'Balance Sheet',
     'Assets, liabilities, equity, retained earnings and net income as of a date, with a balanced check.',
     '/api/v1/finance/reports/balance-sheet', 'finance.read', 'live', true),
    (NULL, 'finance', 'general-ledger', 'General Ledger',
     'Posted journal lines for a chosen account: entry number, date, source, memo, debit and credit.',
     '/api/v1/finance/reports/general-ledger', 'finance.read', 'live', true)
ON CONFLICT (key) WHERE tenant_id IS NULL DO UPDATE SET
    category_key        = EXCLUDED.category_key,
    name                = EXCLUDED.name,
    description         = EXCLUDED.description,
    endpoint            = EXCLUDED.endpoint,
    required_permission = EXCLUDED.required_permission,
    availability        = EXCLUDED.availability;
