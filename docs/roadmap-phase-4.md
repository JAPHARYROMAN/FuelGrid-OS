# Phase 4 — Inventory & Reconciliation Engine

The phase where FuelGrid OS turns captured readings into **trusted stock truth**. Phase 3 recorded what the meters and dips said per shift; Phase 4 takes those numbers and answers the question every fuel business lives or dies by: *does the fuel we think we have match the fuel actually in the tank?*

Phase 4 builds a per-tank **stock ledger** — an append-only record of every litre that moved in (deliveries) and out (metered sales) — and reconciles the **book stock** it computes against the **physical stock** measured by the closing dip. The gap between them is variance: the gain or loss that, left unwatched, is where fuel businesses bleed money.

This is the layer that **closes the loop** on Phase 3. Phase 3 produces the inputs (meter-delta litres, snapshotted dip volumes, person-attributed, append-only); Phase 4 consumes them to produce a signed-off, audited inventory position per tank per operating day. It does **not** price those movements (Phase 6) or run full procurement (the later supply-chain phase) — it stays in litres and tank levels, the operator's physical reality.

## Stack decisions (carried forward from Phases 1–3)

All Phase-4 work continues to ride the patterns locked in earlier:

| Concern | Continued choice |
|---|---|
| Backend transactions | One tx wraps the business change + audit + outbox |
| Tenant scoping | Every repo query takes `tenantID` first; RLS is the safety net |
| Tenant-bound FKs | Children carry `(tenant_id, …)` composite FKs onto parent unique keys |
| Authorization | `requirePermission(code, scopeExtractor)` for URL-scoped, `authorizeStation(...)` in-handler when the station comes from the body/row, `requirePermissionHeld(code)` for tenant-wide reads |
| Migrations | One concern per file; system permissions seeded inline |
| Numeric precision | Litres `numeric(14, 3)`; money `numeric(14, 2)` (money stays unused until Phase 6) |
| Frontend | shadcn-style primitives in `@fuelgrid/ui`; TanStack Query over a hand-written `@fuelgrid/sdk` |

New conventions specific to Phase 4:

| Concern | Convention |
|---|---|
| Ledger immutability | Stock movements are append-only, like readings. A correction is a **reversing entry**, never an `UPDATE` of a posted movement. |
| Source attribution | Every movement records its `source` (`opening`, `delivery`, `sales`, `adjustment`, `transfer`) and a reference to the row that caused it (delivery id, shift id, adjustment id), so book stock is always traceable to a cause. |
| Book stock | Computed by summing the ledger forward from the last reconciled balance. The ledger is the source of truth; a per-tank running balance is derived, not authoritative. |
| Sales posting trigger | Metered sales post to the ledger only when a Phase-3 shift is **approved** — Phase 4 builds exclusively on signed-off numbers, never on open/closed-but-unapproved shifts. |
| Reconciliation grain | One reconciliation per `(tank, operating_day)`. It freezes the book figure, the physical (closing dip) figure, and the variance — a re-strap or late correction can't rewrite a sealed reconciliation. |
| Variance classification | Variance is judged against the product's `loss_tolerance_percent` (Phase-2 config): within tolerance is informational; over tolerance raises a blocking exception. |

---

## Category A — The stock ledger

The append-only spine: every litre in and out of every tank, traceable to a cause.

### Stage 1 — Stock ledger & movements

**Goal:** Each tank has an append-only ledger of stock movements, every one attributed to a source, so book stock can be computed and audited at any point in time.

