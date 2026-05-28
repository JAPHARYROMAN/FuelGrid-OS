# Data-Model & Migrations Audit — FuelGrid OS

**Scope:** All 63 forward migrations (`0001`–`0063`) and their 63 reverse migrations in `services/api/migrations`, defining **105 tables** plus the platform permission/role catalogue, RLS policies, triggers, indexes, and seed data. Read in dependency order against the house conventions in [`docs/db-conventions.md`](../db-conventions.md) and [`docs/multi-tenancy.md`](../multi-tenancy.md). Engine: Postgres 16 via pgx/v5. Read-only audit; the only artifact written is this report.

**Method:** Atomic walkthrough of every table, column, type, constraint, FK, index, trigger, and seeded row. RLS *runtime* posture (enabled-but-inert because the API connects as a superuser) was covered by a prior audit and is **not** re-litigated here; this report focuses on schema-design depth. Findings are prefixed `DB-` and cite `file:line`.

**Headline:** This is an unusually disciplined schema. Type discipline is near-perfect (zero `float`/`money`/`serial`/`timestamp`-without-tz across 105 tables; UUID PKs everywhere; money `numeric(14,2)`, rates `numeric(14,4)`, litres `numeric(14,3)` applied consistently). The composite tenant-FK technique from migration `0008` is propagated rigorously through the operational and procurement layers. The principal *systemic* weakness is in the **finance/accounting layer (Phases 7–10)**, where a recurring class of nullable reference columns — `journal_entry_id`, `supplier_id` on payables, `station_id` on journal rows, polymorphic `source_id` — were left as plain `uuid` with **no foreign key at all**, abandoning the tenant-bound-FK guarantee precisely where the money lives. None of these is an active cross-tenant *leak* (application scoping still filters), but they remove the DB backstop the rest of the schema relies on, and several permit silently-orphaned or cross-tenant references.

---

## 1. Conventions scorecard (what the schema gets right)

Before the findings, it is worth recording the conventions the schema honours essentially everywhere, because they bound the severity of the deviations:

| Convention | Adherence | Evidence |
|---|---|---|
| UUID PKs, `gen_random_uuid()` default | 100% | Every `CREATE TABLE` |
| `tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT` on domain tables | ~100% | All domain tables; only deliberately parent-scoped join tables omit it (documented) |
| money `numeric(14,2)` / rate `numeric(14,4)` / litres `numeric(14,3)` | 100% of fuel/finance columns | e.g. `0018:13-17`, `0030:13-18`, `0033:20-28` |
| `timestamptz` for all timestamps | 100% | 0 `timestamp`-without-tz columns found |
| No `float`/`double`/`money`/`serial` | 100% | grep returns only comment hits |
| `created_at`/`updated_at` + `set_updated_at()` trigger on mutable tables | ~100% | Append-only tables correctly omit `updated_at`+trigger per convention |
| `status` CHECK constraints for lifecycle | 100% of status columns | every `chk_*_status` |
| Composite `(tenant_id,id)` unique + composite child FK | Operational/procurement layers: excellent; finance layer: partial | `0008`, `0010`, `0011`, `0023` vs §4 below |
| RLS policy in same migration as table | ~100% (51 tables enable RLS) | every domain table |
| Permission seeds idempotent (`ON CONFLICT DO NOTHING`) | From `0020` onward: yes; `0004`/`0007`/`0009`–`0019`: no (but those are first-insert, so safe) | see DB-014 |
| Append-only ledgers (`stock_movements`, `audit_logs`, `outbox_events`, `ar_entries`, reading tables) | Correct discipline | `0024`, `0006`, `0016`, `0017`, `0034` |
| Migrations append-only/immutable | Yes — no evidence of in-place edits; fixes are forward migrations (`0008`, `0021`–`0023`, `0026`, `0030`) | git-style forward-fix pattern |
| `pgcrypto` (0001), `btree_gist` (0038) added via migration | Yes | `0001:3`, `0038:7` |

The composite-FK trick deserves special praise: `0011_pumps_nozzles` chains `nozzles → pumps(tenant_id,id,station_id)` and `nozzles → tanks(tenant_id,id,station_id,product_id)` so the *database itself* proves a nozzle shares its pump's station and its tank's station+product. `0023_station_consistency` extends this to force a shift's station to equal its operating day's station. This is materially stronger than most production schemas achieve.

---

## 2. Migration-by-migration / domain walkthrough

### 2.1 Foundation & identity (0001–0008)

