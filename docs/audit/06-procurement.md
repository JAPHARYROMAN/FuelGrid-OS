# Procurement Domain Audit — FuelGrid OS

**Scope:** Read-only, atomic-level audit of the Procurement domain (Phase 5): supplier master, purchase-order lifecycle, PO-backed goods receipt, supplier-invoice three-way match, procurement discrepancies, and the procurement overview surface. Findings are concrete and cite `file:line`. Uncertainty is marked explicitly.

## Files in scope (with LOC)

| File | LOC | Role |
|------|-----|------|
| `internal/procurement/repo.go` | 48 | Repo struct, sentinel errors, helpers |
| `internal/procurement/suppliers.go` | 232 | Supplier CRUD + product associations |
| `internal/procurement/purchase_orders.go` | 322 | PO lifecycle, lines, state machine |
| `internal/procurement/invoices.go` | 429 | Supplier invoices, 3-way match, discrepancies |
| `internal/procurement/overview.go` | 109 | Open POs, supplier balances, price trend |
| `services/api/internal/server/procurement_handlers.go` | 1331 | All procurement HTTP handlers + DTOs |
| `internal/inventory/deliveries.go` | 377 | `ReceiveGoodsReceipt` (PO receipt orchestration) — co-owned with inventory |
| `services/api/migrations/0028_suppliers.{up,down}.sql` | 72 / 8 | suppliers, supplier_products |
| `services/api/migrations/0029_purchase_orders.{up,down}.sql` | 116 / — | purchase_orders, purchase_order_lines |
| `services/api/migrations/0030_goods_receipts.{up,down}.sql` | 72 / 37 | deliveries/stock_movements procurement columns |
| `services/api/migrations/0031_supplier_invoices.{up,down}.sql` | 155 / 13 | supplier_invoices, lines, discrepancies |
| `services/api/internal/server/phase5_integration_test.go` | 137 | The single procurement integration test |

The inventory agent owns stock-ledger posting (`PostMovement`); this report covers PO/receipt/invoice orchestration and the procurement-relevant parts of `deliveries.go`.

---

## Flow 1 — Supplier CRUD

**Code:** `suppliers.go`, handlers `handleListSuppliers`/`handleGetSupplier`/`handleCreateSupplier`/`handleUpdateSupplier`/`handleDeactivateSupplier` (`procurement_handlers.go:187–446`).

Codes are unique per tenant case-insensitively via `idx_suppliers_tenant_code ON suppliers(tenant_id, lower(code))` (`0028:27`). Creation/update map `23505` to `409` through `isUniqueViolation` and `23503` to `404` through `isForeignKeyViolation` — correct. The composite FK convention is honoured (`supplier_products_supplier_fk`, `0028:39`).

**Money/tx discipline:** Supplier rows carry no money. Create and update each wrap business change + audit + outbox in one `tx` via `audit.WriteWithOutbox` (`procurement_handlers.go:286`, `378`) and commit once. Good.

**Defects:**

- **PROC-01 (Medium) — UpdateSupplier COALESCE cannot clear nullable contact fields.** `suppliers.go:142–149`: `contact_name = COALESCE($5, contact_name)` etc. The DTO/request uses `*string` and passes `nil` to mean "not supplied," but COALESCE also treats `nil` as "keep." There is no way to *clear* a contact email/name/phone once set — sending `null` is indistinguishable from omitting the field. This is a silent data-correctness gap for an editable master record.

- **PROC-02 (Medium) — `status:"inactive"` is accepted but never blocks PO creation, and `inactive` suppliers are not reactivatable cleanly.** `handleUpdateSupplier` accepts `active|inactive|deactivated` (`:334`). `CreatePurchaseOrder` only allows a PO when `status = 'active'` (`purchase_orders.go:174–181`), so `inactive` behaves like `deactivated` for ordering. But `DeactivateSupplier` only guards open POs for the `deactivated` path; a manual `PATCH {"status":"inactive"}` bypasses the `ErrSupplierInUse` open-PO guard entirely (`suppliers.go:176–190` is only reached via the DELETE route). A supplier with open POs can be set `inactive` directly, defeating the deactivation guard. The three-state model is under-specified.

- **PROC-03 (Low) — `GetSupplier` re-read before update uses the pool, not the tx.** `handleUpdateSupplier:339` reads `before` via `s.procurement.GetSupplier` (pool) *before* `tx.Begin` (`:348`). That is acceptable for the audit "before" snapshot, but note the read is unsynchronised with the update — concurrent edits race for the audit `PreviousValue`. The convention's "re-read via the SAME tx" applies to the *after*-update read, which `UpdateSupplier` does correctly via `RETURNING`. Informational/low.