- [ ] Migration: `stock_movements` (tank, movement type, source, source reference, litres signed +in/−out, balance-after, recorded_by, recorded_at, supersedes_id, status) — append-only with a reversing-entry correction path
- [ ] Movement types: `opening`, `delivery`, `sales`, `adjustment`, `transfer`; composite tenant FKs to `tanks` and (nullably) the source row
- [ ] `internal/inventory` package: post a movement (inside the caller's tx), compute current book balance for a tank, list movements for a tank/period
- [ ] Permission `inventory.read` (station-scoped) for reads; movement writes are internal/system-driven plus `inventory.adjust` for manual ones
- [ ] Audit + outbox: `stock_movement.posted`, `stock_movement.reversed`
- [ ] Endpoints: list a tank's ledger; get a tank's current book balance

**Done when:** A tank's ledger returns an ordered movement history and a current book balance computed by summing the ledger; a reversing entry corrects a movement without mutating the original.

---

### Stage 2 — Opening balances & balance-forward

**Goal:** Every tank starts its ledger from a known opening balance, and each operating day's closing book stock carries forward as the next day's opening, so the ledger never drifts from a manual reset.

- [ ] Seed/initialize an `opening` movement per tank from its first dip (or a manual opening-stock entry)
- [ ] Balance-forward: a sealed reconciliation's physical figure becomes the next period's opening book balance (the reconciliation is the trust anchor, not the raw ledger sum)
- [ ] Guard: a tank can't post sales/delivery movements before it has an opening balance
- [ ] Endpoint: set/adjust a tank's opening balance (audited, `inventory.adjust`)

**Done when:** A freshly configured tank gets an opening balance from its first dip; after a day is reconciled, the next day opens with book stock equal to the prior day's signed-off physical stock.

---

## Category B — Inflows & outflows

What moves fuel in and out — feeding the ledger from deliveries and from Phase-3 sales.

### Stage 3 — Delivery intake

**Goal:** Fuel received into a tank is recorded and posted to the ledger as a stock-in movement, so book stock reflects every replenishment.

- [ ] Migration: `deliveries` (tank, supplier reference (free-text for now), volume received litres, dip-before / dip-after optional, received_by, received_at, notes) — minimal intake, **not** full procurement
- [ ] Capture path posts a `delivery` stock-in movement to the ledger in the same tx
- [ ] Permission `delivery.receive` (station-scoped)
- [ ] Optional cross-check: compare declared volume received against the dip-before→dip-after delta; flag a mismatch
- [ ] Audit + outbox: `delivery.received`
- [ ] Endpoints: receive a delivery; list a tank's/station's deliveries

**Done when:** Receiving a 10,000 L delivery into a tank posts a +10,000 L `delivery` movement and the tank's book balance rises by 10,000 L; a declared-vs-dip mismatch is surfaced.

> **Scope note:** this is *intake only* — enough to feed the ledger. Suppliers, purchase orders, goods-received-note matching, and delivery pricing are the later **supply-chain / procurement** phase. Phase 4 records *that* fuel arrived and *how much*, not the commercial paperwork around it.

---

### Stage 4 — Sales draw-down posting

**Goal:** When a Phase-3 shift is approved, its metered litres-sold post to the ledger as stock-out movements per tank, bridging captured sales into the inventory position.

- [ ] On `shift.approved` (Phase-3 Stage 6), aggregate the shift's per-nozzle litres-sold up to the tank behind each nozzle
- [ ] Post one `sales` stock-out movement per tank, referencing the shift, in the approval tx (so sales and the ledger commit atomically)
- [ ] Idempotency: re-approving / replaying never double-posts (movement keyed to the shift + tank)
- [ ] Audit + outbox: `stock_movement.posted` with `source = sales`
- [ ] Backfill consideration: a documented path for posting sales from shifts approved before this stage shipped

**Done when:** Approving a shift that sold 4,200 L of PMS posts a −4,200 L `sales` movement against the PMS tank, dropping its book balance accordingly; re-approval does not double-count.

---

## Category C — Reconciliation

Comparing the books to the dipstick, and signing off the difference.

### Stage 5 — Book-vs-physical reconciliation

**Goal:** For each tank each operating day, compute book stock from the ledger and compare it to physical stock from the closing dip, producing a variance classified against the product's loss tolerance.

- [ ] Migration: `tank_reconciliations` (tank, operating_day, opening_book, deliveries_total, sales_total, adjustments_total, closing_book, closing_physical (from dip), variance_litres, variance_percent, tolerance_percent, status, sealed_by, sealed_at)
- [ ] Compute: `closing_book = opening_book + deliveries − sales ± adjustments`; `variance = closing_book − closing_physical`; classify within/over `loss_tolerance_percent`
- [ ] Permission `reconciliation.read` (view) and `reconciliation.manage` (run/seal), station-scoped
- [ ] Guard: a reconciliation can only run once the day's shifts are approved and a closing dip exists for the tank
- [ ] Audit + outbox: `reconciliation.computed`
- [ ] Endpoint: compute (preview) and persist a draft reconciliation for a tank/day

**Done when:** Running reconciliation for a tank/day returns `opening + deliveries − sales` as book stock, the closing dip as physical stock, and a variance flagged within or over tolerance.

---

### Stage 6 — Variance exceptions, adjustments & sign-off

**Goal:** Over-tolerance variances surface as exceptions, supervisors record adjustments with reasons, and a sealed sign-off freezes the reconciliation and carries the physical figure forward.

- [ ] Auto-raise a `stock_variance` exception when variance exceeds tolerance; it blocks sealing until resolved
- [ ] Manual `adjustment` movements (gain/loss, evaporation, theft write-off) with a required reason — each posts to the ledger and re-computes the draft
- [ ] Seal: flip the reconciliation to `sealed`, stamp `sealed_by/at`, write the balance-forward opening for the next period (Stage 2)
- [ ] Permission `reconciliation.manage` for seal; reuse the variance-exception resolve pattern from Phase-3 Stage 6
- [ ] Audit + outbox: `reconciliation.adjusted`, `stock_variance.raised`, `reconciliation.sealed`

**Done when:** An over-tolerance variance raises an exception that blocks sealing; recording a justified adjustment brings the figure back within tolerance; sealing freezes the reconciliation and sets the next day's opening book stock.

---

## Category D — Operator surfaces

The daily UX for the people watching the fuel.

### Stage 7 — Inventory dashboard

**Goal:** A manager opens one screen and sees current book stock per tank, days-of-stock-remaining, and the recent variance trend — the at-a-glance health of the forecourt's fuel.

- [ ] Route `/inventory`: per-tank book balance vs physical (latest dip), days-of-stock estimate, last-reconciled date, variance sparkline/history
- [ ] Backend: `GET /api/v1/stations/{id}/inventory-overview` — tanks with book balance, latest physical, last reconciliation, recent variances, in one call
- [ ] Permission gate: `inventory.read`
- [ ] Mobile responsive

**Done when:** `/inventory` for a station shows each tank's current book stock, its latest physical reading, and whether the last reconciliation was within tolerance.

---

### Stage 8 — Reconciliation review console

**Goal:** A supervisor runs the daily reconcile-and-sign workflow from one console: review each tank's variance, record adjustments, resolve exceptions, and seal the day.

- [ ] Route `/reconciliation` (or a tab on `/inventory`): per-tank reconciliation cards for the active day with book vs physical, variance + tolerance badge, inline adjustment entry, exception resolve, and a seal action
- [ ] Backend: `GET /api/v1/stations/{id}/reconciliation-overview` — the day's per-tank reconciliations with figures, variances, exceptions, in one call
- [ ] Inline adjustment / resolve / seal actions wired to Stage 5–6 endpoints
- [ ] Permission gate: `reconciliation.read` to view, `reconciliation.manage` to adjust/seal
- [ ] Mobile responsive: cards stack below 768px

**Done when:** `/reconciliation` for a station shows the day's per-tank variances; a supervisor records an adjustment, the variance updates, and a one-click seal freezes the day's reconciliation.

---

## Phase 4 acceptance criteria

Phase 4 is complete when **all** of the following are true:

1. Every tank has an append-only stock ledger of opening, delivery, sales, and adjustment movements, each traceable to its source.
2. Approving a Phase-3 shift posts its metered litres-sold as stock-out movements per tank, idempotently.
3. Deliveries are received into a tank as stock-in movements that raise book stock.
4. A per-tank-per-day reconciliation computes book stock vs physical (closing dip) and classifies the variance against the product's loss tolerance.
5. Over-tolerance variances raise exceptions that block sign-off until resolved or adjusted; sealing carries the physical figure forward as the next day's opening.
6. Every movement, adjustment, and reconciliation rides audit + outbox like every Phase-1/2/3 sensitive write.
7. Operators get an inventory dashboard and a reconciliation review console.

---

## Out of scope for Phase 4 (intentionally)

Reserved for later phases — don't let scope creep pull them in:

- **Procurement: suppliers, purchase orders, goods-received-note matching, delivery pricing** — the supply-chain phase. Phase 4 records *that* fuel arrived and *how much*, not the commercial workflow.
- **Pricing & valuing stock movements (cost of goods, stock valuation in money)** — Phase 6 (Sales, Payments & Revenue). Phase 4 reconciles **litres**, not value.
- **Variance scoring, fraud signals, anomaly intelligence** — Phase 10 (Risk, Fraud & Intelligence). Phase 4 raises only the mechanical over-tolerance exception; pattern detection comes later.
- **Multi-tank transfers / inter-station stock movement workflows** — the `transfer` movement type is reserved in the ledger, but the transfer *workflow* (approvals, in-transit state) is later.
- **Wet-stock telemetry / automatic tank gauging (ATG) integration** — a later integrations phase. Phase 4's physical figure comes from the Phase-3 manual dip.

---

## Cross-phase considerations

A few Phase-4 decisions lock shape for later phases:

- **The stock ledger's movement-source pattern** is what the supply-chain phase (delivery → priced receipt) and the finance phase extend. Getting the append-only, source-attributed shape right now keeps those phases additive.
- **Sales post on shift approval, in litres.** Phase 6's revenue engine will value those same movements (litres × price); the ledger shape stays the same, money is layered on.
- **Sealed reconciliations are the trust anchor** regulators and Phase-10 fraud detection will lean on — variance history is only meaningful if each period was signed off and frozen.
- **`tank` + `operating_day` are the reconciliation grain**, continuing Phase 3's grouping keys; every downstream stock question references the day it was reconciled in.

If any of these change, the migration story for Phase 6+ will need careful sequencing.
