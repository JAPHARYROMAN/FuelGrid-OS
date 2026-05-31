# Inventory costing policy (FuelGrid OS, Phase 6 / FIN-4)

This document is the authoritative statement of how FuelGrid OS values stock,
computes cost of goods sold (COGS) and margin, and how the below-cost selling
price guard derives its cost floor.

## TL;DR

FuelGrid OS uses a **cumulative (lifetime) weighted-average landed cost** per
litre. It is **not** a perpetual ("moving") weighted-average cost. Consumed
(sold) stock never lowers the cost basis. The two policies coincide only while a
tank's landed cost per litre is constant across deliveries; they diverge as
cost rises or falls over a tank's life.

This naming was previously misleading: the helpers and comments said
"moving-average" while the SQL computed a cumulative average. The algorithm is
unchanged; the naming and documentation were corrected (path **(B)**) so the
policy is no longer mislabeled.

## What the policy computes

For a tank, the cost basis per litre is:

```
avg_cost = SUM(litres * landed_cost_per_litre) / SUM(litres)
```

aggregated over every stock movement that is:

- `movement_type = 'delivery'` (a stock-in / Phase-5 receipt),
- `landed_cost_per_litre IS NOT NULL` (a *costed* delivery),
- `litres > 0`,
- `status = 'posted'` and `supersedes_id IS NULL` (so reversed/superseded
  delivery originals and their contras are excluded).

This single expression is used identically at four sites, all reading the same
columns:

- `internal/inventory/costing.go` — `WeightedAverageCost` (per tank) and
  `AverageLandedCostForStationProduct` (per station+product; the below-cost
  guard's floor).
- `internal/revenue/repo.go` — the `avg_cost` LATERAL in `RecognizeShiftSales`
  (per tank, used for recognized COGS/margin) and the `avg_cost` LATERAL in
  `InventoryValuation` (per tank, used for stock value).

All arithmetic is performed in SQL `numeric`; litres and costs are decimal
strings end to end, never `float64`. (The only Go-side float is the below-cost
guard's boundary comparison, which never stores a value.)

### Derived figures

- **COGS** per recognized sale: `ROUND(litres * avg_cost, 2)` (NULL when the
  tank has no costed delivery — "unknown", not zero).
- **Margin** per recognized sale: `net - COGS` (gross profit excludes remitted
  tax; see the revenue-day reporting note about gross vs net basis).
- **Stock value** per tank: `ROUND(book_litres * avg_cost, 2)`, where
  `book_litres = SUM(litres)` over all movements (contras net out the quantity).

## The limitation: it does not decrement on consumption

Because the average is taken over *all deliveries over the tank's whole life*,
selling litres never removes their cost from the numerator or denominator. A
true perpetual moving average recomputes the basis on each receipt over the
**on-hand** quantity and consumes proportionally on each sale.

### Worked example (buy-high / sell / buy-low)

A tank:

1. Receives 10,000 L @ 2,400/L.
2. Sells 9,000 L.
3. Receives 10,000 L @ 3,000/L.

| Policy | Cost basis after step 3 |
| --- | --- |
| **Cumulative (this system)** | `(10000*2400 + 10000*3000) / 20000` = **2,700/L** |
| True perpetual moving average | remaining 1,000 L @ 2,400 blended with 10,000 L @ 3,000 = `(1000*2400 + 10000*3000) / 11000` ≈ **2,945/L** |

The cumulative figure (2,700) under-weights the more recent, dearer stock that
actually remains on hand. COGS recognized against the next litres sold will be
~245/L too low, and margin correspondingly too high, until the cheap early
litres age out of the average — which, under this policy, they never fully do.

## When it is accurate

The cumulative average equals the perpetual moving average (and is therefore
exactly correct) when **landed cost per litre is constant across a tank's
costed deliveries**. In practice it is a close, defensible approximation when:

- deliveries are frequent relative to price moves (each new receipt re-weights
  the basis toward current cost), and
- landed cost per litre is reasonably stable over the tank's life.

It drifts materially — and over-/under-states COGS, margin, and stock value —
when landed cost trends strongly up or down over many deliveries and stock
turns over slowly.

## Reversed / superseded deliveries

The aggregation filters `status = 'posted' AND supersedes_id IS NULL`, so a
reversed delivery's original row is excluded from the basis (its contra, which
has negative litres and a NULL cost, is excluded by `litres > 0` and the
NOT-NULL cost filter). A correctly reversed wrong-cost delivery therefore does
not pollute the average. (This is the REV-01 fix; this doc assumes it is in
place.)

## Guard vs recognition cost basis

The below-cost price guard uses a **station-wide product** average
(`AverageLandedCostForStationProduct`), while recognition costs each sale at the
**specific tank's** average. Both are cumulative weighted averages, but over
different populations, so the guard can pass a price above the blended station
cost yet below an individual tank's cost (audit finding REV-05). That is a
separate concern from this costing-policy correction and is not changed here.

## Why not implement a perpetual moving average now (path A)

A perpetual moving average is a sequential fold over the tank's ledger in `seq`
order (recompute basis on each receipt over on-hand quantity; consume at the
current basis on each sale), which a single `SUM(...)` cannot express — it
requires a recursive CTE or a custom running aggregate. It is also entangled
with posting order: a shift's `sales` stock-out movements are posted in the
same transaction **before** revenue recognition runs
(`postShiftSales` → `recognizeShiftRevenue`), so a naive on-hand average at
recognition time would already have consumed the very litres being costed.
Getting that right (costing as of the ledger position just before the shift's
own outflow) is intricate and must be proven with a buy-high/sell/buy-low
integration test. Those DB-backed tests run only in CI, so a perpetual
implementation cannot be proven correct locally. Rather than ship a
subtly-wrong algorithm, this change corrects the labeling and documents the
policy. Migrating to a true perpetual moving average remains tracked as future
work (audit finding REV-02).
