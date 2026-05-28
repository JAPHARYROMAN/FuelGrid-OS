# Phase 6 — Sales, Payments & Revenue

The phase where FuelGrid OS turns **litres sold into money earned**. Phase 4 metered the **outflow** in litres and reconciled it against the tank; Phase 5 priced the **inflow** — the landed cost of every litre received. Phase 6 prices the **outflow**: it sets the selling price, values each approved shift's metered litres into **recognized revenue**, values those same litres at their Phase-5 landed cost to compute **COGS and margin**, records **how customers paid** (cash, mobile money, card, credit), tracks **credit customers and what they owe**, and closes the day's revenue into a **lockable financial period**.

Phase 6 builds on the trust anchors already in place. Revenue is recognized on **shift approval** — the very same signed-off litres Phase 4 posts to the stock ledger — so every shilling of revenue traces to a metered litre. COGS values those litres against the **Phase-5 landed cost basis**: Phase 5 records what fuel cost, Phase 6 values stock and prices the sale against it. The gap between recognized revenue and COGS is **margin** — the number the whole business runs on.

This is the layer that **closes the commercial loop**. It deliberately stops at *revenue*: it does **not** execute supplier payments, run banking or cash-office consolidation, post a general ledger, or file taxes — those are the later **Finance/GL** and **compliance** phases. Phase 6 produces the revenue, margin, tender, and receivables facts those phases consume.

## Stack decisions (carried forward from Phases 1–5)

All Phase-6 work continues to ride the patterns locked in earlier:

| Concern | Continued choice |
|---|---|
| Backend transactions | One tx wraps the business change + audit + outbox |
| Tenant scoping | Every repo query takes `tenantID` first; RLS is the safety net |
| Tenant-bound FKs | Children carry `(tenant_id, …)` composite FKs onto parent unique keys |
| Authorization | `requirePermission(code, scopeExtractor)` for URL-scoped, `authorizeStation(...)` in-handler when the station comes from the body/row, `requirePermissionHeld(code)` for tenant-wide reads |
| Migrations | One concern per file; system permissions seeded inline |
| Numeric precision | Litres `numeric(14, 3)`; money `numeric(14, 2)`; per-litre price/cost `numeric(14, 4)`. Money is parsed and carried as decimals, never `float64`/JS `number` (continuing the Phase-4/5 discipline). |
| Recognition anchor | Revenue posts on `shift.approved` — the same hook Phase-4 sales draw-down uses; never on open or closed-but-unapproved shifts. |
| Stock ledger | Phase 6 **reads** the Phase-4 `stock_movements` ledger to value stock and COGS; it adds money tables, not a parallel litres ledger. |
| Frontend | shadcn-style primitives in `@fuelgrid/ui`; TanStack Query over a hand-written `@fuelgrid/sdk` |

New conventions specific to Phase 6:

| Concern | Convention |
|---|---|
| Money & currency | All money is `numeric(14, 2)` in the **company's single currency** (`companies.currency`); decimals, never float. |
| Price immutability | A sale **snapshots** the unit price, tax rate, and unit cost it transacted at; a later price or cost change never rewrites past revenue (mirrors Phase-4 dip/chart snapshots and Phase-5 landed-cost snapshots). |
| Cost basis | COGS values litres at the **Phase-5 moving-average landed cost**. Phase 6 values; Phase 5 supplies. |
| Financial ledgers | Revenue and AR are **append-only**; a correction is a reversing entry, never an in-place edit (continuing the Phase-4 ledger discipline). |
| Idempotency | Revenue posting is keyed to the shift (per nozzle/product), like Phase-4 sales — re-approval or outbox replay never double-recognizes. |
| Period lock | A **locked** financial period freezes its revenue records — the financial analog of the Phase-4 reconciliation seal. |
| Scope | Selling prices and revenue are **station-scoped**; credit customers are **tenant-wide** (a customer fuels across stations). |

---

## Category A — Pricing

The selling price, and how it changes.

