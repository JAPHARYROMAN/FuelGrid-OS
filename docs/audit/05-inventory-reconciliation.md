# Audit 05 — Inventory & Reconciliation Domain

**Repo:** FuelGrid OS (Go + chi + pgx/v5 + Postgres 16, multi-tenant SaaS)
**Scope:** the per-tank stock-movement ledger, opening-balance handling, book-vs-physical reconciliation lifecycle, and the inventory/reconciliation overview surfaces. The goods-*receiving* orchestration in `deliveries.go` is out of scope (covered by the procurement audit); this report touches `deliveries.go` only where it posts to the ledger.
**Method:** read-only, atomic-level. Every claim cites `file:line`. Uncertainty is marked explicitly.

## Files & LOC reviewed

| File | LOC | Role |
|---|---|---|
| `internal/inventory/repo.go` | 329 | Stock ledger: `PostMovement`, `CurrentBalance`, `ReverseMovement`, opening-balance, `hasOpening*` |
| `internal/inventory/periods.go` | 73 | `PeriodTotalsSince`, `MaxSeqForTank`, `AverageDailySales` |
| `internal/inventory/sales.go` | 82 | `PostSalesForShift`, `SalesPostedForShift` |
| `internal/inventory/costing.go` | 50 | `MovingAverageCost`, `AverageLandedCostForStationProduct` (touched for litre discipline) |
| `internal/inventory/deliveries.go` | 377 | (ledger-post path only) |
| `internal/reconciliation/repo.go` | 297 | Recon persistence: `UpsertDraft`, `Seal`, `LastSealedForTank`, `RecentForTank`, lists |
| `internal/readings/dips.go` | 209 | `FirstDipForTank`, `ClosingDipForTankDay`, `LatestDipsForStation` |
| `internal/readings/repo.go` | 123 | meter readings (peripheral) |
| `services/api/internal/server/inventory_handlers.go` | 221 | ledger/balance/opening HTTP |
| `services/api/internal/server/reconciliation_handlers.go` | 625 | compute/preview/persist/adjust/seal HTTP |
| `services/api/internal/server/inventory_overview_handler.go` | 270 | inventory + reconciliation overview dashboards |
| `services/api/migrations/0024_stock_movements.*` | — | ledger table + RLS + perms |
| `services/api/migrations/0025_deliveries.*` | — | deliveries table |
| `services/api/migrations/0026_sales_idempotency.*` | — | sales unique index |
| `services/api/migrations/0027_tank_reconciliations.*` | — | recon table + perms |
| `services/api/internal/server/phase4_integration_test.go` | 730 | ledger/recon integration tests |

---

## 1. The stock ledger — append-only correctness & sign conventions

### Design (correct in principle)
`0024_stock_movements.up.sql` defines `litres numeric(14,3)` and `balance_after numeric(14,3)`, with `seq bigint GENERATED ALWAYS AS IDENTITY` as the true append order (lines 26, 32-33). The package doc (`repo.go:1-14`) states book stock is **derived** by summing the ledger; `balance_after` is a per-row snapshot, the sum is authoritative. Corrections never rewrite litres — `ReverseMovement` (`repo.go:221-252`) marks the original `'reversed'` and posts a contra with negated litres + `supersedes_id`, so original+contra net to zero and `SUM(litres)` spans all rows regardless of status. This is a sound, auditable design and the integration test (`phase4_integration_test.go:127-170`) confirms reversal arithmetic.

Movement types (`opening|delivery|sales|adjustment|transfer`) are constrained at the DB (`0024:42-44`) and mirrored in Go constants (`repo.go:28-34`). Sign convention is `+in / -out`: sales post negative (`sales.go:73`, `Litres: -ln.LitresSold`); deliveries/opening post positive. `transfer` is declared everywhere but **never produced by any code path** (see INV-014, dead type).

### INV-001 (Critical) — Litres carried as `float64` throughout, violating the money/litre discipline
`Movement.Litres` and `Movement.BalanceAfter` are `float64` (`repo.go:80-81`); `PostInput.Litres` is `float64` (`repo.go:100`); the DTO is `float64` (`inventory_handlers.go:26-27`); every reconciliation figure is `float64` (`reconciliation/repo.go:37-51`, `reconciliation_handlers.go:28-37`). The house rule is *litres = numeric(14,3) as decimal STRINGS in Go, arithmetic in SQL, NEVER float* — and the codebase already honours this for money (landed cost is `*string` cast `::numeric`, `costing.go:16-30`). Litres is the glaring exception.

