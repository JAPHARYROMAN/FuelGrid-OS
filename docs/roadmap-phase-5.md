# Phase 5 — Supply Chain & Procurement

The phase where FuelGrid OS records **where the fuel comes from and what it costs**. Phase 4 answered *did the fuel we think we have match the tank?* — but it only knew *that* fuel arrived and *how much*. Phase 5 wraps the commercial workflow around the truck: *who* supplied it, *what was ordered*, *what actually arrived versus what was ordered*, and *what it cost landed in the tank*.

Phase 5 turns the minimal Phase-4 delivery into a full **goods receipt** matched against a **purchase order** from a real **supplier**, and captures the **landed cost per litre** — the price plus freight, duty, and levies, divided across the litres received. That landed cost is the **cost basis** of every litre in the tank.

This is the layer that prices the **inflow**. Phase 6 (Sales, Payments & Revenue) will price the **outflow** — and the gap between the two is margin. Phase 5 deliberately stops at *what we paid*; it does **not** value the whole stock ledger, compute COGS, or settle payments. It produces the procurement facts those later phases consume: a cost basis for inventory valuation, and approved supplier invoices for the Finance phase to pay.

## Stack decisions (carried forward from Phases 1–4)

All Phase-5 work continues to ride the patterns locked in earlier:

| Concern | Continued choice |
|---|---|
| Backend transactions | One tx wraps the business change + audit + outbox |
| Tenant scoping | Every repo query takes `tenantID` first; RLS is the safety net |
| Tenant-bound FKs | Children carry `(tenant_id, …)` composite FKs onto parent unique keys |
| Authorization | `requirePermission(code, scopeExtractor)` for URL-scoped, `authorizeStation(...)` in-handler when the station comes from the body/row, `requirePermissionHeld(code)` for tenant-wide reads |
| Migrations | One concern per file; system permissions seeded inline |
| Numeric precision | Litres `numeric(14, 3)`; money `numeric(14, 2)`; per-litre cost `numeric(14, 4)`. Money/cost values are parsed and carried as decimals, never `float64`/JS `number` (continuing the Phase-4 discipline). |
| Stock ledger | Receipts post stock-in movements through the **Phase-4 `stock_movements` ledger** — append-only, reversing-entry corrections. Phase 5 adds attribution + cost, not a parallel ledger. |
| Frontend | shadcn-style primitives in `@fuelgrid/ui`; TanStack Query over a hand-written `@fuelgrid/sdk` |

New conventions specific to Phase 5:

| Concern | Convention |
|---|---|
| PO lifecycle | `purchase_orders`: `draft → submitted → confirmed → partially_received → received → closed`; `cancelled` is terminal. Every transition writes audit + outbox. |
| Receipt → ledger | A goods receipt posts exactly one `delivery` stock-in movement (keyed to the receipt id, idempotent), now carrying `supplier_id`, `purchase_order_id`, and the landed cost basis. The Phase-4 delivery intake **evolves into** this receipt — not a second table. |
| Landed cost | `landed_cost_per_litre = (line price + freight + duty + levies) ÷ litres received`, snapshotted on the receipt at receive time so a later price correction can't rewrite history. This is the cost basis; selling price is Phase 6. |
| Three-way match | PO ↔ goods receipt ↔ supplier invoice. Quantity and price within tolerance auto-match; over tolerance raises a blocking discrepancy (reusing the Phase-3/4 exception-resolve pattern) before the invoice can be approved. |
| Scope | Suppliers are tenant-wide (a vendor serves many stations). Purchase orders and receipts are **station-scoped** — fuel is ordered for and received at a specific station. |

---

## Category A — Vendors & ordering

Who we buy from, and the orders we place.

### Stage 1 — Suppliers

**Goal:** A tenant maintains a supplier master so every order, receipt, and invoice traces to a known vendor.

- [ ] Migration `suppliers` (tenant, name, code, contact, payment terms, products supplied, status) with soft-delete + status lifecycle, mirroring the Phase-2 catalogue pattern
- [ ] Permission `supplier.manage` (tenant-wide) for writes; reads ride `purchase_order.read`/tenant-wide
- [ ] Repo + handlers + SDK: list, get, create, update, deactivate (guarded against deletion while referenced by an open PO)
- [ ] Audit + outbox: `supplier.created`, `supplier.updated`, `supplier.deactivated`
- [ ] Seed: a demo supplier for the demo tenant

**Done when:** A supplier can be created, listed, and deactivated; deactivation is refused while an open PO references it.

---

### Stage 2 — Purchase orders

**Goal:** A station raises a purchase order against a supplier for a product and quantity at an agreed price, moving through a clear lifecycle.

