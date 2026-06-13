-- 0105_report_catalog: the Reports & Intelligence Center registry (Phase 1).
--
-- Until now the reports landing page rendered a HARDCODED Go slice of 8
-- categories (reports_structured_handlers.go handleReportsOverview). This
-- migration turns the report catalog into DATA: the 16 blueprint categories
-- (§4.4) and the catalog of reports that actually back them, so the hub can be
-- driven, gated, and extended without a code change per report.
--
-- System-vs-tenant pattern (mirrors roles in 0004): a row with tenant_id IS NULL
-- is a PLATFORM/system row shared by every tenant; a row with a tenant_id is a
-- tenant override/addition. The 16 categories and the catalog rows for the
-- reports that ship today are seeded as system rows here. RLS therefore admits a
-- row when it is a system row (tenant_id IS NULL) OR it belongs to the current
-- tenant — the same shape used for any shared-catalogue table.
--
-- availability is an honest, three-valued enum:
--   live        — a real backing report endpoint exists and returns data today.
--   partial     — the data exists but the tenant-wide metric/endpoint is not yet
--                 wired (the card links to a station-scoped report; no hub
--                 metric is fabricated).
--   placeholder — no backing domain/data yet (Tank live-sensor, Custom builder,
--                 Scheduled). The card renders honestly as unavailable.
--
-- No money or litre figure is stored here — the catalog is metadata only; live
-- figures are computed on read from the existing repos as exact decimal strings.

-- ---------------------------------------------------------------------------
-- reports.read — the tenant-wide permission that gates the catalog/hub. The
-- per-category required_permission still governs which CARDS a user sees; this
-- is the coarse "can open the Reports Center" gate. Granted to every role that
-- already has a reporting reason to be there.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('reports.read', 'View the Reports & Intelligence Center catalog', 'reports', false)
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'reports.read'
  AND r.code IN (
      'system_admin', 'finance_officer', 'regional_manager', 'executive',
      'auditor', 'station_manager', 'procurement_officer', 'supervisor'
  )
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- report_categories — the 16 blueprint categories as data.
-- ---------------------------------------------------------------------------
CREATE TABLE report_categories (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid REFERENCES tenants(id) ON DELETE CASCADE,
    key                 text NOT NULL,
    name                text NOT NULL,
    description         text NOT NULL DEFAULT '',
    sort_order          integer NOT NULL DEFAULT 0,
    icon                text NOT NULL DEFAULT '',
    required_permission text NOT NULL,
    availability        text NOT NULL DEFAULT 'partial',
    target_route        text NOT NULL DEFAULT '',
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_report_categories_availability
        CHECK (availability IN ('live', 'partial', 'placeholder'))
);

-- One key per scope: a system row (tenant_id IS NULL) and a tenant override may
-- both exist for the same key without colliding (NULLs are distinct in a UNIQUE
-- index, so the partial indexes below enforce uniqueness within each scope).
CREATE UNIQUE INDEX uq_report_categories_system_key
    ON report_categories(key) WHERE tenant_id IS NULL;
CREATE UNIQUE INDEX uq_report_categories_tenant_key
    ON report_categories(tenant_id, key) WHERE tenant_id IS NOT NULL;
CREATE INDEX idx_report_categories_tenant ON report_categories(tenant_id);

