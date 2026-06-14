-- 0107_delivery_report_live: register the Delivery & Procurement report and
-- point the Delivery catalog category at it (Reports §5.7).
--
-- Phase 1 (migration 0105) seeded the Delivery category as `live` but pointed at
-- `/reports/station-close` (a borrowed route — there was no delivery report yet)
-- with NO backing report row. Phase 6 ships the signature Delivery & Procurement
-- report: a station-scoped structured endpoint (GET /api/v1/reports/delivery,
-- gated by station.read) returning the §5.7 envelope (ordered/loaded/received
-- comparison, delivery variance, delivery delays, the procurement pipeline, a
-- deterministic supplier scorecard, and supplier-cost figures gated behind
-- margin.view) plus its premium page at /reports/delivery.
--
-- So this migration makes the catalog HONEST: the Delivery category targets the
-- real page, and a `delivery` report row is registered under it. The Procurement
-- category stays `partial` (its payable.read holders still have no payable.read-
-- gated report page — the delivery report needs station.read — so we never
-- advertise a 403-bound link). No money or litre figure is stored here; the
-- catalog is metadata only.

UPDATE report_categories
SET availability = 'live',
    target_route = '/reports/delivery'
WHERE tenant_id IS NULL AND key = 'delivery';

INSERT INTO reports
    (tenant_id, category_key, key, name, description, endpoint, required_permission, availability, is_system)
VALUES
    (NULL, 'delivery', 'delivery', 'Delivery & Procurement',
     'Ordered vs loaded vs received litres, delivery variance, delivery delays, the procurement pipeline and a deterministic supplier scorecard (on-time, quantity accuracy, disputes, document completeness, delivery-variance history). Supplier cost and price competitiveness require margin.view.',
     '/api/v1/reports/delivery', 'station.read', 'live', true)
ON CONFLICT (key) WHERE tenant_id IS NULL DO UPDATE SET
    category_key        = EXCLUDED.category_key,
    name                = EXCLUDED.name,
    description         = EXCLUDED.description,
    endpoint            = EXCLUDED.endpoint,
    required_permission = EXCLUDED.required_permission,
    availability        = EXCLUDED.availability;