- **PROC-04 (Low) — `ListSuppliers` N+1 on product IDs.** `suppliers.go:80` calls `supplierProductIDs` once per supplier inside the row loop, each a separate round-trip. For a tenant with many suppliers this is O(N) extra queries. A single `WHERE supplier_id = ANY(...)` fetch or a `LEFT JOIN LATERAL array_agg` would collapse it. Same N+1 pattern exists in `ListPurchaseOrders` (`purchase_orders.go:135`) and `OpenPurchaseOrdersForStation` (`overview.go:44`) for line fetches.

- **PROC-05 (Info) — No email/phone format validation.** `contact_email` is free text; no `@` check, no length cap. Acceptable for an internal master, flagged for completeness.

---

## Flow 2 — Purchase Order lifecycle

**Code:** `purchase_orders.go`; handlers `handleCreatePurchaseOrder`/`handleUpdatePurchaseOrder`/`handleTransitionPurchaseOrder` (`procurement_handlers.go:475–723`).

**State machine** (`ValidPurchaseOrderTransition`, `purchase_orders.go:274–287`):
```
draft     → submitted | cancelled
submitted → confirmed | cancelled
confirmed → cancelled
received  → closed
(default) → false
```
`partially_received` and `received` are reached only by the *receipt* path (`advancePurchaseOrderStatus`, `deliveries.go:284–310`), never by the manual transition route. The double-gate (handler checks `ValidPurchaseOrderTransition` at `:687`, repo re-checks `status = $3` in the UPDATE at `purchase_orders.go:256`) gives optimistic-concurrency safety: a racing transition that already moved the row yields `pgx.ErrNoRows` → `ErrInvalidTransition` → `409`. Good.

**Line math:** Lines store `ordered_litres numeric(14,3)` and `unit_price numeric(14,2)` (`0029:57–58`). Line *amount* (ordered × price) is **never computed or persisted on the PO** — there is no PO total. Totals only appear on invoices. This is a documented simplification but worth flagging (PROC-13).

**Defects:**

- **PROC-06 (High) — Litres carried as `float64` across the PO/receipt/invoice boundary, violating the house money/quantity discipline.** `PurchaseOrderLine.OrderedLitres float64`, `ReceivedLitres float64` (`purchase_orders.go:51–53`); `GoodsReceiptInput.VolumeLitres float64`; `SupplierInvoiceLineInput.InvoicedLitres float64`. The house rule is litres = `numeric(14,3)` carried as decimal **strings**, arithmetic in SQL. Here litres are decoded from JSON into `float64`, passed through Go, and re-bound to `numeric`. Concretely: `received_litres = received_litres + $4` (`deliveries.go:240`) binds a `float64`; `landed_cost_per_litre = (...) / $6::numeric` divides by the float-derived volume; the tolerance math `variance := newReceived - snap.OrderedLitres` (`deliveries.go:207`) is **float64 subtraction** on two values that originated as `numeric`. A 12,345.678 L order survives a round-trip, but 0.1+0.2-style representational error is reachable at the tolerance boundary, and the dip-variance comparison (`procurement_handlers.go:781`) is pure float. Quantities are not money, but the same precision argument applies; this is the single largest deviation from convention in the domain.

- **PROC-07 (High) — No over-receipt cap; `received_litres` can exceed `ordered_litres` without bound, and the PO can be received past 100%.** `deliveries.go:238–242` does an unconditional `received_litres = received_litres + $4`; the only validation is `volume_litres > 0` (`procurement_handlers.go:748`). `advancePurchaseOrderStatus` (`deliveries.go:284–301`) sets `received` when `bool_and(received >= ordered − tolerance)`, but once `received`, the receipt route is still reachable: `ReceiveGoodsReceipt` only rejects statuses other than `confirmed`/`partially_received` (`deliveries.go:178`). **However** — a fully-received PO has status `received`, which *is* rejected, so the practical cap is "until all lines cross the received threshold." A *single* line can still be massively over-received in one shot (e.g. order 10,000, receive 50,000) with no rejection — `match_status='over'` is recorded and a discrepancy flag returned, but the stock movement posts the full 50,000 L and the PO flips to `received`. There is no guard like `received + volume <= ordered * (1 + over_tolerance)`. Money/stock impact is real.

