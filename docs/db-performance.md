# Database performance: hot-path index audit

This document audits the read paths that grow without bound under real data
volumes — the paginated list endpoints (`ListPage` / `…Page` repo methods) and
the dashboard overview handlers — and records which index serves each one. The
indexes added by migration **0082_perf_indexes** close the gaps found here.

## Method

For every paginated list query and every overview query, we extracted the
`WHERE` predicate, the `ORDER BY`, and any `JOIN`, then cross-referenced
`services/api/migrations/*.up.sql` for a supporting index. A query is
"index-served" when one btree index can satisfy both the equality/range filter
and the sort order, bounding the rows examined to roughly one page.

The dominant pattern in this codebase is **tenant-scoped, newest-first**:
`WHERE tenant_id = $1 ORDER BY <time-or-sequence col> DESC LIMIT … OFFSET …`.
Most big append tables had only a bare `idx_<table>_tenant_id`. That index lets
Postgres find a tenant's rows but not order them, so the planner reads every row
for the tenant and sorts it (a top-N sort) on each page request — fine at
hundreds of rows, increasingly expensive at the hundreds of thousands a busy
tenant accumulates. A composite `(tenant_id, <sort col> DESC[, id])` turns that
into an index range scan that stops after `limit+offset` rows.

`LIMIT/OFFSET` paging still walks (and discards) `offset` index rows, so deep
pages remain O(offset). That is acceptable for the current admin/list UX; if
deep paging becomes hot, switch the worst offenders to keyset paging on the same
index — no schema change needed.

## Representative queries and their access paths

| # | Endpoint / repo method | Table | Predicate + order | Index that now serves it |
|---|---|---|---|---|
| 1 | `handleListAuditLogs` (`audit_handlers.go`) | `audit_logs` | `tenant_id = $1` (+ optional action/entity/actor/since/until); `ORDER BY occurred_at DESC, id DESC` | **`idx_audit_logs_tenant_time (tenant_id, occurred_at DESC, id DESC)`** — previously `idx_audit_logs_occurred_at` was global (not tenant-scoped), so a busy tenant sorted the whole tenant slice. The optional `action`/`entity_type` equality filters are still applied as a residual filter on the scanned range; if one becomes hot it can get its own composite. |
| 2 | `JournalRepo.ListEntriesPage` (`accounting/journal.go`) | `journal_entries` | `tenant_id = $1`; `ORDER BY entry_number DESC` | **`idx_journal_entries_tenant_number (tenant_id, entry_number DESC)`**. `entry_number` is a per-row identity, monotonic, so this is the natural keyset and the sort is fully index-served. The per-row `SUM(debit)` subquery is served by the existing `idx_journal_lines_entry` (one index seek per returned row — see N+1 note below). |
| 3 | `payables.Repo.ListPage` | `payables` | `tenant_id = $1`; `ORDER BY due_date NULLS LAST, created_at, id` | **`idx_payables_tenant_due (tenant_id, due_date NULLS LAST, created_at, id)`**. The index spells `NULLS LAST` to match the query, so undated payables sort to the end without a separate sort step. |
| 4 | `expenses.Repo.ListExpensesPage` | `expenses` | `tenant_id = $1 AND ($2='' OR status=$2)`; `ORDER BY expense_date DESC, created_at DESC, id` | **`idx_expenses_tenant_date (tenant_id, expense_date DESC, created_at DESC)`**. The default (no-status) list is fully index-ordered; the optional `status` filter is a residual predicate. `idx_expenses_status (tenant_id, status)` still covers status-only counts. |
| 5 | `payables.Repo.ListPaymentsPage` / `receivables.Repo.ListCustomerPaymentsPage` | `supplier_payments`, `customer_payments` | `tenant_id = $1`; `ORDER BY payment_date DESC, created_at DESC` | **`idx_supplier_payments_tenant_date`**, **`idx_customer_payments_tenant_date`** — `(tenant_id, payment_date DESC, created_at DESC)`. |
| 6 | `receivables.Repo.ListInvoicesPage` | `customer_invoices` | `tenant_id = $1 AND ($2 IS NULL OR customer_id=$2)`; `ORDER BY invoice_date DESC, created_at DESC, id` | **`idx_customer_invoices_tenant_date (tenant_id, invoice_date DESC, created_at DESC)`** for the tenant-wide list; the existing `idx_customer_invoices_customer` still serves the single-customer drill-down. |
| 7 | `accounting.Repo.ListExportsPage` | `accounting_exports` | `tenant_id = $1`; `ORDER BY generated_at DESC` | **`idx_accounting_exports_tenant_time (tenant_id, generated_at DESC)`**. |
| 8 | `procurement.Repo.ListPurchaseOrdersPage` | `purchase_orders` | `tenant_id = $1` (+ optional station[]/supplier/status); `ORDER BY created_at DESC, id` | **`idx_purchase_orders_tenant_created (tenant_id, created_at DESC, id)`** for the tenant-wide list. The pre-existing `(station_id,status)` / `(supplier_id,status)` composites cover the filtered drill-downs. |
| 9 | `operations.Repo.ListShiftsPage` | `shifts` | `tenant_id=$1 AND station_id=$2 AND ($3 IS NULL OR operating_day_id=$3)`; `ORDER BY opened_at DESC, id DESC` | **`idx_shifts_station_opened (tenant_id, station_id, opened_at DESC, id DESC)`**. The single-column `idx_shifts_station_id` gave the station seek but not the order; this leading-column-prefix index seeks the station and reads in shift order. |
| 10 | `incidents.Repo.ListPage` | `incidents` | `tenant_id=$1` (+ optional station[]/status/severity); `ORDER BY opened_at DESC, id` | **`idx_incidents_tenant_opened (tenant_id, opened_at DESC, id)`**. Existing `status`/`severity`/`station_id` indexes are single-column and could not serve the tenant-scoped ordering. |
| 11 | Enterprise list endpoints (`enterprise/central_commercial.go`) | `stock_transfer_orders`, `central_price_rollouts`, `central_procurement_plans` | `tenant_id=$1`; `ORDER BY created_at DESC` | **`idx_sto_tenant_created`**, **`idx_cpr_tenant_created`**, **`idx_cpp_tenant_created`** — `(tenant_id, created_at DESC, id)`. |
| 12 | `fleet.Repo.ListAuthorizationsPage` | `fuel_authorizations` | `tenant_id=$1` (+ optional customer); `ORDER BY created_at DESC, id` | **`idx_fuel_auth_tenant_created (tenant_id, created_at DESC, id)`**. The existing `(tenant_id,status)` index serves the status dashboard; this serves the chronological list. |

