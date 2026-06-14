-- 0108_customer_credit_report_live: point the Customer Credit catalog category
-- at the signature receivables-aging report and register its report row
-- (Reports §5.9).
--
-- Phase 1 (migration 0105) seeded the Customer Credit category as `live` but
-- pointed it at `/reports/credit-cashflow` (a borrowed station-scoped finance
-- route that needs revenue.read — a customer.read holder would 403) and gave it
-- only an `ar-aging` CSV report row, with no aged-bucket report page. Phase 7
-- ships the signature Customer Credit report: a TENANT-WIDE structured endpoint
-- (GET /api/v1/reports/customer-credit, gated by customer.read) returning the
-- §5.9 envelope (receivable / overdue / %-overdue / over-limit / on-hold KPIs,
-- the Current / 1-30 / 31-60 / 61-90 / 90+ aging buckets aged server-side from
-- due_date, per-customer credit-limit utilization, top-overdue ranking, risk
-- badges and a balance -> invoices -> payments drilldown) plus its premium page
-- at /reports/customer-credit. CREDIT EXPOSURE (limit / exposure / utilization)
-- is gated behind customer_credit.read in-handler and omitted for non-holders.
--
-- So this migration makes the catalog HONEST: the Customer Credit category
-- targets the real page reachable by its own customer.read holders, and a
-- `customer-credit` report row is registered under it. The existing `ar-aging`
-- CSV row stays (it remains the export of record). No money figure is stored
-- here — the catalog is metadata only.

UPDATE report_categories
SET availability = 'live',
    target_route = '/reports/customer-credit'
WHERE tenant_id IS NULL AND key = 'customer-credit';

INSERT INTO reports
    (tenant_id, category_key, key, name, description, endpoint, required_permission, availability, is_system)
VALUES
    (NULL, 'customer-credit', 'customer-credit', 'Customer Credit',
     'Receivables aged into Current / 1-30 / 31-60 / 61-90 / 90+ day buckets from invoice due dates, total and overdue exposure, credit-limit utilization per customer, top-overdue ranking, risk badges and a balance -> invoices -> payments drilldown. Credit exposure, limit and utilization require the customer credit permission.',
     '/api/v1/reports/customer-credit', 'customer.read', 'live', true)
ON CONFLICT (key) WHERE tenant_id IS NULL DO UPDATE SET
    category_key        = EXCLUDED.category_key,
    name                = EXCLUDED.name,
    description         = EXCLUDED.description,
    endpoint            = EXCLUDED.endpoint,
    required_permission = EXCLUDED.required_permission,
    availability        = EXCLUDED.availability;