- **PROC-08 (Medium) — `expected_delivery_date` accepts arbitrary past dates; `received` POs cannot be re-opened or amended.** No validation that `expected_delivery_date >= today` (`handleCreatePurchaseOrder`). Minor. More notably, the only post-`received` transition is `→ closed`; there is no path to correct a mistakenly-received PO (e.g. wrong tank), and `UpdatePurchaseOrderDraft` refuses anything but `draft` (`purchase_orders.go:211`). Receipts are immutable with no reversal/credit flow.

- **PROC-09 (Medium) — `confirmed → cancelled` is allowed even after partial receipts have posted stock.** `ValidPurchaseOrderTransition` permits `confirmed → cancelled` (`:281`). But a PO only leaves `confirmed` for `partially_received`/`received` via the receipt path. So while `confirmed`, no stock has posted and cancel is safe. **But** there is **no** transition out of `partially_received` at all (default→false). A partially-received PO is stuck: it can never be cancelled or closed, only driven to `received` by more receipts. If the remaining litres never arrive, the PO is orphaned in `partially_received` forever and continues to appear in `OpenPurchaseOrdersForStation` (`overview.go:31`). This is a lifecycle dead-end.

- **PROC-10 (Low) — Create PO does not verify the product is in the supplier's coverage (`supplier_products`).** `CreatePurchaseOrder` checks the supplier is `active` but never that `in.Lines[*].ProductID` is among the supplier's associated products. A PO can be raised for a product the supplier doesn't carry. The `supplier_products` join table is purely advisory at order time.

- **PROC-11 (Low) — `RaisedBy` is trusted from `actor.UserID` (good), but `submitted_by`/`confirmed_by` are set by SQL CASE only — no separation-of-duties.** `TransitionPurchaseOrder` (`:243`) stamps the actor into whichever `*_by` column matches the target status. There is no check that approver ≠ raiser. For a finance control domain this is a notable governance gap (acceptable if intentional; flag).

---

## Flow 3 — Goods receipt / receiving

**Code:** `ReceiveGoodsReceipt` (`deliveries.go:147–273`); handler `handleReceivePurchaseOrderReceipt` (`procurement_handlers.go:738–859`).

This is the most intricate flow and is largely well-built: it locks `FOR UPDATE OF po, pol` (`deliveries.go:167`) — closing the two-receipts-racing window at the row level; it validates tank↔PO station+product match (`deliveries.go:194` → `ErrReceiptTankMismatch`); it computes landed cost in SQL (`ROUND((unit*vol)+freight+duty+levies, 2)` and per-litre `/4`, `deliveries.go:227–228`) keeping money in `numeric`; it advances PO status and posts exactly one stock movement guarded by `idx_stock_mvt_delivery_source_once` (`0030:66`).

**Defects:**

- **PROC-12 (Critical) — Division by `volume_litres` with no guard against zero at the SQL layer; relies entirely on a handler check that the inner repo does not enforce.** `deliveries.go:228`: `landed_cost_per_litre = ROUND((...) / $6::numeric, 4)`. `$6` is `volume_litres`. The handler rejects `volume_litres <= 0` (`procurement_handlers.go:748`), but `ReceiveGoodsReceipt` is a public repo method with **no** internal `VolumeLitres > 0` guard. Any future caller (or a code path that bypasses the handler validation) triggers a Postgres `division_by_zero` (22012) inside the tx, surfacing as a generic `500`. Defence-in-depth is missing precisely where the convention says arithmetic lives in SQL. Marked Critical because it is an unguarded divide-by-zero on the money path.

- **PROC-13 (High) — Tolerance for short/over uses the *product loss tolerance* as a receiving tolerance, conflating two different business concepts.** `deliveries.go:208`: `toleranceLitres := snap.OrderedLitres * snap.LossTolerancePct / 100`. `LossTolerancePercent` is the *reconciliation* shrinkage allowance for a product (used in `reconciliation_handlers.go:184`), not a *delivery* tolerance. Reusing it means: a product with `loss_tolerance_percent = 0` flags *every* receipt that isn't exact to the litre as `short`/`over` (and the discrepancy threshold in `advancePurchaseOrderStatus` shifts the "fully received" bar by the same wrong number). And `advancePurchaseOrderStatus` treats `received >= ordered − (ordered * lossTol/100)` as "fully received" (`deliveries.go:288`) — so for a 2% loss-tolerance product, delivering only 98% of the order auto-completes the PO and the remaining 2% is silently written off as "received." That is a real financial leak: the supplier can under-deliver up to the loss tolerance and the PO closes as fully satisfied.