### Paths that were already well-indexed (no change)

- **`stock_movements` ledger** (`inventory.Repo.ListMovements/Page`,
  `CurrentBalance`): `WHERE tenant_id=$1 AND tank_id=$2 ORDER BY seq`. Served by
  `idx_stock_mvt_tank_seq (tank_id, seq)` — `tank_id` already implies the
  tenant, and `seq` is the order. The opening-balance lookups use the partial
  `uq_stock_mvt_one_opening`. No new index needed.
- **`deliveries` by tank** (`ListDeliveriesForTankPage`): `(tank_id, received_at)`
  served by `idx_deliveries_tank_time`.
- **`meter_readings` / `tank_dip_readings`** per shift/tank: served by
  `idx_meter_readings_shift_id` / `idx_tank_dip_shift_id` / `idx_tank_dip_tank_id`
  and the partial active-row uniques; reads are shift- or tank-scoped (small).
- **`notifications` feed** (`ListForUser`): `idx_notifications_user_feed`
  `(tenant_id, user_id, created_at DESC)` plus the unread partial index already
  cover both the "all" and "unread-only" reads.
- **`revenue_days`** (`ListRecentForStation`): `idx_revenue_days_station
  (station_id, business_date DESC)`.
- **`ar_entries`** customer ledger: `idx_ar_entries_customer (customer_id,
  recorded_at)`.
- **`risk_signals` / `risk_scores` / `risk_alerts`**: already carry composite
  `(tenant_id, occurred_at)` / `(tenant_id, score DESC)` / `(tenant_id, status)`.
- Dimension/master tables (`stations`, `tanks`, `pumps`, `nozzles`, `products`,
  `regions`, `accounts`) order by `name`/`code`/`number` and are bounded by
  station/region scope; their cardinality stays small, so the tenant index plus
  a cheap sort is adequate.

## N+1 findings (documented; not fixed in this pass)

These are bounded by page size today (the list endpoints clamp `limit`, default
~50) and each child query is index-served, so they are flagged as follow-ups
rather than refactored under a perf-index migration:

1. **`procurement.Repo.ListPurchaseOrdersPage`** (`internal/procurement/purchase_orders.go`):
   for each PO row returned it calls `listPurchaseOrderLines(...)`, issuing one
   `SELECT … FROM purchase_order_lines WHERE purchase_order_id = $…` per PO. With
   a page of 50 POs that is 51 round-trips. Each child query uses
   `idx_po_lines_order_id`, so it is fast, but it multiplies latency and runs
   nested queries against the pool while the parent `rows` cursor is still open.
   *Follow-up:* drain the parent rows first, then batch-load lines with one
   `WHERE purchase_order_id = ANY($ids)` query and group in Go (or aggregate
   lines into a JSON array in the parent query).

2. **`JournalRepo.ListEntriesPage`** (`internal/accounting/journal.go`): the
   per-row correlated subquery `COALESCE((SELECT SUM(debit) FROM journal_lines
   l WHERE l.journal_entry_id = e.id),0)`. This is one index seek per returned
   row via `idx_journal_lines_entry` — acceptable at page scale, but a
   `LEFT JOIN LATERAL` / grouped aggregate would collapse it if the journal
   list becomes hot.

Both are low-risk to leave as-is at current limits and were intentionally not
changed here to keep migration 0082 to schema-only, verifiable changes.

## Verification

- **Schema validity:** all referenced tables/columns were cross-checked against
  their `CREATE TABLE` definitions in `services/api/migrations`. `NULLS LAST`,
  `DESC`, and multi-column btree index definitions are standard Postgres.
- **Apply / rollback:** the migration runner (`services/api/cmd/migrate`) uses
  golang-migrate's file source and runs each step transactionally. `migrate up`
  applies `0082_perf_indexes.up.sql`; `migrate down` runs
  `0082_perf_indexes.down.sql`, which `DROP INDEX IF EXISTS` for exactly the
  indexes created (reverse order). No `CONCURRENTLY` is used because the runner
  wraps each migration in a transaction (CONCURRENTLY cannot run in one).
  No local Postgres / `DATABASE_URL` was available in the build environment, so
  application was validated by SQL review against the runner contract rather
  than executed.
- After deploy, run `ANALYZE` (autovacuum will also pick the new indexes up) and
  spot-check with `EXPLAIN (ANALYZE, BUFFERS)` on the queries in the table above
  to confirm `Index Scan using idx_…` replaces the prior `Seq Scan` + `Sort`.