### Stage 1 — Selling price book

**Goal:** Every product at every station has a current, effective-dated selling price, and every price change is an audited event.

- [ ] Migration `price_changes` (tenant, station, product, unit_price, effective_from, previous_price snapshot, reason, set_by, set_at) — append-only and effective-dated; the active price is the latest `effective_from ≤ now` per (station, product), mirroring the Phase-2 calibration-chart versioning
- [ ] `internal/pricing` package: set a price (records a change inside the caller's tx), resolve the active price for (station, product) at a point in time, list a product's price history
- [ ] Permission `price.change` (exists from 0004, station-scoped) for writes; reads ride a new tenant-wide `pricing.read`
- [ ] Repo + handlers + SDK: set a price, get a station's current price board, list a product's price history
- [ ] Audit + outbox: `price.changed`
- [ ] Seed: opening selling prices for the demo station's products

**Done when:** Setting PMS to 2,950 at a station records a price change and the active price resolves to it; the prior price is retained in history.

---

### Stage 2 — Scheduled changes & price board

**Goal:** Prices can be scheduled to take effect later, are guarded against selling below cost, and the forecourt price board reads in one call.

- [ ] Future-dated `effective_from`: a change scheduled ahead leaves today's resolved price untouched and activates automatically when its time arrives (resolution is time-based — no cron)
- [ ] Below-cost guard: warn/block setting a selling price below the product's current landed cost (Phase-5 basis); crossing it requires the override path
- [ ] Endpoint: station price board — every product's active price plus its next scheduled change, for forecourt display
- [ ] Audit + outbox: `price.scheduled`
- [ ] Repo + handlers + SDK: schedule a change, read the board

**Done when:** A price scheduled for tomorrow doesn't change today's resolved price but becomes active at its effective time; a below-cost price is flagged before it's set.

---

## Category B — Sales valuation, COGS & margin

Valuing what flowed out — and what it cost.

### Stage 3 — Recognized sales & revenue

**Goal:** Approving a shift values its metered litres-sold at the resolved selling price into recognized sales records, split into net + tax.

- [ ] Migration `sales` (tenant, shift, station, operating_day, nozzle, product, tank, litres, unit_price snapshot, gross_amount, tax_rate snapshot, tax_amount, net_amount, recorded_at) — one per nozzle per approved shift; the authoritative recognized-revenue record built from the Phase-3 `shift_close_lines` (which already froze litres_sold × unit_price at close)
- [ ] On `shift.approved` (the Phase-4 sales-posting hook), value each close line at the price resolved for its product/station and post a sale; **idempotent** per shift (keyed shift + nozzle), like Phase-4 sales movements; a product with no resolvable price is skipped with a logged signal (additive, like Phase-4's un-opened-tank skip)
- [ ] Permission `revenue.read` (new, station-scoped) for reads; posting is system-driven on approval
- [ ] Repo + handlers + SDK: list sales by shift/day/station/product; revenue totals
- [ ] Audit + outbox: `sale.recorded`
- [ ] Backfill consideration: a documented path for recognizing revenue from shifts approved before this stage shipped (mirrors Phase-4 Stage 4)

**Done when:** Approving a shift that sold 4,200 L of PMS at 2,950 records gross 12,390,000 split into net + tax, attributed to PMS; re-approval does not double-recognize.

---

### Stage 4 — Stock valuation, COGS & margin

**Goal:** Value the litres sold at their landed cost to compute COGS and gross margin, and value the stock still on hand.

- [ ] Moving-average cost per tank/product, maintained from the Phase-5 receipt cost basis (the landed cost on each `delivery` stock-in movement); read-side computation over the Phase-4 ledger
- [ ] COGS per sale = litres × cost-at-sale, **snapshotted on the sale** so a later cost change can't rewrite it; margin = net revenue − COGS, rolled up per sale/shift/day/product
- [ ] Stock valuation: value current book stock (Phase-4 `CurrentBalance`) at moving-average cost → inventory value per tank/station
- [ ] Permission `margin.view` (exists, station-scoped) for margin + valuation reads
- [ ] Repo + handlers + SDK: margin by shift/day/product; inventory valuation per station
- [ ] Audit + outbox: COGS folds onto the `sale.recorded` payload (cost + margin), or `cogs.posted`

**Done when:** A PMS sale of 4,200 L at landed cost 2,515.31 shows COGS ≈ 10,564,302 and margin = net revenue − COGS; stock-on-hand valuation reflects the moving-average cost.

---

## Category C — Tender & receivables

How customers paid, and what they still owe.

### Stage 5 — Tender capture & revenue reconciliation

**Goal:** A shift's tendered amounts by type are recorded as payments and reconciled against recognized revenue, surfacing the over/short.

- [ ] Migration `payments` (tenant, station, shift, tender_type `cash | mobile_money | card | credit | voucher`, amount, reference, received_by, received_at, status) — append-only; evolves the Phase-3 `cash_submissions` per-tender fields into discrete payment records tied to **recognized revenue**, not just the close-time expected cash
- [ ] Reconcile total tendered vs recognized revenue per shift → variance; an over-threshold short raises a `cash_variance` exception (reuse the Phase-3 exception-resolve pattern)
- [ ] Permission `payment.record` (new, station-scoped); reuse `cash.submit` where it already fits the attendant path
- [ ] Repo + handlers + SDK: record/list a shift's payments; the shift revenue-vs-tender reconciliation
- [ ] Audit + outbox: `payment.recorded`, `shift_revenue.reconciled`

**Done when:** Recording a shift's cash + mobile-money + card + credit tenders totals against recognized revenue and surfaces the variance; an over-threshold short raises a blocking exception.

---

### Stage 6 — Credit customers & receivables

**Goal:** Credit sales charge a customer's account against a limit, customer payments settle it, and a statement shows the running balance.

- [ ] Migration `customers` (tenant-wide credit-customer master: name, code, contact, credit_limit, status) + `ar_entries` (customer, type `charge | payment | adjustment`, amount, source sale/payment/shift ref, balance_after, recorded_by, recorded_at) — an **append-only AR ledger**, balance derived by summing forward (the Phase-4 ledger pattern)
- [ ] A shift's `credit` tender allocates to a customer → AR `charge`; enforce the credit limit, with `credit.override_limit` to exceed it; customer payments → AR `payment` entries
- [ ] Permissions: `credit.manage` (exists, tenant-wide), `credit.override_limit` (exists), `customer.read` (new)
- [ ] Repo + handlers + SDK: manage customers; allocate a credit sale; record a customer payment; customer statement (AR ledger + balance + aging)
- [ ] Audit + outbox: `customer.created`, `credit_sale.posted`, `customer_payment.recorded`

**Done when:** A credit sale raises a customer's AR balance and is refused when it would breach the limit unless overridden; a payment reduces the balance; the statement reconciles to the ledger.

---

## Category D — Revenue close & surfaces

Closing the books on the day, and the screens that read them.

### Stage 7 — Daily revenue close & period lock

**Goal:** Roll up revenue, COGS, margin, and tender per station per day, and lock a financial period to freeze the figures.

- [ ] Migration `revenue_days` (tenant, station, operating_day, business_date, gross_revenue, net_revenue, tax_total, cogs_total, margin_total, tender totals by type, cash_variance_total, status `draft | locked`, locked_by, locked_at) — one per station per day, computed from sales + payments
- [ ] Compute on day close — after all the day's shifts are approved (and ideally its tanks reconciled, Phase-4 seal); locking freezes it (`period.lock`), the financial analog of the reconciliation seal
- [ ] Permissions: `revenue.read` (view), `period.lock` (lock; exists)
- [ ] Repo + handlers + SDK: compute/get a station's daily revenue; lock a period (guarded against re-lock)
- [ ] Audit + outbox: `revenue_day.computed`, `period.locked`

**Done when:** Closing a day produces a revenue summary (gross/net/tax, COGS, margin, tender mix, cash variance) and locking it freezes the figures against edits.

---

### Stage 8 — Revenue dashboard & exports

**Goal:** Finance and managers see revenue, margin, tender mix, and receivables from one screen, and export reports.

- [ ] Route `/revenue`: per-station/day gross & net revenue, margin %, tender mix, top products, AR aging, recent revenue trend
- [ ] Backend: `GET /api/v1/stations/{id}/revenue-overview` — the day's revenue, COGS, margin, tender breakdown, and recent trend in one call; plus an AR-aging endpoint
- [ ] Permission gate: `revenue.read` to view, `margin.view` for cost/margin figures; exports via `reports.export` (exists)
- [ ] Mobile responsive: cards stack below 768px

**Done when:** `/revenue` for a station shows the day's revenue, margin, and tender breakdown, the recent revenue trend, and AR aging; a finance officer can export it.

---

## Phase 6 acceptance criteria

Phase 6 is complete when **all** of the following are true:

1. Every product at every station has an effective-dated selling price; changes are audited and can be scheduled ahead.
2. Approving a shift recognizes its metered litres-sold as revenue at the resolved price, split net + tax, idempotently.
3. COGS values the same litres at the Phase-5 landed cost basis; margin = net revenue − COGS; stock on hand is valued.
4. Shift tenders by type are recorded and reconciled against recognized revenue; over-threshold variances raise exceptions.
5. Credit sales charge customers against limits; payments settle them; statements reconcile to the AR ledger.
6. A station's day rolls up into a revenue summary that locks into a frozen financial period.
7. Every price, sale, payment, AR, and revenue-close action rides audit + outbox; operators get a revenue dashboard.

---

## Out of scope for Phase 6 (intentionally)

Reserved for later phases — don't let scope creep pull them in:

- **Supplier payment execution, banking, cash-office consolidation, supplier remittance** — the Finance phase (consistent with Phase 5). Phase 6 records customer tenders and receivables, not bank settlement or money going out.
- **General ledger / double-entry accounting & financial statements** — the Finance/GL phase. Phase 6 produces revenue, margin, and AR facts; the GL phase posts them into double-entry.
- **Bank / mobile-money / card settlement integrations** (live feeds, auto-matching deposits to recorded tenders) — the integrations phase. Phase 6 records tenders + references; external settlement matching is later.
- **Tax filing, e-invoicing, statutory revenue reporting** — a compliance phase. Phase 6 computes tax amounts; it does not file them.
- **Promotions, discounts, loyalty, dynamic/zone pricing** — a later pricing/CRM phase. Phase 6 transacts at the posted selling price.
- **Multi-currency / FX** — a single company currency for now.
- **Revenue / margin fraud & anomaly scoring** — Phase 10 (Risk, Fraud & Intelligence). Phase 6 raises only the mechanical over-threshold tender variance.

---

## Cross-phase considerations

A few Phase-6 decisions lock shape for later phases:

- **Revenue is recognized on the Phase-4 shift-approval anchor** — litres and their money valuation share the (shift, tank/product) key, so every shilling traces to a metered litre. Getting recognition tied to the signed-off shift (not open/closed) is what makes the numbers trustworthy.
- **Price, tax, and cost snapshots on sales** mirror Phase-4 dip and Phase-5 landed-cost snapshots — a later price or cost change never rewrites recognized revenue.
- **COGS consumes Phase-5's landed cost basis**; the moving-average cost is the seam between procurement cost and sales margin. Phase 5 records the cost, Phase 6 values against it.
- **The revenue ledger, AR ledger, and `revenue_days`** are what the Finance/GL phase will post into double-entry; their append-only, source-attributed shape keeps that additive.
- **Period lock is the financial analog of the reconciliation seal** — both freeze a period and anchor the figures regulators and Phase-10 fraud detection lean on.

If any of these change, the migration story for the Finance/GL phase and beyond will need careful sequencing.