- **PROC-14 (Medium) — `dip_variance` / `dip_mismatch` computed in float64 in the handler and only returned, never persisted as the mismatch flag.** `procurement_handlers.go:771–782`: `variance := req.VolumeLitres - (*req.DipAfterLitres - *req.DipBeforeLitres)` then `dipMismatch = abs(variance) > req.VolumeLitres*prod.LossTolerancePercent/100`, all float64. `dip_variance_litres` is stored (`deliveries.go:232`), but `dip_mismatch` is only echoed in the JSON response (`:854`) — it is **not** persisted, not audited, and raises **no** discrepancy. A tanker whose dip change contradicts its declared volume produces a transient boolean the client may ignore; there is no durable flag for investigation. Contrast with the quantity discrepancy which *is* persisted via the invoice path.

- **PROC-15 (Medium) — Three audit/outbox writes for one receipt, with duplicated payloads and a conditional third.** `procurement_handlers.go:815–846` writes `GoodsReceiptRecorded` and `GoodsReceiptPriced` back-to-back with **identical** `NewValue: toDeliveryDTO(res.Delivery)` (`:819`, `:829`), then conditionally `PurchaseOrderReceived`. Two events for one atomic action with the same payload is noise and an event-consumer hazard (idempotency keys differ but content is identical). The `PurchaseOrderReceived` payload is a hand-built `map` (`:840`) rather than the PO DTO, inconsistent with every other PO event. Minor but smells like leftover scaffolding.

- **PROC-16 (Medium) — `dip_before`/`dip_after` are independent optionals; only-one-present is silently accepted and yields no variance.** `procurement_handlers.go:752` validates non-negativity but not that the pair is supplied together. If only `dip_after` is sent, `dipVariance` stays nil and no cross-check runs — a partial dip is accepted with no warning. Should require both-or-neither.

- **PROC-17 (Low) — Receipt against a `draft`/`submitted` PO returns a misleading 409 message.** `ErrPurchaseOrderNotReceivable` → "purchase order is not confirmed or partially received" (`procurement_handlers.go:802`). Correct, but the handler resolves the PO via `purchaseOrderForStationPermission` using `delivery.receive` permission (`:739`); a user with `delivery.receive` but no procurement role can drive PO status changes through receiving. The permission boundary between "receive stock" and "advance a purchase order" is blurred — receiving silently mutates `purchase_orders.status`.

---

## Flow 4 — Supplier invoices & 3-way match

**Code:** `RecordSupplierInvoice`/`ApproveSupplierInvoice`/`raiseInvoiceDiscrepancies`/`refreshInvoiceMatchStatus` (`invoices.go:123–385`); handlers `handleRecordSupplierInvoice`/`handleApproveSupplierInvoice` (`procurement_handlers.go:878–1057`).

**Match design.** Line amount: `COALESCE($8::numeric, ROUND($6 * $7, 2))` (`invoices.go:159–160`) — invoiced_litres × unit_price, money in SQL, optional client-supplied override. Total recomputed by `SUM(amount)` (`invoices.go:175`). Two discrepancy checks: **quantity** (invoiced vs SUM of received deliveries for the same PO+line, threshold `> greatest(received*0.005, 1)`, `invoices.go:342`) and **price** (invoice unit vs PO unit, threshold `> greatest(po_price*0.005, 0.01)`, `invoices.go:361`). Open discrepancies set status `discrepancy`; zero → `matched`. Approval refuses unless `matched` and zero open discrepancies (`invoices.go:224–252`). On approve, a `PayableCreated` outbox event is written (`procurement_handlers.go:1043`). The whole record/match and the approve/payable are each one tx. This is a genuine three-way match and is the strongest part of the domain.

**Defects:**

- **PROC-18 (High) — Approval emits a `PayableCreated` event but creates no payable; there is no payables table or consumer in scope, so the 3-way match terminates in a fire-and-forget event.** `procurement_handlers.go:1036–1051`. The audit brief asks "does it create a payable? correctly?" — the answer is **no**: it only writes an outbox event with `Type:"PayableCreated"`. No `accounts_payable` row, no FK, no idempotency beyond the outbox. If no downstream projector consumes it (none found in scope), the approved invoice's financial obligation exists only as an event. `SupplierBalancesForStation` (`overview.go:54`) computes "outstanding" as `SUM(total_amount) WHERE status='approved'` — i.e. it sums *all* approved invoices forever, with **no** subtraction of payments. The "outstanding amount" label is wrong: it is lifetime approved spend, not an open payable balance.

