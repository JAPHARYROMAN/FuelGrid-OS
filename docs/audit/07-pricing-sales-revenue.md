# Audit 07 — Pricing, Sales Recognition & Revenue (Phase 6)

**Domain:** Selling-price book, recognized priced sales, COGS/inventory valuation, daily revenue close.
**Posture:** Read-only, atomic-level, money-correctness-first.
**Verdict:** The money pipeline is *mostly* SQL-numeric and disciplined, and the idempotency backstops are real. But there is **one Critical COGS-correctness defect** (reversed deliveries permanently pollute the moving-average cost), a **High untested tax-inclusive split** that ships with an 18% production tax rate, and several **authZ-scoping / data-integrity** gaps around the revenue close. Margin and revenue figures are trustworthy *only* for tenants that never reverse a delivery and never charge tax — neither of which is the production default.

## Scope & files

| File | LOC | Role |
|------|-----|------|
| `internal/pricing/repo.go` | 173 | Price-book data layer: SetPrice, ResolvePrice, PriceBoard, History |
| `internal/revenue/repo.go` | 214 | Sales recognition, DaySummary, InventoryValuation |
| `internal/revenue/days.go` | 203 | revenue_days: ComputeDay, LockDay, tenders, recent trend |
| `internal/inventory/costing.go` | 50 | MovingAverageCost, AverageLandedCostForStationProduct (below-cost basis) |
| `internal/inventory/sales.go` | 82 | PostSalesForShift (stock-out ledger posting) — cross-ref |
| `services/api/internal/server/pricing_handlers.go` | 239 | SetPrice / PriceBoard / History handlers + `parseDecimal` |
| `services/api/internal/server/revenue_handlers.go` | 180 | recognizeShiftRevenue, ListShiftSales, ListStationSales, InventoryValuation |
| `services/api/internal/server/revenue_close_handlers.go` | 253 | ComputeRevenueDay, LockRevenueDay, RevenueOverview, ARaging |
| `services/api/internal/server/shift_exceptions_handlers.go` (approval call site) | — | postShiftSales / recognizeShiftRevenue invocation |
| Migrations | — | `0024_stock_movements`, `0026_sales_idempotency`, `0030_goods_receipts`, `0032_price_changes`, `0033_sales`, `0036_revenue_days`, `0009_products`, `0014_operating_days`, `0004_rbac` |
| `services/api/internal/server/phase6_integration_test.go` | 124 | End-to-end revenue chain test |

Routing: `services/api/internal/server/server.go:369-385` (pricing + sales), `:615-623` (revenue close).

---

## Flow 1 — Setting a price & the below-cost guard

`handleSetPrice` (`pricing_handlers.go:60`) → `pricing.SetPrice` (`pricing/repo.go:80`).

The selling-price book is append-only and effective-dated (`price_changes`, `0032`): the active price for `(station, product)` is the latest row with `effective_from <= now()`, resolved via `idx_price_changes_resolve`. A future `effective_from` auto-schedules — clean, cron-free, and the audit event correctly flips to `price.scheduled`/`PriceScheduled` when `effFrom.After(time.Now())` (`pricing_handlers.go:145`). `unit_price` is `numeric(14,4)` — correct rate precision. The INSERT snapshots `previous_price` via a correlated subquery (`repo.go:86-88`), and the whole change is wrapped in one tx with `audit.WriteWithOutbox` and committed only after — textbook atomicity.

AuthZ is router-level: `requirePermission("price.change", stationFromURLParam("stationID"))` (`server.go:371`). `price.change` is `station_scoped=true` (`0004:130`), so station scoping is enforced. Reads ride `pricing.read` (`server.go:373`). **Good.**

**The below-cost guard is the money-correctness weak point.** At `pricing_handlers.go:107-119`:

```go
cost, found, err := s.inventory.AverageLandedCostForStationProduct(ctx, actor.TenantID, stationID, req.ProductID)
...
if c, ok := parseDecimal(cost); ok && price < c {
```

Three problems:

