-- 0106_sales_report_live: flip the Sales catalog category LIVE (Reports §5.2).
--
-- Phase 1 (migration 0105) seeded the Sales category as `partial` pointing at
-- `/reports/sales-summary` (which was only a redirect), with NO backing report
-- row — there was no tenant-wide sales report endpoint yet. Phase 3 ships the
-- signature Sales report: a station-scoped structured endpoint
-- (GET /api/v1/reports/sales, revenue.read) returning the §5.2 envelope
-- (litres / revenue / avg-price / txn-count / growth KPIs, the revenue trend,
-- product / payment / shift / attendant / nozzle breakdowns, a peak-hours grid
-- and an optional cross-station ranking) plus its premium page at /reports/sales.
--
-- So this migration makes the catalog HONEST: the Sales category becomes `live`
-- and targets the real page, and a `sales` report row is registered under it. No
-- money or litre figure is stored here — the catalog is metadata only.

UPDATE report_categories
SET availability = 'live',
    target_route = '/reports/sales'
WHERE tenant_id IS NULL AND key = 'sales';

INSERT INTO reports
    (tenant_id, category_key, key, name, description, endpoint, required_permission, availability, is_system)
VALUES
    (NULL, 'sales', 'sales', 'Sales',
     'Litres, revenue, average selling price, transaction count and growth, with product mix, payment breakdown, shift / attendant / nozzle drill-down and a peak-hours heatmap.',
     '/api/v1/reports/sales', 'revenue.read', 'live', true)
ON CONFLICT (key) WHERE tenant_id IS NULL DO UPDATE SET
    category_key        = EXCLUDED.category_key,
    name                = EXCLUDED.name,
    description         = EXCLUDED.description,
    endpoint            = EXCLUDED.endpoint,
    required_permission = EXCLUDED.required_permission,
    availability        = EXCLUDED.availability;