- **PROC-19 (High) — Quantity-discrepancy tolerance pivots on `received_litres`, so a zero-receipt invoice and an over-billing produce asymmetric / wrong thresholds.** `invoices.go:342`: `abs(invoiced − received) > greatest(received * 0.005, 1)`. If no delivery exists yet (`received = 0`), threshold = `greatest(0, 1) = 1` L, so an invoice for 10,000 L against 0 received raises a discrepancy (correct). But the percentage band scales with *received*, not *invoiced* or *ordered* — over-billing 10,000 against 9,800 received gives `abs(200) > 49` → flagged (good), yet the band is computed on the smaller "received" base, making the effective tolerance direction-dependent. More importantly, the discrepancy `variance_litres = invoiced − received` is summed only over `deliveries` joined on PO+line (`invoices.go:327–329`); it ignores `match_status='over'` short-shipped corrections. The match is per-line vs all-receipts-ever, with no period scoping — a second invoice for the same line double-counts received litres differently than the first. Edge-case correctness is fragile.

- **PROC-20 (Medium) — Invoice can be recorded against a PO in `received`/`closed`/`cancelled`-excluded but `confirmed` states, decoupling invoice from actual receipt.** `RecordSupplierInvoice` rejects only `draft` and `cancelled` (`invoices.go:137`). So an invoice can be recorded against a `confirmed` PO **before any goods arrive** — `received = 0` then raises a quantity discrepancy (the test relies on the opposite ordering). Functionally tolerable, but the comment in `0031.up.sql:5` says "matched against PO lines and goods receipts," implying receipts should exist. No guard ties invoicing to having received anything.

- **PROC-21 (Medium) — `delivery_id` on an invoice line is accepted and FK-checked but never validated to belong to the same PO line.** `invoices.go:155–164` inserts `delivery_id` from the request straight through; the FK (`supplier_invoice_lines_delivery_fk`, `0031:72`) only checks tenant+existence, not that the delivery's `po_line_id` matches `ln.POLineID`. A caller can attach an unrelated delivery to an invoice line. The discrepancy query then ignores `il.delivery_id` entirely (it re-aggregates by PO+line, `invoices.go:329`), so the field is decorative and mis-attachable.

- **PROC-22 (Medium) — Price-discrepancy `variance_amount = invoice_unit − po_unit` stores a *per-litre price delta* in a column whose name implies a total money amount.** `invoices.go:354`. `variance_amount numeric(14,2)` (`0031:94`) is populated with a unit-price difference, while the quantity discrepancy leaves it null and populates `variance_litres`. Semantically muddled: a finance user reading `variance_amount` cannot tell if it's a total or a rate. The price variance should arguably be `(invoice_unit − po_unit) * invoiced_litres` to express the money at stake.

- **PROC-23 (Low) — Invoice number uniqueness is per (tenant, supplier, lower(number)), but the handler maps any `23505` to "invoice number already exists" (`procurement_handlers.go:943`).** Correct here since it's the only unique constraint on the table, but brittle if a future constraint is added.

---

## Flow 5 — Procurement discrepancies

**Code:** `GetDiscrepancy`/`ResolveDiscrepancy` (`invoices.go:269–307`); handler `handleResolveProcurementDiscrepancy` (`procurement_handlers.go:1063–1130`).

Raised automatically during invoice recording (`raiseInvoiceDiscrepancies`); resolved manually via `PATCH .../status {"status":"resolved"}`. Resolution flips `open → resolved`, stamps `resolved_by`/`resolved_at`, and re-runs `refreshInvoiceMatchStatus` so the invoice flips `discrepancy → matched` once the last open one closes (`invoices.go:303`). The handler authorizes via `invoice.approve` on the invoice's station (`procurement_handlers.go:1098`) — correct scoping. Optimistic concurrency on `status = 'open'` → `ErrAlreadyResolved` → 409. Solid.

**Defects:**