1. **The guard compares floats, not decimals.** `parseDecimal` (`pricing_handlers.go:230`) is `strconv.ParseFloat`. Both the proposed price and the landed cost are round-tripped through `float64` before the `price < c` comparison. For a guard this is *tolerable* (it's a soft check, the stored value stays exact), but a price exactly equal to cost-minus-epsilon can be mis-classified at the boundary. This is the *only* money value compared in Go in the whole domain — flagged for completeness (REV-09).

2. **"Cost" is a station-wide product average that ignores which tank fills the nozzle.** `AverageLandedCostForStationProduct` (`costing.go:35`) averages landed cost across *all tanks at the station holding the product*. But recognition (Flow 2) costs each sale at the *specific tank's* moving average. So the guard can pass a price that is above the *blended station* cost yet below the *specific tank's* cost — a sale recognized below cost despite the guard. Inconsistent cost bases between guard and recognition (REV-05).

3. **The guard inherits the reversed-delivery pollution bug (REV-01, below).** Its cost basis is the same flawed query, so a reversed expensive delivery inflates the floor and a reversed cheap one deflates it.

---

## Flow 2 — Sales recognition on shift approval (the money core)

`handleApproveShift` (`shift_exceptions_handlers.go:48`) runs, in one tx: `ApproveShift` → `postShiftSales` (stock-out ledger) → `recognizeShiftRevenue` → `audit.WriteWithOutbox` → commit. `recognizeShiftRevenue` (`revenue_handlers.go:52`) delegates to `revenue.RecognizeShiftSales` (`revenue/repo.go:88`).

### What's right
- **All money arithmetic is SQL `numeric`.** Gross, tax, net, COGS, and margin are computed inside the single `INSERT ... SELECT` (`repo.go:113-131`). Litres are `numeric(14,3)`, price `numeric(14,4)`, money `numeric(14,2)` per the schema (`0033:20-28`). No Go-side money math. The `Litres float64` field on `Sale` (`repo.go:29`) is transport-only — it never participates in arithmetic.
- **Idempotency is real.** `ON CONFLICT (shift_id, nozzle_id) DO NOTHING` against `uq_sales_shift_nozzle` (`0033:50`) makes re-approval a no-op per nozzle. The parallel stock-out posting (`PostSalesForShift`, `sales.go:45`) is independently idempotent via `SalesPostedForShift` plus the partial unique index `uq_stock_mvt_sales_per_shift_tank` (`0026`). A shift cannot be double-posted or double-recognized.
- **Price/tax/cost are snapshotted** into the sale row, so a later price or cost change never rewrites recognized revenue — exactly as the schema comment promises.
- Products with no resolvable price are skipped (`WHERE ... price.unit_price IS NOT NULL`, `repo.go:111`) rather than recognized at zero — correct.

### Defects

**REV-01 (Critical) — Reversed/superseded deliveries permanently pollute moving-average cost.** The cost LATERAL (`repo.go:104-109`, mirrored in `InventoryValuation` `repo.go:191-196`, `MovingAverageCost` `costing.go:18-22`, and `AverageLandedCostForStationProduct` `costing.go:37-43`) is:

```sql
SELECT (SUM(sm.litres * sm.landed_cost_per_litre) / NULLIF(SUM(sm.litres), 0))
FROM stock_movements sm
WHERE ... sm.movement_type = 'delivery'
  AND sm.landed_cost_per_litre IS NOT NULL AND sm.litres > 0
```

The reversal model (`0024:9-14`, `inventory/repo.go:221-251`) reverses a delivery by marking the original `status='reversed'` and posting a **contra** with the *same* `movement_type='delivery'`, `source_ref_type='correction'`, `litres = -orig.Litres` (negative), and **`landed_cost_per_litre` left NULL** (the contra `PostInput` never sets it, `inventory/repo.go:242-250`).

Consequences for the cost basis:
- The original reversed delivery has **positive litres and a non-null cost**, and the query has **no `status='posted'` or `supersedes_id IS NULL` filter** → it is *still counted*.
- The contra is excluded twice over: `sm.litres > 0` drops it (negative) and `landed_cost_per_litre IS NOT NULL` drops it (NULL).

So reversing a delivery does **not** remove it from the weighted average — the erroneous delivery's litres and cost remain in the numerator and denominator forever. A duplicate/wrong-cost delivery that is correctly reversed at the ledger level (book balance nets to zero) still corrupts every subsequent COGS, margin, stock-value, and below-cost-guard computation for that tank/product. This is the single most damaging money defect in the domain because it is **silent**: the inventory book balance looks correct, but valuation and margin are wrong. **Fix:** add `AND sm.status = 'posted' AND sm.supersedes_id IS NULL` to every delivery-cost aggregation (the four sites above), so reversed originals are excluded.

**REV-02 (High) — Cumulative average, not a moving average; consumed stock never lowers the cost basis.** The doc comments and function names say "moving-average landed cost," but the query averages **all delivery movements over all time**, never decremented as litres are sold. After a tank receives 10,000 L @ 2,400 then sells 9,000 L and receives 10,000 L @ 3,000, the "moving average" is `(10000*2400 + 10000*3000)/20000 = 2,700`, when a true moving-average (perpetual weighted average over *on-hand* stock) would weight the remaining 1,000 L @ 2,400 against the new 10,000 @ 3,000 ≈ 2,945. This is a **cumulative/lifetime average**, which is a defensible accounting policy *only if chosen deliberately* — but it is mislabeled, undocumented as a policy choice, and diverges materially from "moving average" as the price rises/falls over a tank's life. At minimum the naming is misleading (REV-02); if the intent really is perpetual weighted-average, the algorithm is wrong.

**REV-03 (High) — Tax-inclusive split is untested and ships with an 18% production rate.** The net/tax split (`repo.go:121-122`):

```sql
ROUND(ROUND(litres * unit_price, 2) * tax_rate / (100 + tax_rate), 2) AS tax,
ROUND(litres*unit_price,2) - ROUND(ROUND(litres*unit_price,2)*tax_rate/(100+tax_rate),2) AS net
```

treats the selling price as tax-inclusive. The formula itself is arithmetically sound (gross−tax=net, single round per component, no accumulation across rows since each sale is one row). **But:** the production seed sets `tax_rate=18.00` for all fuel products (`cmd/seed/main.go:165-167`), while the integration-test harness creates products with *no* `tax_rate` → defaults to `0` (`0009:21`, harness `phase2_integration_test.go:184`). The Phase-6 test therefore asserts `gross == net` and `tax == 0` (`phase6_integration_test.go:64-66`) and **never exercises a non-zero tax rate at all**. The most-used production code path (18% VAT-inclusive split) has zero test coverage. Given that this number flows into `tax_total`/`net_revenue` on the revenue day and ultimately to finance, this is a High-severity coverage gap, not merely a nicety.

**REV-04 (Medium) — Price is resolved at *approval* time, not at close/sale time.** `RecognizeShiftSales` resolves `price.unit_price` with `effective_from <= now()` (`repo.go:101`) where `now()` is the moment of approval. `shift_close_lines.unit_price` already froze a price at *close* (`internal/operations/close.go:60`, sourced from `nozzle.default_price` per Phase-3 notes). If a price change lands between close and approval, the recognized gross diverges from the operator-visible expected cash computed at close — two different "truths" for the same litres. Recognition arguably should reuse the close-line snapshot (`cl.unit_price`) or resolve as-of the shift's business date, not `now()`. At minimum this should be a deliberate, documented choice; today it is implicit.

**REV-05 (Medium) — Guard cost basis ≠ recognition cost basis.** See Flow 1 item 2: the below-cost guard uses a *station-wide* product average (`AverageLandedCostForStationProduct`), recognition uses the *per-tank* average (`cost` LATERAL keyed on `n.tank_id`). The guard can approve a price the recognition layer then books at negative margin.

**REV-13 (Low) — Approval status check is not atomic with the UPDATE (TOCTOU).** `handleApproveShift` reads `before.Status` outside the tx (`shift_exceptions_handlers.go:68`), then `ApproveShift` UPDATEs with `WHERE id=$1 AND tenant_id=$2` and **no `status='closed'` guard** (`internal/operations/shifts.go:158-159`). Two concurrent approvals can both pass the pre-check. The damage is bounded — recognition and stock-posting are idempotent at the DB — but the shift's `approved_by`/`approved_at` can be overwritten by the loser, and the audit log records two approvals. Add `AND status = 'closed'` to the UPDATE.

---

## Flow 3 — COGS, margin & inventory valuation

`Sale.cogs = ROUND(litres * avg_cost, 2)` and `margin = net − cogs` (`repo.go:124-128`), both guarded by `avg_cost IS NOT NULL` so an un-costed tank yields NULL COGS/margin rather than zero — correct (a NULL margin signals "unknown," not "zero profit"). `DaySummary` (`repo.go:166`) and `ComputeDay` (`days.go:61`) roll these up with `COALESCE(SUM(...),0)` — also correct.

`InventoryValuation` (`repo.go:182`) values each tank as `book_litres * avg_cost`, where `book_litres = SUM(sm.litres)` over **all** movements (contras included, so reversals net out for the *quantity*). The quantity is right; the **per-litre cost is the polluted figure from REV-01**, so `stock_value` is wrong for any tank that ever had a reversed delivery.

**REV-06 (Medium) — Margin is computed against *net* revenue but reported alongside *gross*.** `margin = net − cogs` (`repo.go:126-127`). That is correct accounting (gross profit excludes the tax you remit). But the revenue-day rollup and overview present `gross_revenue`, `cogs_total`, and `margin_total` side by side (`revenue_close_handlers.go:198-203`, `days.go:69-71`) with no derived margin **percentage** anywhere, and `margin_total ≠ gross_revenue − cogs_total` whenever tax > 0. A consumer naively computing `margin% = margin/gross` will be wrong by the tax fraction. No gross-margin-% field exists; the relationship is implicit and a likely source of downstream reporting error (especially combined with REV-03's 18% rate). Document the basis and/or expose net alongside.

**REV-07 (Low) — No COGS for skipped/un-onboarded tanks, but sales still recognized.** `postShiftSales` skips tanks with no opening balance (`sales.go:64-66`) — the ledger draw-down is deferred. But `RecognizeShiftSales` runs regardless and will recognize the sale with COGS based on whatever delivery movements exist (possibly none → NULL COGS). So a shift can have recognized *revenue* with no corresponding *inventory draw-down* yet, and the two are reconciled only via the manual backfill described in `sales.go:38-44`. The revenue and inventory ledgers can be transiently inconsistent. Low because it is documented and self-heals on backfill, but it is a real divergence window.

---

## Flow 4 — Daily revenue close & lock

`handleComputeRevenueDay` (`revenue_close_handlers.go:56`) → `ComputeDay` (`days.go:61`); `handleLockRevenueDay` (`:110`) → `LockDay` (`days.go:185`).

### What's right
- **Lock immutability is enforced in SQL, not Go.** `ComputeDay`'s upsert has `... DO UPDATE SET ... WHERE revenue_days.status <> 'locked'` (`days.go:94`); when the row is locked the `DO UPDATE` matches nothing, `RETURNING` yields no row, and the code maps `pgx.ErrNoRows → ErrLocked → 409` (`days.go:98-99`, handler `:81-83`). A recompute after lock is correctly refused. `LockDay` likewise `WHERE ... status <> 'locked'` and disambiguates not-found vs already-locked via a follow-up `GetDayByID` (`days.go:193-198`). The integration test confirms the re-lock 409 (`phase6_integration_test.go:115-117`). **Solid.**
- Compute and lock each run in one tx with `audit.WriteWithOutbox` and commit-last — atomic.
- Tender breakdown uses `FILTER (WHERE tender_type = ...)` over `payments` scoped to the day's shifts with `status='recorded'` (`days.go:77-85`, `167-179`) — clean, single-pass, no N+1.

### Defects

**REV-08 (Medium) — Lock route has no station-level authorization.** `r.With(s.requirePermission("period.lock", nil))` (`server.go:620`) passes `nil` for the station resolver, and `period.lock` is `station_scoped=false` (`0004:135`). The day is fetched by `id + tenant_id` (`days.go:189`), so tenant isolation holds, but **any holder of the tenant-wide `period.lock` can lock any station's revenue day**. For a multi-station tenant where a station manager should only finalize their own station, this is a privilege-scoping gap. Compute/overview *are* station-scoped (`server.go:616`), so the asymmetry is sharp: you can't recompute another station's day but you can freeze it. Consider resolving the station from the revenue-day row and authorizing against it.

**REV-09 (Low) — Compute is a WRITE gated by a READ permission.** `handleComputeRevenueDay` performs an upsert (a state change) but rides `requirePermission("revenue.read", ...)` (`server.go:616`). Anyone who can *view* revenue can *materialize/overwrite* a draft revenue_days row. Low because the row is recomputable and lock-protected, but a write under a read permission is a conventions deviation.

**REV-10 (Medium) — `cash_variance` is mislabeled and conflates credit (a receivable) with cash.** `ComputeDay` sets `cash_variance = p.tender - s.gross` (`days.go:71`), where `p.tender = SUM(amount)` over **all** tender types including `credit` and `voucher` (`days.go:82`). Credit is a receivable, not collected cash; a voucher is a settlement instrument. So the field named `cash_variance`:
  (a) is actually *total-tender-vs-gross* variance, not a cash variance; and
  (b) includes credit sales as if they were collected, masking under-collection.
A day fully "paid" on credit shows `cash_variance ≈ 0` despite zero cash in the drawer. The field name will mislead finance. Either rename to `tender_variance`, or compute a true cash variance against cash+momo only. (The genuine cash-vs-expected reconciliation lives in the per-shift payment reconciliation, but the *day* surfaces this misleading aggregate.)

**REV-11 (Low) — `ComputeDay` does not verify the operating day belongs to the URL station.** The select is `FROM operating_days od ... WHERE od.tenant_id=$1 AND od.id=$3` (`days.go:86`) — it never checks `od.station_id = $2`. A caller can POST station A's URL with station B's `operating_day_id`; the resulting `revenue_days` row records station A's `station_id` with `business_date` copied from station B's day, and zero sales (sales filter on both `station_id` and `operating_day_id`, so none match). The `revenue_days_day_fk` is `(tenant_id, operating_day_id)` only — no station component — so the DB won't catch it. Result: a junk zero-revenue day attributed to the wrong business date. Add `AND od.station_id = $2`.

**REV-12 (Low) — `RevenueOverview` issues several sequential round-trips on the pool, not batched.** `handleRevenueOverview` (`revenue_close_handlers.go:160`) does: station Get, `LatestActiveDayForStation`, `DaySummary`, `DayTenders`, `RecentDays` — five separate queries for one dashboard call. Not an N+1 (counts are fixed), but it is a chatty "one-call dashboard." Acceptable; noted for completeness.

---

## Flow 5 — Tender & receivables linkage (light)

`revenue_days` references `payments` only by aggregation (`days.go:77-85`), never by FK — appropriate for a rollup. Tenant isolation on the payments aggregation is correct (`tenant_id=$1`, shifts sub-select also `tenant_id=$1`). Deep payment/receivables correctness is the payments agent's scope; nothing here mis-handles the linkage beyond REV-10's credit-as-cash conflation.

---

## Cross-cutting checks

- **Tenant isolation / IDOR:** Every query in `pricing`, `revenue`, and the cost helpers takes `tenantID` first and filters on it; composite tenant FKs are present on all new tables (`0032`, `0033`, `0036`). No raw ID lookups without tenant scoping. `ListForStationDay` and `ListForShift` filter by tenant. **No IDOR found** in this domain (RLS is inert at runtime per prior audits, but the app layer scopes correctly — except the *station*-level gap in REV-08).
- **AuthZ on mutating routes:** SetPrice → `price.change` (station-scoped) ✓; Compute → `revenue.read` (read perm on a write — REV-09); Lock → `period.lock` (tenant-wide, no station scope — REV-08). Recognition is internal to the approval tx, gated by `shift.approve` upstream ✓.
- **tx + audit + outbox atomicity:** All mutations (SetPrice, ComputeDay, LockDay, recognizeShiftRevenue) wrap the business change + `audit.WriteWithOutbox` in one tx and commit last. ✓
- **Money discipline:** Money/rate/litre columns use the correct `numeric` precisions; arithmetic is SQL-side. The *only* Go-side money handling is `parseDecimal` (float) used for the below-cost guard comparison (REV-09 boundary risk) and for input validation (`>= 0`) — values are never stored from the float. ✓ with the noted caveat.
- **Rounding:** Each money component is `ROUND(...,2)` exactly once; net is derived as `gross − tax` (both pre-rounded) so the three always reconcile per row. No cross-row accumulation (one row per nozzle). The COGS `ROUND(litres*avg_cost,2)` rounds once. Rounding is sound; the *inputs* (avg_cost) are not (REV-01/02).
- **Idempotency:** Strong — `uq_sales_shift_nozzle` + `uq_stock_mvt_sales_per_shift_tank` + lock-aware upsert. A shift's sales cannot be double-posted on re-approval. ✓
- **Error handling / status codes:** Mostly correct — 404 product not found, 422 below-cost, 409 locked, 400 bad FK. One nit: `handleComputeRevenueDay` maps a FK violation to 400 "operating day not found for this station" (`revenue_close_handlers.go:85`), but per REV-11 a wrong-station day does *not* violate the FK, so the user gets a silent junk row instead of an error.
- **Dead code:** `MovingAverageCost` (`costing.go:16`) appears unused by Phase 6 (recognition and valuation inline their own LATERAL rather than calling it); confirm whether any caller remains or remove. The duplicated cost-aggregation SQL across four sites (REV-01) is a maintenance hazard — a single shared helper would make the REV-01 fix one-line instead of four.

---

## Findings

| ID | Severity | File:Line | Issue | Fix |
|----|----------|-----------|-------|-----|
| REV-01 | Critical | `internal/revenue/repo.go:104-109`, `:191-196`; `internal/inventory/costing.go:18-22`, `:37-43` | Delivery-cost average omits `status='posted'`/`supersedes_id IS NULL`; a reversed delivery's positive-litres original stays in the weighted average forever (contra has NULL cost & negative litres, excluded), permanently corrupting COGS, margin, stock value, and the below-cost guard. | Add `AND sm.status='posted' AND sm.supersedes_id IS NULL` to all four delivery-cost aggregations (ideally one shared helper). |
| REV-02 | High | `internal/revenue/repo.go:105`; `internal/inventory/costing.go:11-15` | Labeled "moving-average" but actually a lifetime/cumulative average over all deliveries; consumed stock never decrements the basis, diverging from a true perpetual weighted average as prices move. | Either implement perpetual weighted-average over on-hand stock, or rename + document the cumulative-average policy as deliberate. |
| REV-03 | High | `internal/revenue/repo.go:121-122`; `phase6_integration_test.go:64-66`; `cmd/seed/main.go:165` | Tax-inclusive net/tax split is exercised only with `tax_rate=0` in tests, while production seeds 18%. The most-used split path has zero coverage feeding `tax_total`/`net_revenue`. | Add an integration test with a non-zero `tax_rate`; assert exact net/tax/gross reconciliation. |
| REV-04 | Medium | `internal/revenue/repo.go:101` | Recognition resolves price as of `now()` (approval), not the close/sale time frozen in `shift_close_lines.unit_price`; a mid-window price change desyncs recognized gross from operator-expected cash. | Recognize against the close-line snapshot or as-of the business date; make the choice explicit. |
| REV-05 | Medium | `pricing_handlers.go:108`; `internal/inventory/costing.go:35` | Below-cost guard uses a station-wide product cost average; recognition uses the per-tank average — guard can pass a price booked at negative per-tank margin. | Use the same per-tank (or per-nozzle's tank) cost basis in the guard. |
| REV-06 | Medium | `internal/revenue/repo.go:126`; `revenue_close_handlers.go:198-203`; `days.go:69-71` | `margin = net − cogs` (correct) is surfaced beside `gross_revenue` with no margin-% and `margin ≠ gross − cogs` when tax>0; invites wrong downstream margin-% math. | Expose net alongside gross and/or a computed gross-margin-% on the correct (net) basis; document. |
| REV-07 | Low | `internal/inventory/sales.go:64-66` vs `internal/revenue/repo.go:88` | Revenue can be recognized for a shift whose tanks are skipped at ledger posting (un-onboarded), creating a transient revenue/inventory divergence until manual backfill. | Block recognition (or warn) when the matching stock-out posting is skipped; reconcile on backfill. |
| REV-08 | Medium | `services/api/internal/server/server.go:620` | Lock route uses `requirePermission("period.lock", nil)` (tenant-wide, no station scope); any holder can lock *any* station's day, while compute is station-scoped. | Resolve station from the revenue-day row and authorize against it. |
| REV-09 | Low | `services/api/internal/server/server.go:616`; `pricing_handlers.go:114,230` | Compute (a write/upsert) is gated by `revenue.read`; and below-cost guard compares money as `float64` via `parseDecimal` (boundary risk). | Gate compute on a write permission; compare prices as numeric (DB-side or big.Rat) in the guard. |
| REV-10 | Medium | `internal/revenue/days.go:71,82` | `cash_variance = total_tender − gross` includes credit (a receivable) and voucher as if collected, and is mislabeled "cash"; a credit-only day shows ~0 variance despite no cash collected. | Rename to `tender_variance`, or compute a true cash variance (cash+momo only). |
| REV-11 | Low | `internal/revenue/days.go:86` | `ComputeDay` doesn't verify `operating_day.station_id = $2`; cross-station day id produces a junk zero-revenue row with the wrong business_date (FK is `(tenant_id, day_id)` only). | Add `AND od.station_id = $2` and return 400/404 when no match. |
| REV-12 | Low | `revenue_close_handlers.go:160-217` | RevenueOverview issues five sequential pool round-trips for one dashboard call. | Acceptable; batch if it becomes hot. |
| REV-13 | Low | `internal/operations/shifts.go:158-159`; `shift_exceptions_handlers.go:68` | Approval status check (outside tx) and the UPDATE (no `status='closed'` guard) are not atomic (TOCTOU); concurrent approvals both pass. Damage bounded by idempotency, but `approved_by/at` can be overwritten + double audit. | Add `AND status='closed'` to the `ApproveShift` UPDATE. |
| REV-14 | Info | `internal/inventory/costing.go:16` | `MovingAverageCost` appears unused by Phase 6 (callers inline their own LATERAL). | Confirm callers; remove if dead, or route the four cost queries through it (also fixes REV-01 in one place). |

---

## Severity counts

| Severity | Count |
|----------|-------|
| Critical | 1 |
| High | 2 |
| Medium | 5 |
| Low | 5 |
| Info | 1 |
| **Total** | **14** |

## Top-5 risks

1. **REV-01 (Critical) — Reversed deliveries permanently corrupt moving-average cost.** Silent, systemic; poisons COGS, margin, stock value, and the price guard for any tank that ever had a delivery reversed. The book balance still looks right, so it won't be caught by inventory checks. `revenue/repo.go:104-109` + 3 sibling sites.
2. **REV-03 (High) — The 18%-tax-inclusive split is shipped but untested.** Tests only run `tax_rate=0`; the production split feeding `tax_total`/`net_revenue` to finance has no coverage. `revenue/repo.go:121-122`.
3. **REV-02 (High) — "Moving average" is actually a lifetime cumulative average.** Materially wrong vs a true perpetual weighted average when prices trend; mislabeled and undocumented as a policy. `revenue/repo.go:105`.
4. **REV-10 (Medium) — `cash_variance` counts credit as collected cash.** A credit-financed day reads ~zero variance; the field name will actively mislead finance. `revenue/days.go:71,82`.
5. **REV-08 (Medium) — Revenue-day lock has no station authorization.** Tenant-wide `period.lock` + `nil` scope lets any holder freeze any station's day, asymmetric with station-scoped compute. `server.go:620`.
