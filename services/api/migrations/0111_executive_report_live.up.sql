-- 0111_executive_report_live: point the Executive catalog category at the
-- signature Executive Business Report and register its report row
-- (Reports §5.1 / §20.1).
--
-- Phase 1 (migration 0105) seeded the Executive category as `partial` pointing
-- at `/reports/executive` (a skeleton: live KPIs, placeholder sections) because
-- no tenant-wide cross-domain rollup existed yet, and registered only the
-- `station-comparison` report row under it. Phase 10 ships the signature
-- Executive Business Report: the cross-domain leadership cockpit that
-- consolidates Phases 2-9 into one drillable view — a structured endpoint
-- (GET /api/v1/reports/executive, gated by finance.read held anywhere) returning
-- the §5.1 envelope: a company-wide (or scope-wide) revenue / litres / margin
-- (gated) / loss (value gated) / cash / stockout / risk / approvals / credit
-- (gated) KPI hero, the DETERMINISTIC §5.1 automated management narrative
-- (period-over-period prose, every sentence traceable to a computed figure — no
-- AI), and the reusable visuals (per-station ranking, P&L waterfall,
-- period-comparison cards, loss summary) — plus its premium two-column page at
-- /reports/executive. The cockpit aggregates ONLY the actor's permitted station
-- scope (cross-scope leakage is impossible), and MARGIN / LOSS VALUE / CREDIT
-- EXPOSURE are sensitive: omitted (not zeroed) for non-holders.
--
-- This migration makes the catalog HONEST:
--   1. the Executive category goes `live` and keeps targeting /reports/executive
--      (the page is now the real cockpit, no longer a skeleton);
--   2. the new structured report is registered as a catalog row under the
--      executive category so it surfaces as an accessible report. The existing
--      station-comparison row from 0105 stays (it remains its own report). No
--      money figure is stored here — catalog is metadata only.

UPDATE report_categories
SET availability = 'live',
    target_route = '/reports/executive'
WHERE tenant_id IS NULL AND key = 'executive';

INSERT INTO reports
    (tenant_id, category_key, key, name, description, endpoint, required_permission, availability, is_system)
VALUES
    (NULL, 'executive', 'executive', 'Executive Business Report',
     'The cross-domain leadership cockpit (blueprint 5.1 / 20.1): a company-wide or scope-wide rollup of revenue, litres, margin (margin permission required), fuel loss (value requires the margin permission), cash shortages, stockout risk, open risk alerts and investigations, pending approvals, supplier issues, top and underperforming stations and credit exposure (credit permission required), plus the deterministic automated management narrative (period-over-period prose, every sentence traceable to a computed figure — no AI), a per-station ranking, a network profit-and-loss waterfall, period-comparison cards and a loss summary. The cockpit aggregates only the stations you can access.',
     '/api/v1/reports/executive', 'finance.read', 'live', true)
ON CONFLICT (key) WHERE tenant_id IS NULL DO UPDATE SET
    category_key        = EXCLUDED.category_key,
    name                = EXCLUDED.name,
    description         = EXCLUDED.description,
    endpoint            = EXCLUDED.endpoint,
    required_permission = EXCLUDED.required_permission,
    availability        = EXCLUDED.availability;