CREATE TRIGGER report_categories_set_updated_at
    BEFORE UPDATE ON report_categories
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- reports — the catalog of individual reports that back the categories.
-- ---------------------------------------------------------------------------
CREATE TABLE reports (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid REFERENCES tenants(id) ON DELETE CASCADE,
    category_key        text NOT NULL,
    key                 text NOT NULL,
    name                text NOT NULL,
    description         text NOT NULL DEFAULT '',
    endpoint            text NOT NULL DEFAULT '',
    required_permission text NOT NULL,
    availability        text NOT NULL DEFAULT 'live',
    is_system           boolean NOT NULL DEFAULT false,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_reports_availability
        CHECK (availability IN ('live', 'partial', 'placeholder')),
    CONSTRAINT chk_reports_system_no_tenant CHECK (
        (is_system = true  AND tenant_id IS NULL) OR
        (is_system = false AND tenant_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX uq_reports_system_key
    ON reports(key) WHERE tenant_id IS NULL;
CREATE UNIQUE INDEX uq_reports_tenant_key
    ON reports(tenant_id, key) WHERE tenant_id IS NOT NULL;
CREATE INDEX idx_reports_tenant ON reports(tenant_id);
CREATE INDEX idx_reports_category ON reports(category_key);

CREATE TRIGGER reports_set_updated_at
    BEFORE UPDATE ON reports
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- RLS — a row is visible when it is a shared system row (tenant_id IS NULL) OR
-- it belongs to the current tenant. WITH CHECK only admits tenant rows so a
-- tenant can never write a system row. Mirrors the export_jobs/roles pattern.
-- ---------------------------------------------------------------------------
ALTER TABLE report_categories ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON report_categories
    USING      (tenant_id IS NULL OR tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE reports ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON reports
    USING      (tenant_id IS NULL OR tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ===========================================================================
-- Seed — the 16 blueprint categories (§4.4) as system rows.
--
-- availability is GROUNDED IN THE ACTUAL ROUTES registered on main
-- (server_routes_reports*.go), not in any prose claim:
--   live        — a structured report endpoint exists and returns data:
--                 Inventory (/reports/inventory/reconciliation), Shift &
--                 Delivery via station-close, Procurement/Finance via payables
--                 aging + financials, Customer Credit via ar-aging/credit-
--                 cashflow, Risk & Loss via fuel-loss + risk alerts, Audit via
--                 audit_logs, Export History via export_jobs + /reports/exports.
--   partial     — data present, tenant-wide metric/endpoint not yet wired:
--                 Executive rollup, Sales (revenue_days is station-scoped today),
--                 Pump throughput, Fleet consumption (per-customer only).
--   placeholder — no backing domain: Tank live-sensor (no ATG feed), Custom
--                 builder (not built), Scheduled (per-tenant scheduling absent).
-- ===========================================================================
INSERT INTO report_categories
    (tenant_id, key, name, description, sort_order, icon, required_permission, availability, target_route)
VALUES
    (NULL, 'executive', 'Executive', 'Cross-domain cockpit: revenue, margin, stock, cash, risk rolled up for leadership.', 10, 'layout-dashboard', 'finance.read', 'partial', '/reports/executive'),
    (NULL, 'sales', 'Sales', 'Revenue, litres, product mix and tender breakdown across stations and days.', 20, 'trending-up', 'revenue.read', 'partial', '/reports/sales-summary'),
    (NULL, 'inventory', 'Inventory', 'Per-tank book-vs-physical reconciliation waterfall, variance and tolerance breaches.', 30, 'layers', 'reconciliation.read', 'live', '/reports/inventory/reconciliation'),
    (NULL, 'tank', 'Tank', 'Live tank telemetry: water, temperature and level trends. Requires an ATG/sensor feed.', 40, 'database', 'inventory.read', 'placeholder', '/reports/tank'),
    (NULL, 'pump', 'Pump', 'Pump and nozzle throughput, utilisation and meter movement.', 50, 'gauge', 'revenue.read', 'partial', '/reports/pump'),
    (NULL, 'shift', 'Shift', 'Shift close summaries: sales, cash, variance and approval status per shift.', 60, 'clock', 'station.read', 'live', '/reports/station-close'),
    (NULL, 'delivery', 'Delivery', 'Fuel deliveries: ordered vs received, dip-before/after variance and supplier shortfalls.', 70, 'truck', 'station.read', 'live', '/reports/station-close'),
    (NULL, 'procurement', 'Procurement', 'Suppliers, purchase orders, goods receipts, invoices and payables aging.', 80, 'shopping-cart', 'payable.read', 'live', '/reports/credit-cashflow'),
    (NULL, 'finance', 'Finance', 'Profit & loss, trial balance, expenses and the financial statement.', 90, 'banknote', 'finance.read', 'live', '/reports/profitability'),
    (NULL, 'customer-credit', 'Customer Credit', 'Receivables aging, credit exposure, statements and overdue balances.', 100, 'credit-card', 'customer.read', 'live', '/reports/credit-cashflow'),
    (NULL, 'fleet', 'Fleet', 'Credit-customer vehicle fuelling: consumption, odometer and per-vehicle limits.', 110, 'car', 'fleet_report.read', 'partial', '/reports/fleet'),
    (NULL, 'risk-loss', 'Risk and Loss', 'Fuel loss, variance patterns, repeated incidents and open risk alerts.', 120, 'shield-alert', 'risk.read', 'live', '/reports/fuel-loss'),
    (NULL, 'audit', 'Audit', 'Immutable audit trail of who did what, when, across the platform.', 130, 'file-search', 'audit.read', 'live', '/reports/audit'),
    (NULL, 'custom', 'Custom', 'Build a report: pick a source, columns, filters and a visual, then save and share.', 140, 'sliders', 'reports.read', 'placeholder', '/reports/custom'),
    (NULL, 'scheduled', 'Scheduled', 'Per-tenant scheduled reports delivered by email, in-app or webhook.', 150, 'calendar-clock', 'reports.read', 'placeholder', '/reports/scheduled'),
    (NULL, 'export-history', 'Export History', 'Every report export: who exported what, when, and the resulting file.', 160, 'download', 'reports.export', 'live', '/reports/exports');

-- ===========================================================================
-- Seed — catalog rows ONLY for reports that ACTUALLY exist as registered
-- endpoints on main (enumerated from server_routes_reports*.go). Categories
-- without a live backing report (tank/custom/scheduled, plus the partial ones)
-- intentionally get no catalog row here — they surface as availability flags on
-- the category, never as a fake report entry.
-- ===========================================================================
INSERT INTO reports
    (tenant_id, category_key, key, name, description, endpoint, required_permission, availability, is_system)
VALUES
    (NULL, 'inventory', 'inventory-reconciliation', 'Inventory Reconciliation', 'Per-tank book-vs-physical waterfall for a station day.', '/api/v1/reports/inventory/reconciliation', 'reconciliation.read', 'live', true),
    (NULL, 'shift', 'station-close', 'Daily Station Close', 'Sales, stock variance, cash position, deliveries and approval status for a day.', '/api/v1/reports/station-close', 'revenue.read', 'live', true),
    (NULL, 'finance', 'cash-reconciliation', 'Cash Reconciliation', 'Expected vs submitted vs deposited cash, shortages and excesses by shift.', '/api/v1/reports/cash-reconciliation', 'finance.read', 'live', true),
    (NULL, 'risk-loss', 'fuel-loss', 'Fuel Loss', 'Loss litres and value, variance %, repeated incidents and loss patterns.', '/api/v1/reports/fuel-loss', 'reconciliation.read', 'live', true),
    (NULL, 'finance', 'profitability', 'Profitability', 'Revenue, COGS, gross margin, expenses and net operating result.', '/api/v1/reports/profitability', 'revenue.read', 'live', true),
    (NULL, 'customer-credit', 'credit-cashflow', 'Credit & Cashflow', 'Sales by tender, collections, outstanding/overdue receivables and projected cash.', '/api/v1/reports/credit-cashflow', 'revenue.read', 'live', true),
    (NULL, 'executive', 'station-comparison', 'Station Comparison', 'Per-station ranking by revenue, litres, margin, variance, expenses and risk.', '/api/v1/reports/station-comparison', 'revenue.read', 'live', true),
    (NULL, 'shift', 'attendance', 'Attendance', 'Roster vs check-in/out with late / no-show derivation per station day.', '/api/v1/reports/attendance', 'station.read', 'live', true),
    (NULL, 'shift', 'corrections-variances', 'Corrections & Variances', 'Submitted vs final approved readings and expected vs received collections.', '/api/v1/reports/corrections-variances', 'station.read', 'live', true),
    (NULL, 'finance', 'financials', 'Financial Statement', 'Tenant financial statement (CSV / PDF / XLSX).', '/api/v1/reports/financials.csv', 'finance.read', 'live', true),
    (NULL, 'customer-credit', 'ar-aging', 'AR Aging', 'Outstanding credit-customer balances by aging bucket.', '/api/v1/reports/ar-aging.csv', 'customer.read', 'live', true),
    (NULL, 'export-history', 'export-jobs', 'Export History', 'Durable receipt and history of report exports.', '/api/v1/exports', 'reports.export', 'live', true);
