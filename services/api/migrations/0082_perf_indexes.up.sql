-- 0082_perf_indexes.up.sql
--
-- Hot-path index audit (see docs/db-performance.md). Each index below backs a
-- tenant-scoped list/overview query whose ORDER BY (or filter) column had no
-- supporting composite index, forcing Postgres to filter by tenant_id and then
-- sort the whole result set. The new composites let the planner satisfy both
-- the tenant filter and the ordering from a single index scan, and bound the
-- rows examined to one page.
--
-- The migration runner is transactional; CREATE INDEX (no CONCURRENTLY) is used
-- intentionally. These are append-heavy tables but the locks are taken inside
-- the migration transaction at deploy time.

-- audit_logs: handleListAuditLogs filters on tenant_id (+ optional
-- action/entity/actor/time-range) and orders by (occurred_at DESC, id DESC).
-- The existing idx_audit_logs_occurred_at is NOT tenant-scoped, so a busy
-- tenant still sorted globally. This composite serves filter + order together.
CREATE INDEX idx_audit_logs_tenant_time
    ON audit_logs (tenant_id, occurred_at DESC, id DESC);

-- journal_entries: ListEntriesPage filters tenant_id, orders by entry_number
-- DESC. entry_number is monotonic per tenant, so this is the natural paging key.
CREATE INDEX idx_journal_entries_tenant_number
    ON journal_entries (tenant_id, entry_number DESC);

-- payables: ListPage orders by (due_date NULLS LAST, created_at, id) within a
-- tenant. NULLS LAST in the index matches the query so the sort is index-served.
CREATE INDEX idx_payables_tenant_due
    ON payables (tenant_id, due_date NULLS LAST, created_at, id);

-- expenses: ListExpensesPage orders by (expense_date DESC, created_at DESC)
-- within a tenant.
CREATE INDEX idx_expenses_tenant_date
    ON expenses (tenant_id, expense_date DESC, created_at DESC);

-- supplier_payments: ListPage orders by (payment_date DESC, created_at DESC).
CREATE INDEX idx_supplier_payments_tenant_date
    ON supplier_payments (tenant_id, payment_date DESC, created_at DESC);

-- customer_payments: ListPage orders by (payment_date DESC, created_at DESC).
CREATE INDEX idx_customer_payments_tenant_date
    ON customer_payments (tenant_id, payment_date DESC, created_at DESC);

-- customer_invoices: ListInvoicesPage orders by (invoice_date DESC,
-- created_at DESC). (FK index on customer_id already exists.)
CREATE INDEX idx_customer_invoices_tenant_date
    ON customer_invoices (tenant_id, invoice_date DESC, created_at DESC);

-- accounting_exports: ListExportsPage orders by generated_at DESC.
CREATE INDEX idx_accounting_exports_tenant_time
    ON accounting_exports (tenant_id, generated_at DESC);

-- purchase_orders: ListPurchaseOrdersPage filters tenant_id (+ optional
-- station/supplier/status) and orders by (created_at DESC, id). The existing
-- station/supplier+status composites do not cover the unfiltered, tenant-wide
-- list path.
CREATE INDEX idx_purchase_orders_tenant_created
    ON purchase_orders (tenant_id, created_at DESC, id);

-- supplier_invoices: ListPage orders by (created_at DESC, id) within a tenant;
-- only the (station_id, status) composite existed.
CREATE INDEX idx_supplier_invoices_tenant_created
    ON supplier_invoices (tenant_id, created_at DESC, id);

-- companies: ListPage orders by (created_at DESC, id) within a tenant.
CREATE INDEX idx_companies_tenant_created
    ON companies (tenant_id, created_at DESC, id);

-- incidents: ListPage filters tenant_id (+ optional station/status/severity)
-- and orders by (opened_at DESC, id). Existing indexes are single-column and
-- do not serve the tenant-scoped ordering.
CREATE INDEX idx_incidents_tenant_opened
    ON incidents (tenant_id, opened_at DESC, id);

-- shifts: ListShiftsPage filters (tenant_id, station_id, [operating_day_id])
-- and orders by (opened_at DESC, id DESC). The single-column idx_shifts_station_id
-- gives the station seek but not the ordering.
CREATE INDEX idx_shifts_station_opened
    ON shifts (tenant_id, station_id, opened_at DESC, id DESC);

-- stock_transfer_orders / central_price_rollouts / central_procurement_plans:
-- enterprise list endpoints order by created_at DESC within a tenant; only a
-- bare tenant index existed.
CREATE INDEX idx_sto_tenant_created
    ON stock_transfer_orders (tenant_id, created_at DESC, id);
CREATE INDEX idx_cpr_tenant_created
    ON central_price_rollouts (tenant_id, created_at DESC, id);
CREATE INDEX idx_cpp_tenant_created
    ON central_procurement_plans (tenant_id, created_at DESC, id);

-- fuel_authorizations: ListAuthorizations orders by (created_at DESC, id)
-- within a tenant (the status composite serves a different filter path).
CREATE INDEX idx_fuel_auth_tenant_created
    ON fuel_authorizations (tenant_id, created_at DESC, id);

-- expenses FK: float-scoped petty-cash transactions already have idx_pct_float;
-- expenses.station_id and category already indexed. No further FK gaps on the
-- audited hot tables (every list FK column checked has a supporting index).