- [ ] Migration `purchase_orders` + `purchase_order_lines` (supplier, station, product, ordered litres, unit price, expected delivery, status, raised_by, timestamps); composite tenant + station FKs
- [ ] PO state machine: `draft → submitted → confirmed → partially_received → received → closed`; `cancelled` terminal. Status transitions guarded (e.g. can't cancel a received PO)
- [ ] Permissions: `purchase_order.manage` (raise/edit, station-scoped), `purchase_order.approve` (submit/confirm)
- [ ] Repo + handlers + SDK: list (by station/supplier/status), get, create, update lines (draft only), transition status
- [ ] Audit + outbox: `purchase_order.created`, `purchase_order.submitted`, `purchase_order.confirmed`, `purchase_order.cancelled`
- [ ] Seed: a confirmed PO for the demo station's PMS tank product

**Done when:** A PO is raised in `draft`, submitted, and confirmed; editing lines after submission is refused; cancelling a received PO is rejected.

---

## Category B — Receiving

The truck arrives — match it to the order and price it into the tank.

### Stage 3 — Goods receiving & PO matching

**Goal:** Fuel received is recorded as a goods receipt linked to its purchase order and supplier, with the ordered-versus-received quantity reconciled, and posts the stock-in movement.

- [ ] Evolve the Phase-4 `deliveries` row into a **goods receipt**: add `supplier_id`, `purchase_order_id`, `po_line_id`, and the received quantity per line; keep the existing dip-before/after cross-check
- [ ] Receiving posts (or reuses) the single `delivery` stock-in movement on the Phase-4 ledger in the same tx, now attributed to the supplier + PO
- [ ] Quantity match: compare received litres to the PO line's ordered litres; an over/under-delivery beyond tolerance flags a discrepancy. Advance the PO to `partially_received` / `received` accordingly
- [ ] Permission: reuse `delivery.receive` (station-scoped) for receiving; matching folds in
- [ ] Audit + outbox: `goods_receipt.recorded`, `purchase_order.received`
- [ ] Endpoints: receive against a PO; list a station's receipts; get a receipt with its match status

**Done when:** Receiving 9,800 L against a 10,000 L PO line posts a +9,800 L stock movement, flags the 200 L short-delivery, and moves the PO to `partially_received`; receiving the balance closes it to `received`.

---

### Stage 4 — Priced receipt & landed cost

**Goal:** Each receipt captures its landed cost so every litre in the tank carries a cost basis.

- [ ] Add cost columns to the receipt: line unit price, freight, duty, levies, computed `landed_cost_total` and `landed_cost_per_litre` (`numeric(14, 4)`), snapshotted at receive time
- [ ] Default the unit price from the PO line; allow an override at receive (price variance feeds the Stage-5 match)
- [ ] Attach the cost basis to the receipt's stock-in movement so Phase 6 can value stock and COGS from it — Phase 5 records the cost, Phase 6 does the valuation
- [ ] Permission: `delivery.receive` (cost entry folds into receiving)
- [ ] Audit + outbox: `goods_receipt.priced`
- [ ] Endpoint: the receipt response exposes landed cost per litre + the cost breakdown

**Done when:** A receipt of 9,800 L at 2,500/L + 150,000 freight stores `landed_cost_per_litre ≈ 2,515.31` and surfaces it on the receipt; the figure is frozen against later price edits.

---

## Category C — Payables & matching

Reconciling the supplier's bill against what we ordered and received.

### Stage 5 — Supplier invoices & three-way match

**Goal:** A supplier invoice is matched against its purchase order and goods receipt, with quantity and price discrepancies surfaced before approval.

- [ ] Migration `supplier_invoices` + lines (supplier, PO, invoice number, invoiced qty + amount, status, received_at)
- [ ] Three-way match: PO (ordered) ↔ goods receipt (received) ↔ invoice (billed); within-tolerance auto-matches, over-tolerance raises a `procurement_discrepancy` exception that blocks approval
- [ ] Permission: `invoice.manage` (record/match, tenant or station-scoped)
- [ ] Audit + outbox: `supplier_invoice.recorded`, `procurement_discrepancy.raised`
- [ ] Endpoints: record an invoice against a PO; get an invoice with its match result + discrepancies

**Done when:** An invoice billing 10,000 L against a 9,800 L receipt raises a quantity discrepancy that blocks approval; an in-tolerance invoice matches clean.

---

### Stage 6 — Invoice approval & payables handoff

**Goal:** A matched invoice is approved and emitted as a payable for the Finance phase, with discrepancies resolved first.

- [ ] Approval handler (one tx): flips a matched invoice to `approved`, refusing while unresolved discrepancies remain; reuse the exception-resolve pattern
- [ ] Emit a `payable.created` outbox event carrying the supplier, amount, and due date — the Finance phase consumes it; Phase 5 does **not** execute payment
- [ ] Supplier balance / aging view: outstanding approved-but-unpaid invoices per supplier
- [ ] Permission: `invoice.approve`
- [ ] Audit + outbox: `supplier_invoice.approved`, `procurement_discrepancy.resolved`, `payable.created`

**Done when:** Approving a clean invoice emits a `payable.created` event and the supplier's outstanding balance reflects it; an invoice with an open discrepancy can't be approved until it's resolved.

---

## Category D — Procurement surfaces

The daily UX for the people buying the fuel.

### Stage 7 — Procurement dashboard

**Goal:** A manager opens one screen and sees open purchase orders, expected/in-transit deliveries, recent receipts, and the price trend per product and supplier.

- [ ] Route `/procurement`: open POs by status, upcoming/overdue expected deliveries, recent receipts with landed cost, a price-trend sparkline per product/supplier
- [ ] Backend: `GET /api/v1/stations/{id}/procurement-overview` — open POs, recent receipts, supplier balances, price trend, in one call
- [ ] Permission gate: `purchase_order.read`
- [ ] Mobile responsive

**Done when:** `/procurement` for a station shows its open POs, the last receipts with landed cost per litre, and the recent price trend for each product.

---

### Stage 8 — Receiving & matching console

**Goal:** A supervisor runs the receive-and-match workflow from one console: receive against an open PO, enter costs, record the supplier invoice, and resolve match discrepancies.

- [ ] Route `/procurement/receiving` (or a tab on `/procurement`): pick an open PO, enter received litres + dip + costs, see the live quantity/price match, and record the invoice
- [ ] Inline receive / price / invoice / resolve-discrepancy actions wired to Stage 3–6 endpoints
- [ ] Permission gate: `delivery.receive` to receive, `invoice.manage`/`invoice.approve` for invoicing
- [ ] Mobile responsive: receiving cards stack below 768px

**Done when:** A supervisor receives a delivery against a PO, enters its costs, records the matching invoice, and approves it — all from `/procurement/receiving`, with the stock-in movement and payable emitted.

---

## Phase 5 acceptance criteria

Phase 5 is complete when **all** of the following are true:

1. A tenant maintains suppliers and raises station-scoped purchase orders that move through a guarded lifecycle.
2. Fuel is received as a goods receipt linked to its PO + supplier, posting the single stock-in movement on the Phase-4 ledger, with ordered-vs-received quantity reconciled.
3. Each receipt captures a landed cost per litre, snapshotted as the cost basis Phase 6 will value stock from.
4. Supplier invoices three-way-match against PO and receipt; over-tolerance discrepancies block approval until resolved.
5. Approved invoices emit a payable for the Finance phase; supplier balances reflect outstanding amounts.
6. Every supplier, PO, receipt, and invoice action rides audit + outbox like every prior sensitive write.
7. Operators get a procurement dashboard and a receiving-and-matching console.

---

## Out of scope for Phase 5 (intentionally)

Reserved for later phases — don't let scope creep pull them in:

- **Stock valuation, COGS, margin, and selling price** — Phase 6 (Sales, Payments & Revenue). Phase 5 records the **cost basis** (what we paid); Phase 6 values stock and prices sales against it.
- **Payment execution, banking, cash-office reconciliation, supplier remittance** — the Finance phase. Phase 5 emits a payable; it does not pay it.
- **Demand forecasting, auto-replenishment, reorder-point optimization** — a later analytics/intelligence phase. Phase 5's POs are raised by people.
- **Tenders, contract management, supplier scorecards / SLA tracking** — a later vendor-management phase.
- **Customs/duty filing & statutory fuel-levy remittance integrations** — a later compliance/integrations phase. Phase 5 records duty/levy as receipt cost components, not statutory filings.
- **Inter-station stock transfers** — the Phase-4 `transfer` movement type stays reserved; the transfer workflow is still later.

---

## Cross-phase considerations

A few Phase-5 decisions lock shape for later phases:

- **Landed cost per litre is the cost basis** Phase 6 values inventory and computes COGS/margin from. Snapshotting it on the receipt (immutable) is what makes downstream valuation trustworthy.
- **Approved supplier invoices become payables** the Finance phase settles; the `payable.created` event is the contract between procurement and finance.
- **Receipts post through the Phase-4 ledger**, evolving the minimal delivery into a priced, attributed receipt — exactly the "delivery → priced receipt" extension Phase 4 anticipated. The ledger shape doesn't change; attribution + cost are layered on.
- **Supplier + PO become dimensions** for Phase-10 risk/fraud (price manipulation, collusion, phantom deliveries) and for reporting; capturing them cleanly now is what makes that analysis possible.

If any of these change, the migration story for Phase 6+ will need careful sequencing.