- **PROC-24 (Medium) — Discrepancies are resolved with no reason, evidence, or amount adjustment — "resolved" is a pure status flip that unblocks payment.** `ResolveDiscrepancy` (`invoices.go:285`) records who/when but no note, no corrected value. A blocking price/quantity mismatch is cleared by anyone with `invoice.approve` simply asserting "resolved," with no audit of *why* — then the invoice becomes payable at the (possibly wrong) invoiced amount. For a control whose entire purpose is to stop overpayment, the resolution path has no teeth. (The audit row records the actor, but not a justification.)

- **PROC-25 (Low) — Resolving a discrepancy does not re-validate that the underlying mismatch still holds.** Resolution never re-checks the numbers; if more stock arrives after the discrepancy was raised, the invoice could now genuinely match, but the human must still manually resolve, and conversely a stale "resolved" sticks even if a later receipt is reversed. No re-derivation.

- **PROC-26 (Low) — `severity` is always `'blocking'`; the `'warning'` value and severity column are dead.** `raiseInvoiceDiscrepancies` hard-codes `'blocking'` for both checks (`invoices.go:338`, `:352`). The schema allows `warning` (`0031:101`) and the DTO exposes it, but nothing ever produces it. Dead capability.

---

## Per-file notes

**`repo.go`** — `scanTimePtr` (`:31`) is dead (identity function, no callers found in scope). `uuidSliceFromRows` (`:33`) is also unused — suppliers/PO line ID fetches use ad-hoc loops instead. Two dead helpers. *(PROC-27, Info.)*

**`overview.go`** — `PriceTrendForStation` JOINs `deliveries → suppliers → tanks → products` (`:82–92`) with `LIMIT`, indexed by `idx_deliveries_supplier_id` (partial). Reasonable. `SupplierBalancesForStation` mislabels lifetime spend as outstanding (see PROC-18). `OpenPurchaseOrdersForStation` has the same line N+1 as PROC-04. The handler caps `RecentReceipts` to 10 *in Go after fetching all* (`procurement_handlers.go:1208–1213`) — it calls `ListDeliveriesForStation` which returns the station's **entire** delivery history then slices; should `LIMIT` in SQL. *(PROC-28, Low — unbounded fetch.)*

**`procurement_handlers.go`** — The mutating PO/invoice/discrepancy routes are **not** wrapped in router-level `requirePermission`; they rely on in-handler `authorizeStation` after loading the row (`server.go:339–347`). This is the *correct* pattern (station id is on the body/row, per `policy_middleware.go:102` comment) and every mutating handler does call `authorizeStation` with the right permission (`purchase_order.manage`, `purchase_order.approve`, `delivery.receive`, `invoice.manage`, `invoice.approve`). Verified no missing authZ on any mutating procurement route. Tenant isolation: every repo query filters `tenant_id = $1` and RLS policies exist on all four tables — no IDOR found. `handleCreatePurchaseOrder` validates the station exists *and* `authorizeStation` *before* opening the tx (`:501–510`) — good ordering. `decimalPattern` (`:23`) allows up to 4 decimal places for all money inputs including `numeric(14,2)` amounts — a `2500.1234` unit price passes Go validation then truncates/rounds in Postgres silently (PROC-29, Low). `validDecimal` rejects negatives by regex `^\d+...` (no leading `-`), so negative prices are blocked at the edge — good.

**`deliveries.go` (procurement parts)** — `defaultDecimal` is **duplicated** here (`:275`) and in `procurement_handlers.go:1318`; the inventory copy doesn't `TrimSpace`. Minor drift (PROC-30, Info). The `FOR UPDATE OF po, pol` lock (`:167`) correctly serialises concurrent receipts on the *same* PO line; two receipts on *different* lines of one PO can still race `advancePurchaseOrderStatus` (`:284`) — but that function reads all lines and writes one status, and both runs are inside row-locked txs on overlapping data, so the second blocks. Acceptable.

---

## Test coverage assessment — `phase5_integration_test.go`

A single happy-path test (`TestPhase5_ProcurementFlow`, 137 lines) drives supplier → PO → submit → confirm → receive(partial) → invoice(discrepancy) → resolve → approve → receive(balance). It is a *good* end-to-end smoke test and asserts real numbers (landed cost `2515.3061`, partial status, payable event count). **Gaps:**