Consequences, all real:
- **Round-trip drift.** `PostMovement` binds `in.Litres float64` to a `numeric(14,3)` column (`repo.go:151-154`). `RETURNING litres` scans back into `float64`. Values that aren't exactly representable in binary float (e.g. a write-off of `0.001` L, or `33333.333`) accumulate representation error across re-reads. The seal write-off explicitly compares against `0.0005` (`reconciliation_handlers.go:536`) precisely because float residue is expected — a tell that the team knows this is lossy.
- **Variance math done in Go float.** `variance := closingBook - dip.VolumeLitres` and `variancePercent = variance / closingBook * 100` run in float64 (`reconciliation_handlers.go:174-177`), not SQL numeric. `overTolerance` does `math.Abs(variance) > math.Abs(book)*tolerancePercent/100` in float (`reconciliation_handlers.go:45-47`). A variance landing exactly on the tolerance boundary can flip class (`draft` vs `exception`) on float rounding.
- **JSON precision.** litres surface to the client as JSON numbers (float64), so a UI/finance consumer cannot rely on exact 3-dp litres.

This is the single most pervasive deviation in the domain. **Fix:** carry litres as decimal strings end-to-end (mirror the landed-cost pattern); do `closingBook`, `variance`, `variance_percent`, and the tolerance comparison in SQL numeric (a single `compute` query returning text), not Go float. At minimum, do all ledger arithmetic in SQL and never re-derive balances in Go.

### INV-002 (High) — No DB-level append-only enforcement; `litres` is freely UPDATE-able
The "append-only" guarantee is purely a *convention* in the repo layer. `0024_stock_movements.up.sql` has **no** trigger or rule forbidding `UPDATE litres`/`UPDATE balance_after`/`DELETE`. The only trigger is `stock_movements_set_updated_at` (`0024:69-71`), which *expects* UPDATEs. `ReverseMovement` itself issues `UPDATE stock_movements SET status='reversed'` (`repo.go:223-225`), so a blanket "no UPDATE" trigger isn't possible, but a column-guard trigger (reject any UPDATE that changes `litres`, `tank_id`, `movement_type`, `seq`, or `balance_after`) is. Without it, a future bug, a stray migration, or direct SQL can silently rewrite posted litres and corrupt the derived balance with no audit trace. **Fix:** add a `BEFORE UPDATE` trigger that raises unless only `status`/`updated_at`/`notes` change; forbid `DELETE` (or restrict to a superuser maintenance role).

### INV-003 (High) — `PostMovement` computes `balance_after` with no row lock → races under concurrent posts
`PostMovement` (`repo.go:145-159`) inserts `balance_after = (SELECT COALESCE(SUM(litres),0) FROM stock_movements WHERE tenant_id=$1 AND tank_id=$2) + $6`. All handler transactions begin via `s.deps.DB.Begin(ctx)` (e.g. `inventory_handlers.go:183`, `reconciliation_handlers.go:434`,`526`), which is pgx default **Read Committed** with **no `SELECT ... FOR UPDATE`** and no advisory lock on the tank. Two concurrent transactions posting to the *same* tank each read the committed sum *before* the other commits, so both compute the same `balance_after`. After both commit, `SUM(litres)` (the authoritative `CurrentBalance`) is still correct, but the two rows carry **wrong `balance_after` snapshots** — the running balance displayed in the ledger (`ListMovements`, used by the UI and asserted in tests at `phase4_integration_test.go:122`) is then internally inconsistent with the litres column. Because `seq` is assigned by the IDENTITY and ordering is by `seq`, you can also get a later-`seq` row showing a *lower* balance_after than an earlier one. This is a latent data-quality bug, not a balance-corruption bug (the sum stays right), but it undermines the ledger's auditability. **Fix:** take `pg_advisory_xact_lock(hashtextextended(tank_id::text))` or `SELECT 1 FROM tanks WHERE id=$tank FOR UPDATE` at the top of the post, so balance snapshots serialize per tank.

### INV-004 (Medium) — Book balance can go negative; no non-negative / dead-stock guard
Nothing prevents `CurrentBalance` from going below zero. `PostSalesForShift` posts `-ln.LitresSold` unconditionally (`sales.go:68-75`) — if metered sales exceed book stock (mis-calibrated dip, missed delivery), the ledger goes negative and the tank silently reports negative book stock through `book/CapacityLitres*100` fill% (`inventory_overview_handler.go:117-119`, which would render a negative fill%). The `tanks` table carries `dead_stock_litres`/`safe_min_litres` (`tanks/repo.go`) but the ledger never references them. This may be intentional (sales must post even if it implies a prior under-recording), but there is no warning/exception raised, no clamp, and reconciliation will only catch it at day end. **Fix:** at minimum flag negative book balance as a stock exception at post time; document the intended policy.

