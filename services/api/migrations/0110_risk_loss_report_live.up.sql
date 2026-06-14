-- 0110_risk_loss_report_live: point the Risk & Loss catalog category at the
-- signature Risk & Loss intelligence report and register its report row
-- (Reports §5.11 / §20.4).
--
-- Phase 1 (migration 0105) seeded the Risk & Loss category as `live` pointing at
-- `/reports/fuel-loss` (the original loss report) and registered the `fuel-loss`
-- catalog row. Phase 9 ships the signature Risk & Loss report: a station-scoped
-- structured endpoint (GET /api/v1/reports/risk-loss, gated by
-- reconciliation.read) returning the §5.11 envelope — a loss litres + value /
-- variance % / open-alerts / open-investigations / repeated-incident /
-- highest-risk-station KPI hero, the DETERMINISTIC pattern intelligence (variance
-- events by station / product / pump / shift / attendant turned into "% of related
-- events" findings), a risk heatmap, loss trend, station risk ranking, root-cause
-- distribution donut, alert-severity board and investigation timeline, and a
-- read-only risk-rules tuning context — plus its premium two-column page at
-- /reports/risk-loss. The loss VALUE is sensitive: it is margin.view-gated
-- in-handler and OMITTED for non-holders.
--
-- This migration makes the catalog HONEST:
--   1. the Risk & Loss category now targets the real /reports/risk-loss page;
--   2. the new structured report is registered as a catalog row under the
--      risk-loss category so it surfaces as an accessible report. The original
--      fuel-loss row from 0105 stays (the endpoint remains mounted and
--      authoritative). No money figure is stored here — catalog is metadata only.

UPDATE report_categories
SET availability = 'live',
    target_route = '/reports/risk-loss'
WHERE tenant_id IS NULL AND key = 'risk-loss';

INSERT INTO reports
    (tenant_id, category_key, key, name, description, endpoint, required_permission, availability, is_system)
VALUES
    (NULL, 'risk-loss', 'risk-loss', 'Risk & Loss Intelligence',
     'Premium risk and loss intelligence for a station: loss litres and value (value requires the margin permission), variance %, open risk alerts and investigations, repeated incidents and the highest-risk station, with deterministic pattern intelligence (variance events by station, product, pump, shift and attendant turned into "% of related events" findings), a risk heatmap, loss trend, station risk ranking, root-cause distribution donut, alert-severity board and investigation timeline, and a read-only view of the risk rules driving the alerts.',
     '/api/v1/reports/risk-loss', 'reconciliation.read', 'live', true)
ON CONFLICT (key) WHERE tenant_id IS NULL DO UPDATE SET
    category_key        = EXCLUDED.category_key,
    name                = EXCLUDED.name,
    description         = EXCLUDED.description,
    endpoint            = EXCLUDED.endpoint,
    required_permission = EXCLUDED.required_permission,
    availability        = EXCLUDED.availability;