- No negative-path coverage for: illegal PO transitions (e.g. `draft → confirmed`), over-receipt (PROC-07), divide-by-zero volume (PROC-12), receipt against wrong tank (`ErrReceiptTankMismatch`), receipt against `received` PO, cross-tenant IDOR, the `partially_received` dead-end (PROC-09), `inactive`-status deactivation bypass (PROC-02).
- No concurrency test for two racing receipts (the headline correctness concern).
- No price-discrepancy test (only quantity).
- No assertion that the second `GoodsReceiptPriced` event is distinct/needed (PROC-15).
- Asserts only one `PayableCreated` event but never that a payable *exists* (because none does — PROC-18).

Coverage is **shallow but honest**: it proves the golden path, not the guards. For a finance-control domain the absence of negative and concurrency tests is the biggest quality gap.

---

## Documented / scope simplifications (confirmed gaps)

- **Central procurement does not auto-create POs.** `enterprise_commercial_handlers.go:149–239` implements `central_procurement_plan` create/list/release as a target-litres planning artefact only; `ReleasePlan` flips plan state and counts lines (`:229`) but **never** inserts `purchase_orders`. The hand-off from a released central plan to station POs is entirely manual. Confirmed gap (PROC-31, Info — matches the prior project note).
- **No PO header total / no payable ledger** (PROC-13, PROC-18).
- **No receipt reversal / credit note** (PROC-08).
- **`warning`-severity discrepancies never produced** (PROC-26).

---

## Findings table

| ID | Severity | File:Line | Issue | Fix |
|----|----------|-----------|-------|-----|
| PROC-12 | Critical | `internal/inventory/deliveries.go:228` | `landed_cost_per_litre / $6` divides by `volume_litres` with no guard in the repo method (only the handler checks `>0`) | Add `if in.VolumeLitres <= 0 { return ErrInvalid }` at top of `ReceiveGoodsReceipt`; or `NULLIF($6,0)` in SQL |
| PROC-06 | High | `purchase_orders.go:51-53`, `deliveries.go:207,240` | Litres carried as `float64` and arithmetic done in Go, violating numeric(14,3)/decimal-string discipline | Carry litres as decimal strings; do all add/compare in SQL |
| PROC-07 | High | `deliveries.go:238-242` | No over-receipt cap; a single line can be received far beyond `ordered_litres` and post full stock | Enforce `received + volume <= ordered * (1+over_tol)` or reject `over` beyond tolerance |
| PROC-13 | High | `deliveries.go:208,288` | Product *loss tolerance* reused as receiving tolerance → under-delivery up to loss% auto-completes the PO (financial leak) | Introduce a distinct delivery/receipt tolerance; do not auto-complete on shortfall |
| PROC-18 | High | `procurement_handlers.go:1036-1051`, `overview.go:59` | "Approval" emits `PayableCreated` event but creates no payable; "outstanding" sums lifetime approved spend, never nets payments | Add payables table + consumer; net payments in balance query |
| PROC-19 | High | `invoices.go:342` | Quantity-discrepancy tolerance pivots on `received_litres`; multi-invoice re-aggregation double-counts; direction-dependent band | Base tolerance on ordered/invoiced; scope receipts per-invoice |
| PROC-01 | Medium | `suppliers.go:142-149` | `COALESCE` updates cannot null-out contact fields (null == omit) | Use `CASE WHEN $n_set THEN $n ELSE col END` per nullable field |
| PROC-02 | Medium | `procurement_handlers.go:334`, `suppliers.go:176` | `PATCH status:"inactive"` bypasses the open-PO deactivation guard | Route all deactivations through the open-PO check; clarify 3-state model |
| PROC-08 | Medium | `purchase_orders.go:211,274` | No receipt reversal; `received` POs cannot be corrected; past `expected_delivery_date` accepted | Add reversal/credit flow; validate delivery date |
| PROC-09 | Medium | `purchase_orders.go:274-287` | `partially_received` has no exit transition → POs orphaned forever | Allow `partially_received → cancelled/closed` |
| PROC-14 | Medium | `procurement_handlers.go:771-782` | `dip_mismatch` computed in float, returned only, never persisted/audited/raised | Persist mismatch + raise a discrepancy; compute in SQL |
| PROC-15 | Medium | `procurement_handlers.go:815-846` | Two identical-payload events per receipt; PO event uses ad-hoc map | Emit one receipt event; use PO DTO |
| PROC-16 | Medium | `procurement_handlers.go:752` | Only-one-dip-present silently accepted, no cross-check | Require both-or-neither dip readings |
| PROC-20 | Medium | `invoices.go:137` | Invoice recordable against `confirmed` PO before any goods received | Optionally require ≥1 receipt before invoicing |
| PROC-21 | Medium | `invoices.go:155-164`, `0031:72` | `delivery_id` not validated to belong to the line; ignored by match | Validate delivery.po_line_id == line; or drop the field |
| PROC-22 | Medium | `invoices.go:354`, `0031:94` | `variance_amount` stores a per-litre delta, name implies total | Store `(inv_unit−po_unit)*litres` or rename |
| PROC-24 | Medium | `invoices.go:285` | Discrepancy "resolved" is a status flip with no reason/correction; unblocks payment | Require resolution note + corrected amount |
| PROC-03 | Low | `procurement_handlers.go:339` | Audit `before` read on pool, racy vs concurrent edit | Read within tx or accept as snapshot |
| PROC-04 | Low | `suppliers.go:80`, `purchase_orders.go:135`, `overview.go:44` | N+1 child fetches per parent row | Batch with `= ANY(...)` / lateral aggregate |
| PROC-05 | Low | `procurement_handlers.go:239` | No email/phone format validation | Add basic format checks |
| PROC-10 | Low | `purchase_orders.go:170` | PO lines not checked against supplier_products coverage | Validate product ∈ supplier coverage |
| PROC-11 | Low | `purchase_orders.go:243` | No separation of duties (approver may equal raiser) | Enforce raiser ≠ approver if required |
| PROC-17 | Low | `procurement_handlers.go:739` | Receiving (`delivery.receive`) silently advances `purchase_orders.status` | Document/gate PO mutation behind a procurement perm |
| PROC-23 | Low | `procurement_handlers.go:943` | All `23505` mapped to "invoice number exists" | Inspect constraint name before messaging |
| PROC-25 | Low | `invoices.go:303` | Resolution doesn't re-validate the underlying mismatch | Re-derive on resolve |
| PROC-26 | Low | `invoices.go:338,352` | `warning` severity never produced (dead) | Wire warning-tier thresholds or drop |
| PROC-28 | Low | `overview.go`, `procurement_handlers.go:1208` | `ListDeliveriesForStation` fetches full history, sliced to 10 in Go | Add SQL `LIMIT` |
| PROC-29 | Low | `procurement_handlers.go:23` | `decimalPattern` allows 4 dp for `numeric(14,2)` fields → silent rounding | Validate scale per field type |
| PROC-27 | Info | `repo.go:31,33` | `scanTimePtr`, `uuidSliceFromRows` dead | Remove |
| PROC-30 | Info | `deliveries.go:275`, `procurement_handlers.go:1318` | `defaultDecimal` duplicated, divergent (trim vs no-trim) | Consolidate to one helper |
| PROC-31 | Info | `enterprise_commercial_handlers.go:212` | Central procurement plan release does not create POs (manual hand-off) | Documented simplification; note for roadmap |