- **0001 `init`** — `tenants`, `companies`, `regions`, `stations`, and the reusable `set_updated_at()` (`CREATE OR REPLACE`, idempotent). Lat/long `numeric(10,7)` with range CHECKs — correct, not float. `slug` regex CHECK. `stations.region_id` is `ON DELETE SET NULL` (correct — a region can be removed without orphaning stations). Down drops in correct dependency order and intentionally leaves `pgcrypto` (documented). Clean.
- **0002 `users`** / **0003 `auth`** — additive `ALTER`s; `sessions.token_hash bytea NOT NULL UNIQUE`, `ip inet`. `devices`/`sessions` use `ON DELETE CASCADE` from `user_id` (appropriate — sessions die with the user) and `ON DELETE RESTRICT` from `tenant_id`. `0003` down correctly drops sessions before devices and reverses the column adds. Good.
- **0004 `rbac`** — `permissions`, `roles`, `role_permissions`, `user_roles`, `user_station_access`. The `chk_roles_system_no_tenant` CHECK elegantly enforces "system role ⇔ tenant_id NULL". `idx_roles_system_code` partial-unique on `code WHERE is_system` lets every tenant share one catalogue. Platform seed lives in the schema migration (justified — it is platform vocabulary, not tenant data). Note: this seed has **no `ON CONFLICT`** (DB-014), tolerable only because it is the first insert.
- **0005 `rls`** — creates `fuelgrid_app` role idempotently, grants, `ALTER DEFAULT PRIVILEGES`, and `tenant_isolation`/`tenant_or_system` policies. Policies compare `tenant_id::text = current_setting('app.current_tenant', true)` — fail-closed on missing GUC. Out of audit scope per instructions.
- **0006 `audit_outbox`** — append-only `audit_logs` (no `updated_at`/trigger — correct) and `outbox_events`. `actor_id` is `ON DELETE SET NULL` (audit must outlive users — correct). Partial index `WHERE published_at IS NULL` is the publisher hot path. `jsonb` for `previous_value`/`new_value`/`payload` (justified). Solid.
- **0007 `admin_permissions`** — permission/grant seed only. Down deletes `role_permissions` before `permissions` to respect the `ON DELETE RESTRICT` on `role_permissions.permission_id` — careful and correct.
- **0008 `tenant_integrity`** — the keystone. Adds `uq_<table>_tenant_id UNIQUE(tenant_id,id)` to `companies/regions/stations/users` and rewrites child FKs to the composite `(tenant_id, parent_id)` form, replacing the cross-tenant-permissive single-column FKs. Down reverses cleanly. This migration is the reference pattern every later domain table follows (mostly).

### 2.2 Equipment & inventory (0009–0017, 0024–0027)

`products` (0009), `tanks` (0010), `pumps`/`nozzles` (0011), `tank_calibration_charts`/`_entries` (0012), `pump_calibrations`/`incidents` (0013), readings (0016/0017), `stock_movements` (0024), `deliveries` (0025), sales-idempotency index (0026), `tank_reconciliations` (0027).

Highlights:
- `products`: `default_price numeric(14,2)`, `tax_rate numeric(5,2)`, `density_kg_m3 numeric(10,3)`, `loss_tolerance_percent numeric(5,2)` — all appropriate. `color` hex CHECK. Composite `(tenant_id,id)` added up front for tanks to reference.
- `tanks`: `chk_tanks_safe_band` (`safe_min ≤ safe_max ≤ capacity`) is a thoughtful multi-column CHECK. Exposes `uq_tanks_tenant_station_product` so nozzles can prove product alignment.
- `tank_calibration_entries`: intentionally **no `tenant_id`** and no RLS, scoped through its parent chart (documented, mirrors `role_permissions`). The unique `(chart_id, dip_mm)` and `idx_tcc_one_active` partial-unique (one active chart per tank) are correct.
- `stock_movements`: the ledger. `seq bigint GENERATED ALWAYS AS IDENTITY` for true append order (correct choice over timestamp). `balance_after` is documented as a per-row snapshot, not authoritative — sound. Polymorphic `source_ref_(type,id)` with a CHECK on type and a partial index — justified (no single FK target). `0026` adds `uq_stock_mvt_sales_per_shift_tank` partial-unique for idempotent sales posting; `0030` adds `idx_stock_mvt_delivery_source_once`. Excellent idempotency design.
- `incidents` (0013): `related_entity_(type,id)` is an explicitly polymorphic pointer with no FK — documented and reasonable. All `_by` user columns use composite FKs.
- `tank_reconciliations` (0027): freezes book/physical/variance with `through_seq` watermark; `idx_tank_recon_sealed ... WHERE status='sealed'` supports balance-forward lookup. `variance_percent numeric(10,4)` (rate-grade precision — good).

### 2.3 Operations: days, shifts, readings, close (0014–0023)

`operating_days` (0014), `shifts` + `shift_attendants` + `shift_nozzle_assignments` (0015), readings (0016/0017), `shift_close_lines` + `cash_submissions` (0018), `shift_exceptions` (0019), permission expansions (0020/0021), and two integrity hardening migrations (0022/0023).