### INV-005 (Low) — `ReverseMovement` re-derives the original via a second query for error disambiguation
`repo.go:229-235`: when the `UPDATE ... RETURNING` matches no row, it calls `r.GetMovement` (a fresh pool query) to decide between `ErrMovementNotFound` and `ErrAlreadyReversed`. Two issues: (a) `GetMovement` uses `r.pool`, not the caller's `tx` (`repo.go:203-207`), so inside an open transaction it reads a *different* snapshot than the failed UPDATE — for a movement created earlier in the *same* uncommitted tx this would wrongly return `ErrMovementNotFound`; (b) it's a needless second round-trip. Not currently exploitable (reversal isn't wired to any HTTP route — see INV-013) but a correctness trap if it ever is. **Fix:** disambiguate with a single `tx` query, or fetch status within the tx.

### INV-006 (Info) — `ReverseMovement` is reachable only from tests; no HTTP route reverses a movement
`grep` for `ReverseMovement` finds callers only in `repo.go` and `phase4_integration_test.go`. There is no handler exposing reversal, so the contra mechanism — a core design pillar — is **untested in production paths** and effectively dead at the API surface. Either wire it (with `stock.adjust` authZ + audit) or mark it explicitly deferred.

---

## 2. Opening balance

### INV-007 (Medium) — Opening-balance idempotency guard is a TOCTOU with no unique index backstop
`SetOpeningBalance` (`repo.go:308-329`) checks `hasOpeningTx` then posts the genesis `'opening'`. The guard predicate (`hasOpeningPredicate`, `repo.go:268-271`) is `movement_type='opening' AND status='posted' AND (source_ref_type IS NULL OR <> 'correction')`. There is **no unique index** enforcing "one opening per tank" — unlike sales, which has `uq_stock_mvt_sales_per_shift_tank` (`0026`). Two concurrent `POST /opening-balance` for the same tank under Read Committed both pass `hasOpeningTx` (neither sees the other's uncommitted row) and both insert an opening movement. Result: a tank with **two** opening movements, double-counting the opening into book stock. The handler maps `ErrOpeningExists` to 409 (`inventory_handlers.go:194-196`), but that guard never fires in the race. **Fix:** add a partial unique index `(tank_id) WHERE movement_type='opening' AND status='posted' AND (source_ref_type IS NULL OR source_ref_type<>'correction')` and treat the unique violation as the 409.

### INV-008 (Medium) — Opening can be seeded with `litres=0`, and negative is the only thing blocked
`handleSetTankOpeningBalance` rejects `req.Litres == nil || *req.Litres < 0` (`inventory_handlers.go:176-178`) but **allows 0**. A zero opening passes `hasOpening` (it's a posted `'opening'` row) and then `requiresOpening` lets deliveries/sales post — but it also means a tank "opened" at 0 is indistinguishable from a genuine empty tank, and `from_dip` could equally seed 0 from a zero-volume dip with no warning. Low impact, but a 0 opening on a tank that actually holds fuel will produce a large day-1 variance that looks like loss. **Fix:** decide policy; if 0 is invalid, reject it; if valid, document.

### INV-009 (Low) — `OpeningBalance` reader uses `ORDER BY seq DESC LIMIT 1`, masking the duplicate-opening bug
`repo.go:290-303`: `OpeningBalance` selects the *latest* matching opening. Combined with INV-007, if two openings exist this silently returns only one, so `computeReconciliation` (`reconciliation_handlers.go:137-140`) reads one opening's litres for `openingBook` while `CurrentBalance` sums *both* — guaranteeing a phantom variance equal to the duplicate. The `DESC LIMIT 1` hides the data problem instead of surfacing it.

### Opening — what's correct
The `requiresOpening` gate (`repo.go:61-68`) correctly exempts `opening` and `adjustment` (so reversal contras and recon write-offs can post) and forces `delivery|sales|transfer` to fail with `ErrNoOpeningBalance` (`repo.go:135-142`). Tests cover the delivery-before-opening rejection (`phase4_integration_test.go:193-204`) and the from-dip path (`:247-264`). `from_dip` correctly reads `FirstDipForTank` ordered by `recorded_at, created_at` (`dips.go:97-109`). Opening writes are wrapped in tx + `audit.WriteWithOutbox` (`inventory_handlers.go:204-216`) and gated by `stock.adjust` station-scoped (`inventory_handlers.go:155`). Good.

---

## 3. Book vs physical & the reconciliation compute

### Flow
`computeReconciliation` (`reconciliation_handlers.go:116-187`):
1. Guard: all the day's shifts approved, else 409 (`:119-126`).
2. Opening book + watermark: last sealed recon's `closing_physical`/`through_seq` if any; else genesis opening's `Litres`/`Seq`; else 0/0 (`:130-150`).
3. `PeriodTotalsSince(fromSeq)` (`periods.go:26-41`) sums per type for `seq > fromSeq`; sales reported positive via `-SUM(... 'sales')`.
4. `closingBook = openingBook + OpeningTotal + Deliveries - Sales + Adjustments` (`:157`).
5. Physical = day's closing dip (`ClosingDipForTankDay`, `dips.go:145-159`).
6. `variance = closingBook - dip.VolumeLitres`; `variancePercent = variance/closingBook*100` (`:174-178`).
7. Classify with product `LossTolerancePercent` (`:184-185`).

### INV-010 (High) — Opening-balance double-count between `openingBook` and `PeriodTotalsSince` on the genesis path
When there is **no prior sealed reconciliation**, `fromSeq = opening.Seq` (`reconciliation_handlers.go:140`). `PeriodTotalsSince` then sums `seq > fromSeq` (`periods.go:36`), which correctly *excludes* the genesis opening row, so `OpeningTotal` is 0 on the first reconciliation. Good. **But** `closingBook` adds `totals.OpeningTotal` (`:157`) and `PeriodTotals.OpeningTotal` is documented "genesis only" (`periods.go:18`). If any *additional* `'opening'`-type row ever exists with `seq > fromSeq` (which INV-007's duplicate opening, or any future re-open path, would create), it gets added to `closingBook` **on top of** `openingBook` — double counting. The arithmetic is only safe because the system assumes exactly one opening ever. Marked High because it compounds with INV-007. **Fix:** never include `opening`-type rows in the period sum after the anchor is established; assert at most one opening exists.

### INV-011 (Medium) — `variancePercent` and tolerance both divide by `closingBook`; a near-zero book makes the percentage meaningless / tolerance trivially exceeded
`variancePercent = variance/closingBook*100` guards only `closingBook != 0` (`reconciliation_handlers.go:176`). `overTolerance` computes `|variance| > |book|*tol/100` (`:45-47`). When `closingBook` is small but non-zero (tank nearly empty), the percentage explodes and `|book|*tol/100` shrinks toward 0, so *any* physical discrepancy trips `exception` and blocks sealing (`:521-523`) — the operator can't seal a legitimately near-empty tank without an adjustment. Conversely a tank at exactly 0 book skips the percentage entirely (left at 0) yet `overTolerance` returns `|variance| > 0` → exception for any non-zero physical. The float equality `closingBook != 0` (INV-001) makes the boundary fragile. **Fix:** define tolerance against capacity or an absolute litre floor, not solely `closingBook`.

### INV-012 (Medium) — Compute reads opening/last-sealed from the **pool**, not the caller's tx → adjustment recompute can read a stale anchor
In `handleAdjustReconciliation` the recompute passes the `tx` as `invQ` so `PeriodTotalsSince` sees the just-posted adjustment (`reconciliation_handlers.go:466`, and `periods.go:26` takes a `Querier`). **However**, inside `computeReconciliation`, `LastSealedForTank` (`:132`) and `OpeningBalance` (`:137`) use `s.reconciliation`/`s.inventory` repos that query `r.pool` directly (`reconciliation/repo.go:127-135`, `repo.go:290-295`) — **not** the tx. For the adjustment path these read-only anchors don't change within the tx, so it's currently benign; but it's an inconsistency: only *part* of the compute is tx-aware. If sealing and adjusting ever interleave, or a future change posts an opening/seal inside the same tx, the compute would mix a pre-tx anchor with post-tx period totals. **Fix:** thread the `Querier` through `LastSealedForTank`/`OpeningBalance` too, or document that anchors are immutable within a recompute.

### INV-013 (Low) — `PeriodTotalsSince` and `MaxSeq` ignore movement `status`
`periods.go:30-37` sums litres by type with no `status` filter, by design (a reversed row + its contra net to zero). Correct for balance. But `DeliveriesTotal`/`SalesTotal`/`AdjustmentsTotal` reported to the user (`reconciliation_handlers.go:180-181`) **include reversed originals and their contras**. A reversed 10,000 L delivery shows `deliveries_total` inflated by 10,000 with a compensating −10,000 sitting in `adjustments_total` (the contra carries `movement_type = orig.MovementType`, `repo.go:244` — so a reversed *delivery* contra is typed `'delivery'`, landing in `DeliveriesTotal` as −10,000, netting correctly but making the per-type display confusing). The net `closingBook` is right; the line items are misleading for audit. **Fix:** decide whether period line-items should be net-of-reversals and filter accordingly, or surface reversed rows separately.

---

## 4. Reconciliation lifecycle — persist, adjust, seal, balance-forward

### Persist (`handlePersistReconciliation`, `:276-368`)
- Normalizes `operating_day_id` from body→query then validates via `tankAndDayForReconcile` (`:282-295`), which checks the day belongs to the tank's station (`:225-228`). Good.
- Pre-checks sealed → 409 (`:304-307`).
- Computes with `s.deps.DB` (pool), then in a tx: `UpsertDraft` + audit + conditional `stock_variance.raised` event on transition into exception (`:322-358`). `UpsertDraft` (`reconciliation/repo.go:223-258`) `ON CONFLICT (tank_id, operating_day_id) DO UPDATE ... WHERE status<>'sealed'` and returns `ErrSealed` when the guard suppresses the row. Solid idempotency: re-running compute overwrites the draft. The `uq_tank_recon_tank_day` unique (`0027:50`) backs it.

### INV-014 (High) — Compute-then-persist is non-atomic: TOCTOU between the pool-based compute and the tx write
`handlePersistReconciliation` computes against `s.deps.DB` (`:309`) **before** opening the tx (`:315`). `handleSealReconciliation` does the same (`:516` compute on pool, `:526` begin tx). Between compute and the tx, another request can approve/close a shift, post a delivery, or post an adjustment, so the persisted/sealed figures reflect a *stale* ledger snapshot. The seal path is worst: it computes `writeoff = ClosingPhysical - ClosingBook` from the pre-tx `computed` (`:535`), posts that exact write-off inside the tx, then seals with the pre-tx `closing_book`/`variance` while stamping `through_seq = MaxSeqForTank(tx)` (`:561`) — so `through_seq` reflects the *post-write-off* ledger but the sealed `closing_book` reflects the *pre-tx* compute. If a movement slipped in between, the ledger no longer lands exactly on the sealed `closing_physical`, and the next day's balance-forward anchor (INV-016) is wrong. **Fix:** recompute *inside* the tx (pass `tx` as `invQ`) and, ideally, lock the tank/day for the duration; the adjust path already recomputes in-tx (`:466`) and should be the template.

### INV-015 (Medium) — Seal does not re-verify the day's shifts are still approved within the tx, nor lock the day
`computeReconciliation`'s shift-approval guard (`:119-126`) runs on the pool pre-tx. Nothing stops a shift being re-opened/un-approved between compute and seal, and nothing prevents new shifts being added to the day after sealing (the recon seals `through_seq`, but a later same-day shift's sales would post with a higher `seq` and silently fall into the *next* period rather than the sealed day). Whether a day can gain shifts post-seal is a domain question, but the code neither prevents nor flags it. **Fix:** re-run the approval guard inside the seal tx; consider locking the operating day.

### Adjust (`handleAdjustReconciliation`, `:404-498`)
Validates non-zero litres + non-empty reason (`:415-422`), loads recon+tank+authZ via `reconciliationForManage` (`:372-397`, `reconciliation.manage`), rejects sealed (`:428-431`), then in one tx: posts an `adjustment` movement (`source_ref_type='reconciliation'`, `source_ref_id=rec.ID`, `:441-447`) + audit, **recomputes in-tx** (`:466`), `UpsertDraft`, audit (`:481-492`), commit. This is the correctly-atomic path. Sign convention is documented (`:416`, "sign indicates gain/loss"). Good — except it shares INV-001 (float litres) and INV-017 below.

### INV-016 (High) — Balance-forward correctness hinges on the seal write-off, which is float-thresholded and pre-tx-computed
The design intent (`0027:14-17`): sealed `closing_physical` opens the next period; the seal write-off forces the ledger onto the physical figure. `handleSealReconciliation:533-559` posts `writeoff = ClosingPhysical - ClosingBook` **only if** `math.Abs(writeoff) >= 0.0005` (`:536`). Two problems: (a) the threshold exists *because* litres are float (INV-001) — with proper numeric this comparison would be exact; (b) the write-off is computed from the **pre-tx** `computed` (INV-014), so if the ledger moved between compute and seal, the write-off lands the ledger on the wrong number and `LastSealedForTank.ClosingPhysical` (`reconciliation/repo.go:127-143`, ordered `through_seq DESC`) becomes a false anchor for tomorrow. The day-2 balance-forward test (`phase4_integration_test.go:544-560`) passes only because the test is single-threaded. **Fix:** compute the write-off and seal figures from a single in-tx compute; use numeric.

### INV-017 (Medium) — Adjustment & seal-writeoff post movements but never re-evaluate negative-balance or capacity
An operator can post an arbitrary adjustment of any magnitude/sign (`:443-447`) — e.g. +1,000,000 L — with only a free-text reason; there's no bound against tank capacity and no negative-book guard (INV-004). The seal write-off similarly posts unbounded. AuthZ is correct (`reconciliation.manage`), and it's audited, so it's traceable — but a fat-finger adjustment is accepted silently and immediately corrupts book stock and the sealed anchor. **Fix:** sanity-bound adjustments (e.g. reject if resulting book < 0 or > capacity without an override flag).

### Lifecycle — what's correct
Sealing immutability is enforced in **two** layers: the `WHERE status<>'sealed'` guard on both `UpsertDraft` (`reconciliation/repo.go:243`) and `Seal` (`:284`), plus handler pre-checks (`:304`, `:428`, `:510`). Re-seal → 409 is tested (`phase4_integration_test.go:540-542`). Seal-over-tolerance is blocked (`:521-523`, tested `:508-511`). All mutating paths are tx+audit+outbox. Status transition `draft→exception` raises a one-shot variance event (`:344-358`) — correctly de-duped against the prior status.

---

## 5. Overview endpoints

### INV-018 (Medium) — N+1 query fan-out in both overview handlers
`handleInventoryOverview` (`inventory_overview_handler.go:95-139`) loops over tanks and issues, **per tank**: `CurrentBalance` (`:97`), `RecentForTank` (`:103`), `AverageDailySales` (`:109`) — three pool round-trips × N tanks, on top of `tanks.List` + `LatestDipsForStation`. `handleReconciliationOverview` (`:251-268`) issues `CurrentBalance` per tank (`:253`). For a hyper-station with many tanks this is 3N+2 / N+k queries per dashboard load. `LatestDipsForStation` is already a single batched `DISTINCT ON` query (`dips.go:185-209`) — the per-tank balance/recent/sales should be too. **Fix:** batch `CurrentBalance` into one `GROUP BY tank_id` over the station, ditto recent variances and avg daily sales (window/lateral). Also note these reads run on the pool with **no `WithTenant`**, so RLS GUC is unset (the documented inert-RLS situation) — isolation rests entirely on the `tenant_id = $1` WHERE clauses, which are present and correct here.

### INV-019 (Low) — `AverageDailySales` divides by calendar `days`, not days-with-sales, and keys on `recorded_at`
`periods.go:58-73`: `SUM(-litres) WHERE movement_type='sales' AND recorded_at >= now()-interval / days`. (a) It divides by the full `days` window (7), so a tank that only sold on 2 of 7 days reports an artificially low daily rate → inflated `days_of_stock` (`inventory_overview_handler.go:120-123`). (b) `recorded_at` for sales is the *approval* time (`PostMovement` uses DB `now()`, no explicit recorded_at), so a backfilled/late-approved shift's sales land on the approval date, skewing the rate. Cosmetic for a dashboard estimate but worth a comment. The reversed-sales contras (`movement_type='delivery'`? no — contra of a sales is typed `'sales'` per `repo.go:244`) would also be summed here, slightly skewing the rate; status is not filtered.

### INV-020 (Low) — `FillPercent` and `DaysOfStock` can present negative/garbage on negative book
`inventory_overview_handler.go:117-123`: `FillPercent = book/Capacity*100` and `DaysOfStock = book/dailySales` with no clamp. Given INV-004 (book can go negative), the dashboard can render a negative fill% and negative days-of-stock. **Fix:** clamp at 0 and surface an explicit "negative book" warning.

### Overview — what's correct
Both overviews are gated by `requirePermission("inventory.read"|"reconciliation.read", stationFromURLParam("stationID"))` at the route (`server.go:364-367`), and load the station (404 if missing) before fanning out. `reconciliation-overview` resolves the day from `?operating_day_id` or latest-active (`:188-216`) and reports `all_shifts_approved` so the UI can gate the run button (`:220-226`). Reconciliations are keyed by tank into a map then joined to the tank list (`:234-266`) — no N+1 there.

---

## 6. Tenant isolation & authZ — cross-cutting

Tenant scoping is consistently `tenant_id = $1` (often first param) on **every** query in scope (`repo.go`, `periods.go`, `sales.go`, `costing.go`, `reconciliation/repo.go`, `dips.go`). Composite FKs use `(tenant_id, …)` (`0024:53-56`, `0027:42-47`). No raw-id lookups bypass tenant. AuthZ:
- Ledger/balance reads: `inventory.read` via `tankForInventoryRead` → `authorizeStation` against the tank's station (`inventory_handlers.go:55-75`). Tested cross-station 403 (`phase4_integration_test.go:172-181`).
- Opening write: `stock.adjust` (`inventory_handlers.go:155`).
- Recon read: `reconciliation.read`; run/adjust/seal: `reconciliation.manage` (`reconciliation_handlers.go:238,256,292,393,506` via the helpers). Station-scoped, in-handler.

### INV-021 (Low) — `handleListStationReconciliations` does not validate the station exists or that the day belongs to it
`reconciliation_handlers.go:596-623`: parses `stationID` + `operating_day_id`, then `ListForStationDay`. The route guards `reconciliation.read` on `stationID` (`server.go:359`), and `ListForStationDay` is tenant+station scoped (`reconciliation/repo.go:147-154`), so there is **no IDOR / cross-tenant leak** — a foreign station/day simply returns an empty list. The gap is only that a non-existent station or a day from a different station returns `200 {items:[]}` instead of 404/400, unlike the sibling overview/preview handlers which validate both. Cosmetic inconsistency, not a security defect.

### INV-022 (Info) — Handler transactions never set the RLS tenant GUC
Every inventory/reconciliation write uses `s.deps.DB.Begin(ctx)` (e.g. `inventory_handlers.go:183`, `reconciliation_handlers.go:315,434,526`), never `database.WithTenant` (`database/tenant.go:29`). So `app.current_tenant` is unset and the RLS policies on `stock_movements`/`tank_reconciliations`/`deliveries` (`0024:73-76`, `0027:66-69`, `0025:48-51`) are inert at runtime — consistent with the prior audit's finding. Isolation depends entirely on the app-layer `tenant_id` predicates, which are present. The RLS policies are therefore dead defense-in-depth. Flagged Info because it's a known, repo-wide condition; the app-layer scoping is correct.

---

## 7. Migrations & schema notes

- `stock_movements`: good indexes — `(tank_id, seq)` for the ledger read (`0024:61`), `(source_ref_type, source_ref_id)` partial for trace-back (`0024:63-64`), `(tenant_id)` (`0024:59`). `seq` IDENTITY gives a true append order independent of timestamp collisions (`0024:22-26`). CHECK constraints on type/source/status (`0024:42-51`). **Missing:** the one-opening-per-tank unique (INV-007) and any append-only guard (INV-002).
- `0026_sales_idempotency`: `uq_stock_mvt_sales_per_shift_tank ON (tank_id, source_ref_id) WHERE type='sales' AND src='shift'` (`0026:11-13`). Correct backstop. Note it omits `tenant_id` from the index, which is fine because `tank_id` (a PK) is globally unique. The TOCTOU in `SalesPostedForShift` (`sales.go:23-33`) is thus safe — a concurrent double-approve hits the unique and the *second* commit fails — **but** the resulting unique-violation error is not caught and would surface as a 500 to the user rather than a clean idempotent no-op (INV-023, Low).
- `tank_reconciliations`: `uq_tank_recon_tank_day` (`0027:50`) enforces one-per-(tank,day); `idx_tank_recon_sealed (tank_id, through_seq DESC) WHERE sealed` (`0027:57-58`) backs `LastSealedForTank`. Good.
- `numeric(14,3)` for litres / `numeric(10,4)` for variance_percent / `numeric(5,2)` for tolerance are the right column types — the defect is the Go side reads them as float (INV-001).

### INV-023 (Low) — Sales unique-violation surfaces as 500 instead of idempotent no-op under a concurrent double-approve
See above. The repo relies on `SalesPostedForShift` (`sales.go:46-52`) returning early, but the unique index is the real backstop; the index-violation path isn't handled. **Fix:** treat `23505` on `uq_stock_mvt_sales_per_shift_tank` as "already posted, no-op".

---

## 8. Dead code / unused

- **`transfer` movement type** (`repo.go:33`, CHECK `0024:43`, `requiresOpening` `:63`): no code ever posts a `transfer`. Dead until inter-tank transfers ship (INV-014 reference). *(Info)*
- **`ReverseMovement`** reachable only from tests (INV-006). *(Info)*
- **`MovingAverageCost`/`AverageLandedCostForStationProduct`** (`costing.go`) are Phase 6 cost-basis helpers; in scope only for litre discipline — they correctly use SQL numeric and return strings (`costing.go:19,38`), the *right* pattern litres should follow.

---

## Findings table

| ID | Severity | File:Line | Issue | Fix |
|---|---|---|---|---|
| INV-001 | Critical | inventory/repo.go:80-81,100; reconciliation/repo.go:37-51; reconciliation_handlers.go:45-47,174-177 | Litres & all recon figures carried as `float64`, violating numeric-string discipline; arithmetic & tolerance comparison done in Go float | Carry litres as decimal strings; do variance/tolerance math in SQL numeric |
| INV-002 | High | migrations/0024_stock_movements.up.sql (no trigger) | "Append-only" is convention only; `litres`/`balance_after`/`seq` freely UPDATE-able/DELETE-able at DB | Add BEFORE UPDATE trigger rejecting changes to litres/tank/type/seq; forbid DELETE |
| INV-003 | High | inventory/repo.go:145-159 | `balance_after` computed via unlocked SUM under Read Committed → concurrent posts to same tank produce inconsistent balance snapshots | `pg_advisory_xact_lock` per tank or `SELECT … FOR UPDATE` on the tank |
| INV-010 | High | reconciliation_handlers.go:157; inventory/periods.go:18 | `closingBook` adds `OpeningTotal`; any 2nd `opening`-type row (incl. INV-007 dup) double-counts opening | Exclude opening-type rows from period sum after anchor; assert single opening |
| INV-014 | High | reconciliation_handlers.go:309,516,535 | Compute runs on pool *before* the persist/seal tx → TOCTOU; seal write-off & figures from stale snapshot | Recompute inside the tx (as adjust path does); lock tank/day |
| INV-016 | High | reconciliation_handlers.go:533-559 | Balance-forward correctness depends on float-thresholded, pre-tx-computed seal write-off | In-tx numeric compute of write-off & sealed figures |
| INV-004 | Medium | inventory/sales.go:68-75; repo.go | Book balance can go negative; no non-negative/dead-stock guard | Flag negative book as exception at post time; document policy |
| INV-007 | Medium | inventory/repo.go:308-329 | Opening idempotency is TOCTOU with no unique index → concurrent double-open double-counts | Partial unique index on one posted opening per tank |
| INV-008 | Medium | inventory_handlers.go:176-178 | Opening of `litres=0` allowed and indistinguishable from genuine empty | Decide/enforce 0-opening policy |
| INV-011 | Medium | reconciliation_handlers.go:45-47,176 | variance% & tolerance both divide by `closingBook`; near-zero book trips false exceptions / blocks seal | Tolerance vs capacity or absolute litre floor |
| INV-012 | Medium | reconciliation_handlers.go:132,137 | Compute reads anchors from pool while period totals read tx → only partly tx-aware | Thread Querier through LastSealedForTank/OpeningBalance |
| INV-015 | Medium | reconciliation_handlers.go:516,561 | Seal doesn't re-verify shift-approval in-tx; same-day shifts can post after seal into next period | Re-run approval guard in seal tx; lock day |
| INV-017 | Medium | reconciliation_handlers.go:443-447,538 | Unbounded adjustment / write-off; no capacity or negative-book sanity bound | Bound adjustments; reject resulting book<0 or >capacity without override |
| INV-018 | Medium | inventory_overview_handler.go:95-139,251-268 | N+1: per-tank CurrentBalance/RecentForTank/AverageDailySales in a loop | Batch into GROUP BY / lateral queries |
| INV-005 | Low | inventory/repo.go:229-235 | `ReverseMovement` disambiguates via pool `GetMovement`, not tx → wrong error in-tx; extra round-trip | Disambiguate within tx in one query |
| INV-009 | Low | inventory/repo.go:290-303 | `OpeningBalance` `ORDER BY seq DESC LIMIT 1` masks duplicate-opening data bug | Surface/assert single opening |
| INV-013 | Low | inventory/periods.go:30-37; repo.go:244 | Period line-items include reversed originals+contras → misleading per-type totals (net is correct) | Net-of-reversal line items or surface reversals separately |
| INV-019 | Low | inventory/periods.go:58-73 | AverageDailySales divides by calendar days, keys on approval-time recorded_at, ignores status | Divide by days-with-sales; document |
| INV-020 | Low | inventory_overview_handler.go:117-123 | FillPercent/DaysOfStock unclamped → negative garbage on negative book | Clamp ≥0; warn |
| INV-021 | Low | reconciliation_handlers.go:596-623 | List-station-reconciliations doesn't 404 missing station / validate day-station (no leak) | Validate station & day like sibling handlers |
| INV-023 | Low | inventory/sales.go:46-52; 0026 | Concurrent double-approve → unique violation surfaces as 500, not idempotent no-op | Catch 23505 on the sales unique index as no-op |
| INV-006 | Info | inventory/repo.go:221-252 | `ReverseMovement` only reachable from tests; contra mechanism unexercised at API | Wire reversal route or mark deferred |
| INV-022 | Info | inventory_handlers.go:183; reconciliation_handlers.go:315 etc. | Writes use plain `DB.Begin`, never `WithTenant`; RLS GUC unset → RLS inert (app-layer scoping correct) | Adopt WithTenant for defense-in-depth |
| INV-014b | Info | repo.go:33; 0024:43 | `transfer` movement type defined but never produced | Remove or implement transfers |

## Severity counts

- **Critical:** 1 (INV-001)
- **High:** 5 (INV-002, INV-003, INV-010, INV-014, INV-016)
- **Medium:** 8 (INV-004, INV-007, INV-008, INV-011, INV-012, INV-015, INV-017, INV-018)
- **Low:** 7 (INV-005, INV-009, INV-013, INV-019, INV-020, INV-021, INV-023)
- **Info:** 3 (INV-006, INV-022, INV-014b/transfer)

## Top-5 risks

1. **INV-001 (Critical)** — Litres are float64 end-to-end; all variance/tolerance math is float. Systemic numeric-discipline violation that produces drift and class-flips on the tolerance boundary. The seal-write-off `0.0005` threshold is direct evidence the team is already papering over float residue.
2. **INV-014 (High)** — Compute-then-persist/seal is non-atomic (compute on pool, write in a later tx). The seal write-off and sealed figures are taken from a stale pre-tx snapshot, so a concurrent ledger change corrupts the sealed balance-forward anchor.
3. **INV-016 (High)** — Balance-forward integrity rests on a float-thresholded, pre-tx seal write-off; a wrong write-off cascades into every subsequent day's opening book.
4. **INV-002 + INV-003 (High)** — The ledger is "append-only" by convention only: no DB guard against rewriting `litres`, and `balance_after` is computed without a per-tank lock, so concurrent posts yield inconsistent (though sum-correct) running balances.
5. **INV-007 + INV-010 (Medium→High compound)** — Opening-balance idempotency is a TOCTOU with no unique-index backstop; a concurrent double-open creates two openings, and the `closingBook` formula then double-counts the opening, producing a phantom variance that masquerades as stock loss.