---

## Severity counts

- **Critical:** 1 (PROC-12)
- **High:** 5 (PROC-06, PROC-07, PROC-13, PROC-18, PROC-19)
- **Medium:** 11 (PROC-01, -02, -08, -09, -14, -15, -16, -20, -21, -22, -24)
- **Low:** 11 (PROC-03, -04, -05, -10, -11, -17, -23, -25, -26, -28, -29)
- **Info:** 3 (PROC-27, -30, -31)
- **Total: 31**

## Top-5 risks

1. **PROC-12 (Critical)** — Unguarded divide-by-zero on `volume_litres` inside `ReceiveGoodsReceipt` (`deliveries.go:228`); the only guard is in the handler, leaving the money-math repo method fragile to any other caller.
2. **PROC-13 (High)** — Product *loss tolerance* doubles as a *receiving* tolerance (`deliveries.go:208,288`): a supplier may under-deliver up to the loss% and the PO auto-completes as fully received — a quiet financial leak.
3. **PROC-18 (High)** — The 3-way match "approval" creates **no payable**, only a fire-and-forget `PayableCreated` event (`procurement_handlers.go:1043`); `SupplierBalancesForStation` reports lifetime approved spend as "outstanding," never netting payments.
4. **PROC-07 (High)** — No over-receipt cap (`deliveries.go:238`): a single receipt can post stock far beyond the ordered quantity with only an advisory `over` flag.
5. **PROC-06 (High)** — Litres flow through the PO→receipt→invoice path as `float64` with Go-side arithmetic (`purchase_orders.go:51`, `deliveries.go:207`), violating the numeric/decimal-string discipline at the tolerance boundary where precision matters most.