- `operating_days`: `idx_operating_days_active` partial-unique `WHERE status<>'locked'` — at most one non-locked day per station/date, allowing historical re-open. Smart.
- `0022` and `0023` are exemplary forward-fix migrations closing audit findings: `0022` ties a nozzle assignment to an actual `shift_attendants` row (`sna_attendant_on_shift_fk`); `0023` adds station-bearing composite uniques and forces shift/day/nozzle stations to agree. **`0023` performs an inline `UPDATE` backfill** (`0023:27-30`) inside a schema migration to populate the new `station_id` before `SET NOT NULL` — see DB-013 (convention "no data manipulation in schema migrations").
- `shift_close_lines` (0018) is append-only (no `updated_at`/trigger — correct). `cash_submissions` carries the per-tender breakdown + `variance`, one per shift (`uq_cash_submissions_shift`).

### 2.4 Procurement / supply chain (0028–0031)

`suppliers` + `supplier_products` (0028), `purchase_orders` + `_lines` (0029), goods-receipt evolution of `deliveries`+`stock_movements` (0030), `supplier_invoices` + `_lines` + `procurement_discrepancies` (0031).

- Composite FKs are thorough: `purchase_order_lines` exposes `uq_po_lines_tenant_order_id UNIQUE(tenant_id, purchase_order_id, id)` so `deliveries.po_line_fk` and `supplier_invoice_lines.po_line_fk` can prove a line belongs to the named PO. This is the same rigor as the equipment layer.
- **0030** is a clean additive `ALTER` evolving Phase-4 `deliveries` into priced goods receipts, with `landed_cost_per_litre numeric(14,4)` (rate-grade) and a careful `chk_deliveries_costs_nonnegative`. Partial indexes `WHERE <col> IS NOT NULL` on the new nullable FKs avoid bloating. Down reverses every column/constraint with `IF EXISTS`. Strong.
- `procurement_discrepancies` and `supplier_invoice_lines` are append-only (no `updated_at`), yet `procurement_discrepancies` is mutated on resolve (`status`, `resolved_at`). Since it has no `updated_at` column at all, this is a benign in-place status update — see DB-015.
- `idx_suppliers_tenant_code` (0028:27) is **not** filtered `WHERE status<>'deleted'`. Suppliers use statuses `active/inactive/deactivated` (no `deleted`), so the convention's "reuse a code after soft-delete" pattern is unavailable — see DB-016 (Low).

### 2.5 Revenue & finance core (0032–0036)

`price_changes` (0032), `sales` (0033), `customers` + `ar_entries` (0034), `payments` (0035), `revenue_days` (0036).

- `price_changes`: append-only, effective-dated, `unit_price numeric(14,4)`. `idx_price_changes_resolve(station_id, product_id, effective_from DESC)` is exactly the active-price query shape. Good.
- `sales`: the recognized-revenue table. Snapshots `unit_price (14,4)`, `tax_rate (5,2)`, `unit_cost (14,4)`, and money columns at `(14,2)`. Seven composite FKs pin every dimension to the tenant. `uq_sales_shift_nozzle` is the idempotency key. `unit_cost`/`cogs_amount`/`margin_amount` are nullable (cost may be unknown at sale time) — acceptable.
- `ar_entries`: append-only AR ledger with `balance_after` snapshot, `source_ref_(type,id)` polymorphic (no FK — consistent with the ledger pattern).
- `payments`: `tender_type` CHECK, `customer_id` partial index. `shift_id`/`customer_id` nullable with composite FKs. Clean.
- `revenue_days`: a wide denormalized daily rollup (gross/net/tax/cogs/margin + five tender totals + variance). This is a *deliberate* read-model/close artifact, not a normalization smell, and is frozen by `status='locked'`. `uq_revenue_days_station_day` enforces one per station/day.

### 2.6 Accounting & treasury (0037–0049) — the weak quartile

`accounts` (0037), `accounting_periods` (0038), `journal_entries`/`journal_lines` (0039), `cash_reconciliations`+lines (0042), `bank_accounts`/`bank_deposits`+lines (0043), `bank_statement_imports`/`lines` (0044), `customer_invoices`+lines (0045), `customer_payments`+allocations (0046), `expense_categories`/`expenses` (0047), petty cash (0048), `accounting_exports` (0049), plus payables/supplier-payments (0040/0041).

Strengths:
- `accounts`: self-referential `accounts_parent_fk` uses the composite `(tenant_id, parent_id)` form — correct, prevents a cross-tenant parent. `idx_accounts_system_key` partial-unique. Clean COA.
- `accounting_periods` (0038): a **GiST exclusion constraint** `EXCLUDE USING gist (tenant_id WITH =, daterange(start,end,'[]') WITH &&)` prevents overlapping periods per tenant — the correct, elegant tool (requires `btree_gist`, added in the same file). Notable highlight.
- `journal_lines.chk_journal_line_amounts` (`debit≥0 AND credit≥0 AND NOT(debit>0 AND credit>0)`) enforces the one-sided-line rule at the row level. Good.

Weaknesses (the systemic issue) — see DB-001/002/003:
- `journal_entries.station_id` (0039:17) and `journal_lines.station_id` (0039:58) are plain `uuid` with **no FK to `stations`**. Every other table in the schema FK-binds `station_id` with the composite form; the GL does not. A journal line could carry a station from another tenant.
- `journal_entries.reverses_entry_id` / `reversed_by_entry_id` (0039:20-21) have **no self-FK**, though `uq_journal_entries_tenant_id` exists as a perfect composite target. Reversal links can dangle or cross tenant.
- `payables` (0040): `supplier_id`, `station_id`, `journal_entry_id` are all plain `uuid` with **no FK** — the migration comment explicitly states "No FK to the Phase-5 tables — the source ids are carried as a contract, scoped by tenant." This is a conscious denormalization but it abandons the DB backstop for AP, the most fraud-sensitive ledger. Composite targets (`uq_suppliers_tenant_id`, `uq_journal_entries_tenant_id`) exist and could be used.
- The pattern repeats: `supplier_payments.supplier_id`/`journal_entry_id` (0041), `cash_reconciliations.journal_entry_id`/`reviewed_by` (0042 — note `reviewed_by` is an unFK'd user ref), `bank_deposits.prepared_entry_id`/`confirmed_entry_id` (0043), `bank_statement_lines.journal_entry_id`/`matched_doc_id` (0044), `customer_invoices.journal_entry_id`/`source_id` (0045), `customer_payments.journal_entry_id` (0046), `expenses.approved_by`/`journal_entry_id` (0047 — `approved_by` is an unFK'd user ref while `created_by` is FK'd), petty-cash `journal_entry_id` columns (0048).

The `journal_entry_id` links in particular could be made FK-safe trivially (the composite unique target already exists), and the inconsistency between FK'd `created_by` and unFK'd `approved_by`/`reviewed_by` within the same table (DB-004) is almost certainly an oversight rather than a design decision.

Other notes:
- `bank_accounts.currency DEFAULT 'NGN'` (0043:14) vs `companies.currency DEFAULT 'USD'` (0001:47) — inconsistent default currency across the schema (DB-017, Info).
- The double-entry balance invariant (Σdebits = Σcredits per entry) is necessarily a cross-row constraint and is **not** enforced in the DB (no deferred constraint trigger). This is app-enforced. Acceptable and conventional, but worth recording (DB-009, Low/Info) given this is a financial system — a `CONSTRAINT TRIGGER ... DEFERRABLE INITIALLY DEFERRED` summing lines per entry would harden it.
- `accounting_exports.filters jsonb` and `risk/*` `evidence/components/metadata jsonb` are justified (variable-shape provenance/evidence), not normalization smells.

### 2.7 Fleet, credit, enterprise, risk (0050–0063)

- **0050 `customer_master`** — additive `ALTER` on `customers`, broadening `chk_customers_status` while *retaining* legacy `inactive`/`deleted` values so the Phase-6 soft-delete index keeps working. Backward-compatible and well-reasoned. `customer_contacts` child uses composite FK + `ON DELETE CASCADE`.
- **0051 `customer_credit_profiles`** — one-per-customer (`uq_ccp_customer`), `warning_threshold_pct (5,2)` CHECK 0–100. Clean.
- **0052 `customer_price_agreements`** — two partial-unique indexes elegantly distinguish the NULL-station (tenant-wide) scope from station-specific scopes for the "one active agreement" rule. `fixed_price/discount/markup numeric(14,4)`. Strong.
- **0053 `fleet_identity`** — vehicles/drivers/credentials. `fuel_credentials.token_hash`/`customer_drivers.pin_hash` store only hashes (good security). **`customer_drivers.allowed_product_ids uuid[]`** (0053:50) is an **array of product UUIDs with no FK enforcement** — directly contradicting the `0028` design note that products live in a join table "rather than being hidden in JSON" precisely to keep them tenant-bound. Cross-tenant/stale product IDs are possible (DB-006, Medium).
- **0054 `fuel_authorizations`** — `fuel_limits.product_id` (0054:12) and `fuel_authorizations.product_id` (0054:42) have **no FK to `products`** while every sibling reference (customer/vehicle/driver/credential/station) is composite-FK'd (DB-007, Medium). `uq_fuel_auth_consumed` partial-unique ensures one auth consumed by ≤1 sale. `fuel_authorization_denials` is a log table with unFK'd `customer_id`/`station_id`/`actor_id` (acceptable for a denial log).
- **0055 `odometer`** — `reading numeric(14,1)` (odometer, appropriate). Append-only.
- **0056 `statements_alerts`** — `customer_credit_alerts` uses `uq_credit_alert_open ... WHERE status IN ('open','acknowledged')` for idempotent scanning. `assigned_to` unFK'd (minor).
- **0057 `enterprise_governance`** — station groups, `enterprise_scope_grants` (polymorphic `scope_id` by `scope_type` — justified, no FK), and a generic approval engine. `approval_decisions.uq_approval_decision_one(tenant_id, request_id, decided_by)` enforces one decision per approver. `approval_requests.reference_(type,id)` polymorphic. Reasonable.
- **0058 `enterprise_projections`** — `station_daily_kpis` read-model (`ON DELETE CASCADE` from station — correct for a projection) and `enterprise_projection_state` (composite PK, no surrogate — fine for a state table).
- **0059 `central_commercial`** — `stock_transfer_orders` has `chk_sto_distinct(from_tank_id<>to_tank_id)` but does **not** enforce that both tanks and the order share `product_id` (no composite FK like `tanks(tenant_id,id,product_id)`), so a transfer could be booked between mismatched-product tanks (DB-008, Medium). `out_movement_id`/`in_movement_id` unFK'd to `stock_movements` (same `journal_entry_id`-style gap).
- **0060–0063 `risk`** — `risk_signals` is an append-only fact table with several unFK'd dimension refs (`station_id`/`actor_id`/`customer_id`/`supplier_id`); for an immutable signal log this is defensible (sources may be soft-deleted), though `tenant_id`-bound composite FKs would still be safe. `score integer` is the right type for a 0–N score. `uq_risk_alert_open` / `uq_risk_score_entity` give idempotent detection/scoring. Investigation cases (0062) use composite FKs to the case throughout. Suppressions/feedback (0063) are log-shaped. Consistent, clean.

---

## 3. Down-migration correctness

Every `down` was inspected. General quality is high: tables are dropped child-before-parent; permission grants are removed before their permissions (respecting `role_permissions.permission_id`'s `ON DELETE RESTRICT`); `0030`/`0003`/`0008`/`0023` reverse `ALTER`s in correct reverse order; additive-column downs use `DROP COLUMN`/`DROP CONSTRAINT IF EXISTS`.

Observations:
- **`0005_rls.down` (lines 13-14)** does `ALTER TABLE roles ENABLE ROW LEVEL SECURITY; -- keep current state` immediately followed by `DISABLE`. The enable-then-disable is a redundant no-op (harmless) — DB-018 (Info).
- **Column-dropping downs are lossy by nature** (`0003`, `0030`, `0050`, etc. drop data-bearing columns). This is acceptable under the convention ("both up and down must work *during development*"), but any of these run against production would destroy data — they should carry the "irreversible-by-design / requires sign-off" flag the convention mentions. Currently none are annotated as such (DB-019, Low).
- No `down` was found that would *fail* due to dependency ordering or that drops too much/too little. The reverse migrations are correct.

---

## 4. Tenant-integrity deep dive (the central isolation concern)

Per the brief, this is the highest-priority axis. Findings:

1. **Operational + procurement + equipment layers (0001–0036 mostly):** composite `(tenant_id, parent_id)` child FKs are used essentially everywhere a parent reference exists. A tenant-A row physically *cannot* reference a tenant-B station/tank/nozzle/shift/PO/supplier. This is the gold standard and is the bulk of the schema.

2. **Finance/accounting layer (0039–0048):** a recurring set of reference columns were left as bare `uuid` with no FK — `journal_entry_id` (≈8 tables), `payables.supplier_id`/`station_id`, `journal_*.station_id`, `journal_entries.reverses*_id`, plus unFK'd user refs `reviewed_by`/`approved_by`. Because composite unique targets already exist on the referenced tables, **the DB-level cross-tenant guarantee was available and simply not wired up.** Application scoping still filters these (so this is not a demonstrated live leak), but it violates the documented "DB is the backstop" contract for the most sensitive data. **This is the single most important cluster of findings (DB-001–005).**

3. **Array/implicit references:** `customer_drivers.allowed_product_ids uuid[]` (DB-006), `fuel_*.product_id` (DB-007), and `stock_transfer_orders` product alignment (DB-008) are places where a tenant-bound FK or join table was the established pattern but was skipped.

4. **Uniqueness without `tenant_id` prefix:** several natural-key uniques key on a globally-unique UUID without leading `tenant_id` — e.g. `uq_sales_shift_nozzle(shift_id, nozzle_id)` (0033:50), `uq_tank_recon_tank_day(tank_id, operating_day_id)` (0027:50), `uq_cash_submissions_shift(shift_id)` (0018:57), `uq_revenue_days_station_day` (0036:41). These are **not** isolation bugs (the leading column is a UUID PK, so cross-tenant collision is impossible), but they deviate from the "tenant-scoped indexes should lead with tenant_id" guidance and are slightly less efficient for tenant-filtered planning (DB-011, Low).

---

## 5. Index coverage

- **FK indexing:** every FK column carries an index in the overwhelming majority of tables; `tenant_id` is always indexed. Spot-checks found no missing FK index on a high-traffic path.
- **Query-shape composites:** strong — `idx_price_changes_resolve`, `idx_stock_mvt_tank_seq`, `idx_sales_station_day`, `idx_purchase_orders_station_status`, `idx_tank_recon_sealed`, `idx_revenue_days_station(business_date DESC)` all match real read patterns.
- **Idempotency partial-uniques:** abundant and correct (`uq_stock_mvt_sales_per_shift_tank`, `idx_stock_mvt_delivery_source_once`, `uq_cash_recon_day`, `uq_bsi_hash`, `uq_credit_alert_open`, `uq_risk_alert_open`, `uq_fuel_auth_consumed`).
- **Minor gaps:** `journal_entries.station_id` is unindexed *and* unFK'd (DB-002) — station-filtered GL reports would seq-scan. `payables` has no index on `(tenant_id, supplier_id)` composite for supplier-aging (only `idx_payables_supplier` on the bare column) — minor (DB-012, Low). No evidence of redundant/duplicate indexes (verified: zero duplicate index names globally, and no two indexes lead with the same column set on the same table).
- **No duplicate index names and no duplicate `uq_` constraint names** exist across all 63 migrations (verified by extraction). The historical `idx_cpa_*` collision flagged in the brief has been resolved: `0046` uses `idx_cpa_*` and `0052` deliberately uses `idx_cpagr_*`. (Note: FK constraint names `cpa_payment_fk`/`cpa_customer_fk` are reused across the two tables but are table-scoped in Postgres, so they do not collide.)

---

## 6. Naming & hygiene

- Index/constraint naming is highly consistent: `idx_`/`uq_` prefixes, `chk_<table>_<col>`, `<table>_set_updated_at` triggers, `<child>_<parent>_fk`. Pluralized snake_case tables throughout.
- A few unique indexes use `uq_` as an *index* name rather than the convention's `idx_` (e.g. `uq_sna_shift_nozzle` 0015:112, `uq_cash_recon_day` 0042:38, `uq_bdl_recon` 0043:90). The convention says unique indexes use `idx_<...>` with "suffix optional"; using `uq_` for a `CREATE UNIQUE INDEX` is a harmless local inconsistency (DB-020, Info).
- New permission **categories** introduced ad hoc — `reading`/`cash` (0021), `procurement` (0028), `fleet` (0053), `enterprise` (0057), `risk` (0060) — none documented in conventions; the doc's status/category lexicon was never updated (DB-010, Low).
- Migrations are immutable/append-only; corrections are forward migrations. No in-place edits detected.

---

## 7. Permission/role seeding

- Catalogue seeds live in schema migrations (justified — platform vocabulary). From `0020` onward all use `ON CONFLICT (role_id, permission_id) DO NOTHING`; `0004`/`0007`/`0009`–`0019` do not, but those are first-time inserts so re-application would fail only on a partially-applied migration (golang-migrate's transaction wrapping makes this safe in practice). Recommendation: add `ON CONFLICT` uniformly for defense (DB-014, Low).
- Grants are sensible and least-privilege-shaped (attendant minimal then expanded in `0020`; finance permissions confined to `system_admin`/`finance_officer`; `system_admin` catch-all in `0004`). Down migrations remove grants before permissions correctly.
- One subtlety: `0004` grants `system_admin` *all* permissions via a catch-all (`OR (r.code = 'system_admin')`), so every later migration's explicit `system_admin` grant is redundant-but-idempotent. Not a defect.

---

## 8. Findings table

| ID | Severity | File:Line | Issue | Fix |
|---|---|---|---|---|
| DB-001 | High | `0040_payables.up.sql:12-13,19,21` | `payables.supplier_id`, `station_id`, `journal_entry_id` are bare `uuid` with **no FK**. Comment explicitly opts out of FKs for the AP ledger — the most fraud-sensitive table — even though composite targets exist. Permits cross-tenant or orphaned supplier/station/journal references. | Add composite FKs `(tenant_id, supplier_id)→suppliers`, `(tenant_id, station_id)→stations`, `(tenant_id, journal_entry_id)→journal_entries` (all `ON DELETE RESTRICT`). |
| DB-002 | High | `0039_journals.up.sql:17,58` | `journal_entries.station_id` and `journal_lines.station_id` have no FK to `stations` (every other table composite-FK's station_id). GL rows can carry a cross-tenant station; also unindexed. | Add `FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id)` + index. |
| DB-003 | High | `0039_journals.up.sql:20-21` | `journal_entries.reverses_entry_id` / `reversed_by_entry_id` have no self-FK; the composite target `uq_journal_entries_tenant_id` already exists. Reversal links can dangle/cross tenant — corrupts the immutable-reversal audit chain. | Add self composite FKs `(tenant_id, reverses_entry_id)→journal_entries(tenant_id,id)` and same for `reversed_by_entry_id`. |
| DB-004 | Medium | `0042:22`, `0047:48` | Within a single table, `created_by` is composite-FK'd to users but the sibling `reviewed_by` (cash_reconciliations) / `approved_by` (expenses) are bare `uuid` — inconsistent, almost certainly an oversight. Cross-tenant approver/reviewer possible. | Add `(tenant_id, reviewed_by/approved_by)→users` composite FKs. |
| DB-005 | Medium | `0041:19`, `0043:45-46`, `0044:49`, `0045:23,21`, `0046:19`, `0048:52,80` | Systemic: `journal_entry_id` / `prepared_entry_id` / `confirmed_entry_id` / `matched_doc_id` / `source_id` left unFK'd across treasury tables, despite `journal_entries` exposing a composite unique. Removes the DB backstop for finance provenance links. | Wire composite FKs where the target is a single known table (esp. all `journal_entry_id`); leave genuinely-polymorphic `matched_doc_id`/`source_id` as documented exceptions with a CHECK on the companion `*_type`. |
| DB-006 | Medium | `0053_fleet_identity.up.sql:50` | `customer_drivers.allowed_product_ids uuid[]` — array of product UUIDs with no FK; contradicts the `0028` design note that products use a join table to stay tenant-bound. Stale/cross-tenant product IDs possible. | Replace with a `driver_allowed_products` join table carrying composite FK `(tenant_id, product_id)→products`, mirroring `supplier_products`. |
| DB-007 | Medium | `0054_fuel_authorizations.up.sql:12,42` | `fuel_limits.product_id` and `fuel_authorizations.product_id` have no FK while all sibling refs do. A limit/auth can name a cross-tenant or deleted product. | Add `FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id) ON DELETE RESTRICT`. |
| DB-008 | Medium | `0059_central_commercial.up.sql:75-95` | `stock_transfer_orders` enforces `from_tank<>to_tank` but not that both tanks (and the order) share `product_id`; `out/in_movement_id` unFK'd. A transfer between mismatched-product tanks is schema-legal. | Add composite FKs `(tenant_id, from_tank_id, product_id)` / `(tenant_id, to_tank_id, product_id)` → `tanks(tenant_id, id, product_id)` (requires a `(tenant_id,id,product_id)` unique on tanks, which partially exists as `uq_tanks_tenant_station_product`); FK the movement ids. |
| DB-009 | Low | `0039_journals.up.sql:51-79` | Double-entry balance (Σdebit=Σcredit per entry) is not DB-enforced; relies entirely on app code. For a financial GL, an unbalanced entry could persist if a code path errs. | Add a `DEFERRABLE INITIALLY DEFERRED` constraint trigger summing `journal_lines` per `journal_entry_id` on commit. |
| DB-010 | Low | `0021:12-13`, `0028:65`, `0053:117`, `0057:138`, `0060:92` | New permission categories (`reading`, `cash`, `procurement`, `fleet`, `enterprise`, `risk`) and many new status values were introduced without updating `db-conventions.md`'s documented lexicon, which it asks contributors to do. | Update conventions doc with the expanded category/status vocabulary. |
| DB-011 | Low | `0027:50`, `0033:50`, `0018:57`, `0036:41`, others | Several natural-key UNIQUEs omit a leading `tenant_id` (rely on the UUID PK's global uniqueness). Not an isolation bug, but deviates from the "tenant-scoped index leads with tenant_id" guidance and is marginally less plan-friendly. | Optionally re-key as `(tenant_id, …)`; low priority since correctness holds. |
| DB-012 | Low | `0040_payables.up.sql:31` | `idx_payables_supplier` indexes bare `supplier_id`; supplier-aging queries filter by `tenant_id` first. | Make it `(tenant_id, supplier_id, status)` to match the aging read shape. |
| DB-013 | Low | `0023_station_consistency.up.sql:27-30` | Inline `UPDATE … FROM shifts` data backfill inside a schema migration, violating convention rule #3 ("no data manipulation in schema migrations"). Necessary to satisfy the subsequent `SET NOT NULL`, but mixes concerns. | Acceptable as-is given the NOT NULL dependency; if strict separation is desired, split into schema + data migrations. Document the exception inline. |
| DB-014 | Low | `0004`, `0007`, `0009`–`0019` permission seeds | Early permission/role seeds lack `ON CONFLICT DO NOTHING` (only `0020`+ have it). Safe today (first inserts under transactional migrate) but fragile on partial re-apply. | Add `ON CONFLICT … DO NOTHING` uniformly. |
| DB-015 | Low | `0031:83-114` | `procurement_discrepancies` has no `updated_at`/trigger yet is UPDATEd on resolve (`status`, `resolved_at`). Loss of "when was it resolved" beyond the explicit `resolved_at`. Same shape acceptable but inconsistent with mutable-table convention. | Either add `updated_at` + trigger, or document it as a resolve-only mutable log. |
| DB-016 | Low | `0028_suppliers.up.sql:27-28` | `idx_suppliers_tenant_code` not filtered `WHERE status<>'deleted'`; suppliers have no `deleted` status, so a deactivated supplier's code can never be reused — diverges from the soft-delete-reuse convention. | If reuse is desired, add a `deleted` status + partial-index filter; otherwise document the intentional permanence. |
| DB-017 | Info | `0001:47` vs `0043:14` | Default currency differs: `companies` defaults `'USD'`, `bank_accounts` defaults `'NGN'`. Inconsistent and surprising. | Pick one default (or require explicit currency) and align. |
| DB-018 | Info | `0005_rls.down.sql:13-14` | `roles ENABLE … ; -- keep current state` then `DISABLE` — redundant no-op pair. | Remove the redundant `ENABLE`. |
| DB-019 | Low | `0003`, `0030`, `0050` downs (column drops) | Several down migrations `DROP COLUMN`, which is data-destructive if run in production; none flagged as "irreversible-by-design / requires sign-off" per the convention. | Annotate lossy downs; consider guarding production. |
| DB-020 | Info | `0015:112`, `0042:38`, `0043:90`, `0044:27`, etc. | A handful of `CREATE UNIQUE INDEX` use a `uq_` name where the convention's index naming is `idx_<...>`. Harmless inconsistency. | Standardize on `idx_` (or formally bless `uq_` for unique indexes in the doc). |

---

## 9. Severity counts

| Severity | Count |
|---|---|
| Critical | 0 |
| High | 3 |
| Medium | 5 |
| Low | 9 |
| Info | 3 |
| **Total** | **20** |

No **Critical** finding: there is no demonstrated live cross-tenant leak in the schema, no type that would corrupt money/quantity values, and no migration that fails or irreversibly destroys structure. The High findings are "the DB backstop was omitted exactly where it matters most (the GL/AP)," not "the boundary is broken today" — application scoping remains the operative defense.

---

## 10. Top-5 risks

1. **AP ledger has no foreign keys (DB-001).** `payables` deliberately stores `supplier_id`/`station_id`/`journal_entry_id` as bare UUIDs. Accounts payable is the most fraud-exposed ledger in a fuel retailer; it is the one table that should have the *strongest* DB guarantees and currently has the weakest.
2. **General-ledger station references are unbound (DB-002).** `journal_entries`/`journal_lines.station_id` can hold a cross-tenant station with no DB objection, and the column is also unindexed — both an isolation gap and a performance gap on station-scoped financial reports.
3. **Reversal chain is unFK'd (DB-003).** The immutability story of the journal ("corrections are reversals linked to the original") rests on `reverses_entry_id`/`reversed_by_entry_id`, which can dangle or point cross-tenant. A corrupted reversal link undermines auditability of every correction.
4. **Systemic unFK'd finance links + inconsistent approver/reviewer FKs (DB-004/005).** Roughly eight treasury tables leave `journal_entry_id` unbound, and `approved_by`/`reviewed_by` are unFK'd while `created_by` is FK'd in the same table — strong signal of accidental omission rather than deliberate denormalization.
5. **Implicit product references in fleet/transfer (DB-006/007/008).** `allowed_product_ids uuid[]`, unFK'd `fuel_*.product_id`, and missing product-alignment on `stock_transfer_orders` reintroduce, in the newest phases, exactly the cross-tenant/stale-reference class the `0008` composite-FK pattern was built to eliminate.

---

## 11. Data-model health scorecard

| Dimension | Grade | Notes |
|---|---|---|
| Type discipline (money/rate/litre, timestamptz, uuid) | **A+** | 0 violations across 105 tables; IDENTITY over serial; numeric precision exactly per convention. |
| Tenant integrity — operational/equipment/procurement layers | **A** | Rigorous composite `(tenant_id,id)` FK chaining; multi-column invariants enforced in-DB. |
| Tenant integrity — finance/accounting layer | **C** | Systemic unFK'd reference columns (DB-001/002/003/005) abandon the backstop in the highest-stakes domain. |
| Referential integrity (ON DELETE/UPDATE choices) | **A−** | RESTRICT/CASCADE/SET NULL chosen appropriately throughout; only gap is the missing-FK class, not wrong cascade behavior. |
| Constraints (CHECK/NOT NULL/UNIQUE/idempotency) | **A** | Excellent status CHECKs, range CHECKs, GiST period exclusion, and partial-unique idempotency keys. |
| Index coverage | **A−** | FKs and tenant_id consistently indexed; query-shape composites strong; minor gaps (DB-002/012) and tenant-leading-column nits (DB-011). |
| Normalization & modeling | **A−** | Append-only ledgers, snapshotting, read-models all modeled correctly; `uuid[]` in 0053 (DB-006) is the one real smell. |
| Down-migration correctness | **A−** | Dependency-correct, RESTRICT-aware; lossy column-drop downs unannotated (DB-019); one cosmetic no-op (DB-018). |
| Naming & migration hygiene | **A−** | Highly consistent; immutable/forward-fix discipline; minor `uq_`-index and undocumented-category nits (DB-010/020); prior `idx_cpa_*` collision already resolved. |
| Permission/role seeding | **A−** | Sensible least-privilege grants, dependency-correct downs; early seeds lack `ON CONFLICT` (DB-014). |
| **Overall** | **A−** | A genuinely well-engineered schema. Closing the finance-layer FK gap (DB-001/002/003/005) would lift it to a clean A. |

*End of report.*
