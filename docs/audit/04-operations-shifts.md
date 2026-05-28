# Audit 04 — Operations & Shifts Domain

**Status:** READ-ONLY atomic-level audit. No source modified.
**Date:** 2026-05-28
**Auditor scope:** Operating-day lifecycle, shift lifecycle + approval/sales-posting trigger, attendant & nozzle assignment, meter readings, dip readings, shift close summary, cash submission, shift exceptions, incidents.

## Scope — files & LOC

Domain (`internal/`):

| File | LOC |
|---|---|
| `internal/operations/repo.go` (operating days) | 163 |
| `internal/operations/shifts.go` (shifts, attendants, nozzle assignments) | 365 |
| `internal/operations/close.go` (close lines, cash submission, tank-sales rollup) | 164 |
| `internal/operations/exceptions.go` | 114 |
| `internal/operations/myshift.go` (attendant console) | 71 |
| `internal/readings/repo.go` (meter readings) | 123 |
| `internal/readings/meter.go` (litres + scale math) | 41 |
| `internal/readings/meter_test.go` | 48 |
| `internal/readings/dips.go` (dip readings) | 209 |
| `internal/incidents/repo.go` | 181 |

Server handlers (`services/api/internal/server/`):

| File | LOC |
|---|---|
| `shifts_handlers.go` | 616 |
| `operating_days_handlers.go` | 363 |
| `meter_readings_handlers.go` | 323 |
| `dip_readings_handlers.go` | 309 |
| `shift_close_handlers.go` | 397 |
| `shift_exceptions_handlers.go` | 290 |
| `me_shift_handler.go` | 176 |
| `operations_overview_handler.go` | 165 |
| `incidents_handlers.go` | 241 |

Migrations read: `0013_pump_cal_and_incidents`, `0014_operating_days`, `0015_shifts`, `0016_meter_readings`, `0017_tank_dip_readings`, `0018_shift_close`, `0019_shift_exceptions`, `0020_attendant_perms`, `0021_reading_override_perms`, and the RBAC seed `0004_rbac`. Routing reviewed in `server.go` lines 762–817. Policy evaluator `internal/identity/policy/policy.go`, middleware `policy_middleware.go`. Sales-posting trigger target `internal/inventory/sales.go` and revenue trigger `revenue_handlers.go:52`.

---

## 1. Cross-cutting finding: money & litre discipline broken at the Go boundary (the big one)

House convention: money = `numeric(14,2)`, litres = `numeric(14,3)`, carried as decimal **strings**, arithmetic in SQL, **never float**. The DDL honours this — every value column is `numeric(14,3)`/`numeric(14,2)` (e.g. `0016_meter_readings.up.sql:14` `reading numeric(14,3)`, `0018_shift_close.up.sql:13-17`, `cash_submissions` all `numeric(14,2)`).

But **every Go struct in this domain scans those numerics into `float64`** and does the arithmetic in Go:

- `readings.MeterReading.Reading float64` (`internal/readings/repo.go:23`), `scanMeter` (`:52`) scans `numeric(14,3)` into a `float64`.
- `readings.DipReading.DipMM/VolumeLitres/WaterMM/TemperatureC float64` (`dips.go:21-23`).
- `operations.CloseLine` — `OpeningReading/ClosingReading/LitresSold/UnitPrice/ExpectedValue` all `float64` (`close.go:17-21`).
- `operations.CashSubmission` — `ExpectedCash/CashAmount/.../Variance float64` (`close.go:30-37`).
- `operations.TankSales.LitresSold float64` (`close.go:124`); `inventory.SaleLine.LitresSold float64` (`sales.go:16`).

The arithmetic then happens **in Go float**, not SQL:

- `readings.LitresDispensed` does `closing - opening` in `float64` (`meter.go:26`).
- `handleCloseShift` computes `LitresSold: litres` and `ExpectedValue: litres * nozzle.DefaultPrice` in Go (`shift_close_handlers.go:174-175`).
- `handleSubmitCash` computes `total := req.CashAmount + req.MobileMoneyAmount + req.CardAmount + req.CreditAmount` and `Variance: total - expected`, all float (`shift_close_handlers.go:328, 341`).
- `sumExpected` accumulates `total += lines[i].ExpectedValue` in float (`shift_close_handlers.go:69-75`), reused by the operations overview (`operations_overview_handler.go:135`) and `me_shift_handler.go:164`.
- `postShiftSales` forwards the float `LitresSold` into `inventory.PostMovement` with `Litres: -ln.LitresSold` (`shift_exceptions_handlers.go:145-148`, `sales.go:73`).

**Consequences.** A meter reading like `123456.789` litres, a unit price of `1.459`, or summing dozens of close lines accumulates IEEE-754 error. `expected_value` (the figure an attendant is held cash-accountable for) and `variance` (the shortage that triggers an exception and feeds finance) are float-derived, then written back into `numeric(14,2)` columns where Postgres silently rounds — so two code paths that "agree" can disagree by a cent, and the auto-raised `cash_variance` threshold comparison (`math.Abs(sub.Variance) > 1000.0`, `shift_close_handlers.go:369`) is evaluated on a lossy float. The `ValidateScale` precision guard (`meter.go:32-41`) is itself a float hack: it multiplies by `math.Pow10(dp)` and compares to a rounded value within `1e-6` — for large readings the epsilon is meaninglessly tight or loose. This is the single most material deviation in the domain and underpins findings OPS-001..OPS-004.

The correct shape, per house convention used elsewhere (inventory/revenue agents confirm SQL arithmetic), is to carry these as `string`/`pgtype.Numeric` and compute litres/value/variance in SQL (`closing - opening`, `litres * price`) inside the same statement that inserts the close line.

---

## 2. Operating-day lifecycle

State machine: `open → closed → locked`, plus `closed → open` (reopen). DDL check constraint `chk_operating_days_status CHECK (status IN ('open','closed','locked'))` (`0014:24`). One-non-locked-day-per-station enforced by partial unique index `idx_operating_days_active ON (station_id, business_date) WHERE status <> 'locked'` (`0014:42-43`). Good.

**Open** (`handleOpenOperatingDay`, `operating_days_handlers.go:75`): route-gated `operations.manage_day` at the URL station (`server.go:775`). Confirms the station exists for a clean 404 (`:104`), opens inside a tx, maps unique-violation → 409 (`:121`), audits via `WriteWithOutbox`, commits. Correct atomicity.

- **Timezone / business-date defect.** `businessDate := time.Now().UTC()` (`:91`). The "business date" is a calendar date stored in a `date` column; using UTC means a station in UTC+3 that opens its day at 01:00 local on the 15th records business_date = 14th. The one-open-day invariant and all downstream reconciliation key on this date. There is no per-tenant/station timezone. → **OPS-005**.

**Status update** (`handleUpdateOperatingDayStatus`, `:181`): authorizes `operations.manage_day` against the loaded day's station (`:213`) — good IDOR posture (id-based route, station from row). Guards: locked day can't change (`:217`), no-op rejected (`:221`), can't close while open shifts exist (`:226-235`). Re-read after `SetStatus ... RETURNING` is in the same tx (`:250`). Audit + commit correct.

- **Illegal-transition gap.** The only state guards are "not locked" and "not already X". There is no positive transition table. Because `req.Status` is constrained to `open|closed` (`:197`) and locked is rejected, the reachable transitions are open↔closed only, so it's *effectively* safe — but a `closed → closed` is caught only by the no-op guard, and there is no guard that a day being **reopened** has no posted/approved downstream facts. Reopening a `closed` day to `open` is allowed even after shifts in it were approved and sales posted; nothing un-posts. Combined with the locking rule (only `closed` days lock, and locking requires all shifts approved), an operator can reopen a fully-approved-but-unlocked day, open a new shift, and the day's prior approved sales remain posted while new activity accrues. Low-to-medium integrity risk depending on intended semantics. → **OPS-006**.

**Lock** (`handleLockOperatingDay`, `:284`): authorizes `operations.manage_day` on the row's station, requires `before.Status == "closed"` (`:315`), requires `UnapprovedShiftCountForDay == 0` (`:320`). `Lock` stamps `status='locked', locked_by, locked_at` (`repo.go:147`). Terminal & immutable: subsequent status changes are blocked by the locked guard above, and there is no unlock route. Lock immutability holds at the day level. **However** lock does not freeze child rows — meter/dip corrections are blocked only by `shift.Status` being open (see §5), and a locked day's shifts are all `approved`, so child writes are already blocked transitively. Acceptable.

- **TOCTOU on close/lock guards (race).** All guard counts (`OpenShiftCountForDay`, `UnapprovedShiftCountForDay`) run on the **pool, before** the tx begins (`:227`, `:320`), then the mutation runs in a later tx (`:243`). Two concurrent requests — close-day and open-a-new-shift — can interleave: the close reads zero open shifts, a shift opens, the close commits a closed day that now has an open shift. The `OpenShift` path only checks `day.Status != "open"` *also* before its own tx (`shifts_handlers.go:157`), so the symmetric race exists. No advisory lock or `SELECT ... FOR UPDATE` on the day row serialises these. → **OPS-007**.

## 3. Shift lifecycle & approval/posting trigger

State: `open → closed → approved` (`chk_shifts_status`, `0015:28`).

**Open** (`handleOpenShift`, `shifts_handlers.go:122`): route-gated `shift.open` at URL station (`server.go:786`). Loads the day, checks `day.StationID == stationID` (`:153`) and `day.Status == "open"` (`:157`). tx + audit + commit correct.

- The shift is FK-bound to the day via composite `(tenant_id, operating_day_id)` (`0015:32`), and the day must be open at open-time, so a shift can't be created under a locked/closed day at the moment of creation (modulo the OPS-007 race).

**Close** (`handleCloseShift`, `shift_close_handlers.go:81`): gated `shift.close` via `shiftForWrite(..., requireOpen=true)`. Rejects a shift with **zero** nozzle assignments (`:101`) — good (audit P2 fix). Validates every assigned nozzle has opening+closing meter and every tank has a closing dip, builds close lines, inserts them + flips to closed in **one tx** (`:189-226`). Audit fires. Correct atomicity and good validation coverage.

- **Race: validation outside the tx.** All reads (`ListNozzleAssignments`, `ListActiveForShift`, `ListDipsForShift`, the per-nozzle `nozzles.Get`) happen *before* `Begin` (`:93-187`). A concurrent meter correction (superseding/reinserting a reading) between validation and the `InsertCloseLine` loop produces a close line snapshot that doesn't match the now-active readings, with no re-check inside the tx. → **OPS-008**.
- **N+1 read.** Inside the assignment loop, `s.nozzles.Get(ctx, ..., nozzleID)` is called once per assignment (`:149`); the same per-nozzle `Get` recurs in the close-line build. A shift with N nozzles issues N round-trips for nozzle metadata. Minor at station scale but a real N+1. → **OPS-016**.
- **Unit price source.** `UnitPrice: nozzle.DefaultPrice` (`:174`). Expected value is valued at the nozzle's *default* price, not a time-effective price from the pricing module. If pricing changes intra-shift, expected cash uses the static default. May be intentional for Phase 3 (revenue recognition re-prices at approval via `recognizeShiftRevenue`), but the close-summary figure shown to the attendant/supervisor and the cash-variance exception are computed off `default_price`, which can diverge from the recognized revenue. → **OPS-017** (Info/Low — confirm intent).

**Approve** (`handleApproveShift`, `shift_exceptions_handlers.go:48`): gated `shift.approve` via `shiftForWrite(..., requireOpen=false)`; requires `before.Status == "closed"` (`:68`) and zero open exceptions (`:74-82`). Inside one tx: `ApproveShift` → `postShiftSales` → `recognizeShiftRevenue` → audit → commit. **The posting/revenue trigger wiring is correct and atomic**: sales-to-ledger and priced-revenue recognition commit in the same tx as the status flip, so a failure rolls all back.

- **Idempotency of the trigger.** `postShiftSales` is idempotent via `inventory.SalesPostedForShift` (`sales.go:23-33,46-52`) — re-running posts nothing if a posted `sales` movement for the shift exists. `recognizeShiftRevenue` returns a count and is only invoked from approval. Because re-approval is blocked (status must be `closed`, `:68`), the trigger fires at most once per normal lifecycle; the only re-entry is the documented inventory backfill (`sales.go:38-44`), which the idempotency check guards. Wiring is sound. (Posting *math* is out of scope per brief.)
- **Separation-of-duties: NONE.** Approval requires only `shift.approve` at the station; nothing checks that the approver is not an attendant on the shift, nor that the approver ≠ the closer/submitter. The `supervisor` role holds `shift.open + shift.close + shift.approve + reading.edit` (`0004:164-166`) and can also be added as an attendant (`AssignAttendant` accepts any tenant user). So one supervisor can open a shift, be its attendant, capture its readings, close it, submit its cash, and approve it — a complete self-approval loop with zero second pair of eyes. For a fuel-station cash-control system this is a material control weakness. → **OPS-002 (High)**.
- **Exception re-check is outside the tx.** `OpenExceptionCountForShift` runs on the pool before `Begin` (`:74`). A `cash_variance` exception auto-raised concurrently (via a late cash submission) between the count and the commit would not block this approval. Same TOCTOU class as OPS-007. → **OPS-009**.

**Reopen-after-approve.** There is no route to move `approved → closed/open`; once approved a shift is terminal at the handler layer. Good.

## 4. Attendant & nozzle assignment

**Assign attendant** (`handleAssignAttendant`, `shifts_handlers.go:373`): `shiftForWrite(..., "shift.assign", requireOpen=true)`. Unique-violation → 409 "already on this shift" (`:402`); FK-violation → 404 "user not found" (`:406`). PK `(shift_id, user_id)` enforces one row per user per shift (`0015:69`). tx + audit + commit correct.

- **No station-membership / role check on the assigned user.** Any tenant user id that satisfies the composite user FK can be made an attendant — including users with no station access or non-operational roles. The FK is `(tenant_id, user_id) → users` (`0015:73`), not scoped to the station's staff. Tenant isolation holds (cross-tenant user fails the composite FK), but cross-station assignment within a tenant is unconstrained. → **OPS-010 (Low/Medium)**.

**Assign nozzle** (`handleAssignNozzle`, `:487`): validates the nozzle belongs to the shift's station (`:518`) and the attendant is on the shift (`:523-531`). Unique index `uq_sna_shift_nozzle (shift_id, nozzle_id)` (`0015:112`) guarantees **one attendant per nozzle per shift**; the handler maps the violation → 409 (`:541`). Good — answers the "one nozzle to two attendants?" question: blocked at the DB.

- **Bug: unique-violation check precedes error nil-check incorrectly.** `assignment, err := AssignNozzle(...)` then `if isUniqueViolation(err) {...}` then `if err != nil {...}` (`:540-549`). This is fine logically, but note `AssignNozzle` returns `(nil, err)` on violation and the 409 path returns before dereferencing — OK. No defect; flagged only as reviewed.
- **One nozzle per attendant only.** Because the unique is on `(shift_id, nozzle_id)` a nozzle maps to exactly one attendant, but an attendant may hold many nozzles. Matches the model comment (`0015:87-88`). Correct.

**Unassign** (attendant `:432`, nozzle `:568`): both gated `shift.assign`, both delete inside a tx with audit, both map `ErrAssignmentNotFound` → 404. `ON DELETE CASCADE` on `shift_attendants` (`0015:71`) means **unassigning is a hard delete**, not a soft state — and unassigning an attendant does **not** cascade-remove their nozzle assignments (those FK to `users` via `sna_attendant_fk ON DELETE RESTRICT`, `0015:103-104`, and to the shift via CASCADE). So a nozzle can remain assigned to a user who is no longer an attendant on the shift; readings keyed to that nozzle/attendant would still validate. → **OPS-011 (Medium)**.

- **Tenant/station scoping** on all assignment repo methods is correct: every query filters `tenant_id` and `shift_id` (`shifts.go:196-365`). No IDOR observed on the assignment surface — the shift is always loaded tenant-scoped first via `shiftForWrite`.

## 5. Meter readings

DDL: append-only with supersede; partial unique `idx_meter_readings_active (shift_id, nozzle_id, reading_type) WHERE status='active'` (`0016:39-40`) → at most one active opening + one active closing per nozzle per shift. `chk_meter_readings_value CHECK (reading >= 0)`.

**Capture** (`handleCaptureMeterReading`, `meter_readings_handlers.go:127`): `shiftForScopedWrite("reading.edit","reading.override", requireOpen=true)` — attendants self-scoped to their assigned nozzles, supervisors override. Validates nozzle at shift's station (`:165`), nozzle assigned (`:169`), `ValidateScale` (`:172`), non-negative (`:142`). Unique-violation → 409 "correct it instead" (`:188`). tx + audit + commit correct. Good authZ layering.

**Correction** (`handleCorrectMeterReading`, `:221`): only while shift open (`:245`). Loads old, verifies `old.ShiftID == shift.ID` (`:260`), `old.Status == 'active'` (`:264`), nozzle assigned (`:268`), scale (`:277`). Then in one tx: `Supersede(old)` + `Capture(new, SupersedesID=&old.ID)` (`:289-300`). **The original is preserved** (status flipped to `superseded`, new row points back via `supersedes_id`) — audit trail intact, both old and new captured in the audit record (`:311`). Correct correction flow.

- **Monotonicity is NOT enforced at capture.** `closing ≥ opening` is checked *only* at close (`LitresDispensed` → `ErrMeterRollback`, `meter.go:22-27`; surfaced in `handleCloseShift:166-169` as `meter_rollback_nozzles`). An attendant can capture a closing below opening and it's stored happily; the error only surfaces when closing the shift, listing the nozzle as a rollback. The list endpoint silently *skips* rolled-back pairs (`meter_readings_handlers.go:106-110`) rather than flagging them, so the attendant gets no feedback until close fails. Acceptable as a design (correction is the recourse) but the silent skip hides the problem. → **OPS-012 (Low)**.
- **Meter rollover (wrap) is unhandled by design.** `meter.go:13-14` documents that a wrap is treated as an error, not modular arithmetic. Real mechanical pump meters wrap at a max (e.g. 8 digits). A legitimate wrap (`opening=99,999,990`, `closing=120`) is rejected as a rollback and *cannot be closed* without a correction that fabricates a non-wrapped closing — losing the true dispensed litres. No `meter_max`/rollover capacity on the nozzle. → **OPS-013 (Medium)**.
- **Correction does not re-validate against the sibling reading.** Correcting an opening to a value above the existing closing (or vice versa) is allowed; the inconsistency only surfaces at close. Same class as OPS-012.
- **No correction once closed/locked.** Both capture and correct require `shift.Status == open` via `shiftForScopedWrite(requireOpen=true)`. Once closed, the close-line snapshot is frozen and readings can't change — correct (audit P1). Good.

Decimal places: stored `numeric(14,3)` (3 dp) but validated against the **nozzle's** `meter_decimal_places` (`:172`), which can be 0–3. Reasonable, but the validation runs in float (see OPS-001).

`ListActiveForShift` orders `nozzle_id, reading_type` so opening sorts before closing alphabetically ('closing' < 'opening' — actually **'closing' sorts before 'opening'**). The comment at `repo.go:59-60` claims "opening precedes closing"; alphabetically `closing` < `opening`, so the order is closing-then-opening. The handlers don't rely on order (they bucket by type into a map), so harmless, but the comment is wrong. → **OPS-018 (Info)**.

## 6. Dip readings

DDL `0017`: `dip_mm/volume_litres/water_mm numeric(14,3)`, `temperature_c numeric(6,2)`, `chart_id` FK NOT NULL, snapshotted volume + chart. Partial unique `idx_tank_dip_active (shift_id, tank_id, reading_type) WHERE status='active'`. Checks for non-negative dip/volume/water. Good immutable-history design.

**Capture** (`handleCaptureDipReading`, `dip_readings_handlers.go:114`): scoped write, tank at shift's station (`:152`), tank assigned via `requireTankAssigned` (`:156`), then `resolveDipVolume` looks the dip up against the active calibration chart (`:160`, `:94-112`) — maps `ErrNoActiveChart`→409, `ErrOutOfRange`→422, `ErrEmptyChart`→422. Volume + chart_id snapshotted at capture (`:172-176`). tx + audit + commit correct. Water level and temperature captured as optional (`:88-89`). Good.

**Correction** (`handleCorrectDipReading`, `:212`): mirrors meter correction — supersede + re-capture with `SupersedesID`, both in one tx, original preserved, audit carries old+new (`:273-300`). Re-resolves volume against the chart for the new dip (`:261`). Correct.

- **No water-level / dip-vs-tank-capacity validation beyond chart range.** Water_mm is stored but never used in any computation or flagged (e.g. high water → contamination exception). The dip is range-checked only against the chart, not the tank's physical capacity. Low priority. → **OPS-019 (Info)**.
- **`FirstDipForTank`** (`dips.go:97`) orders `recorded_at, created_at` to find the opening physical level for the stock ledger — correct, returns earliest active dip. `ClosingDipForTankDay` joins shifts on `(id, tenant_id)` and filters by operating day (`dips.go:145-159`) — tenant-scoped, correct. `LatestDipsForStation` uses `DISTINCT ON (tank_id) ... ORDER BY tank_id, recorded_at DESC` (`dips.go:185-195`) — correct per-tank latest. These are consumed by the inventory/reconciliation agents; wiring here is sound.
- **Monotonicity not relevant for dips** (level rises with deliveries, falls with sales) — correctly no closing≥opening check. Good.

## 7. Shift close summary & cash

`handleCloseSummary` (`shift_close_handlers.go:237`): gated `station.read` on the shift's station. Lists close lines, sums expected, attaches cash submission if present. Read-only; correct.

`handleSubmitCash` (`:294`): scoped write `cash.submit`/`cash.override`, requires shift **closed** (`:316`). Validates non-negative tenders (`:305`). Recomputes expected from close lines (`:322-327`), total + variance in Go float (OPS-001). Inserts submission (unique per shift → 409 on dup, `:344`), audits, and if `|variance| > 1000` auto-raises a `cash_variance` exception **in the same tx** with its own audit record (`:369-390`), then commits. Atomicity correct; the exception correctly blocks later approval.

- **Hard-coded variance threshold** `cashVarianceThreshold = 1000.0` (`:26`), acknowledged as future work in the comment. The threshold is tenant/currency-agnostic — 1000 units means very different things across tenants. → **OPS-014 (Low)**.
- **Cash submission cannot be corrected.** Unique `(shift_id)` (`0018:57`) + insert-only repo (`InsertCashSubmission`, `close.go:105`); no update/resubmit path. If an attendant fat-fingers a tender, the only recourse is... none at this layer (no delete, no update handler). A supervisor with `cash.override` also hits the 409. → **OPS-015 (Medium)**.
- **Cash submission allowed by attendant after self-scope passes but no "is it my shift to submit" beyond attendant-on-shift.** `shiftForScopedWrite(..., false)` requires the attendant be on the shift (`:253-261`) — fine. But it does not require the submitter be the *closer*; combined with no SoD (OPS-002) this is part of the same control gap.

## 8. Shift exceptions

DDL `0019`: types constrained (`missing_reading, cash_variance, meter_rollback, late_close, other`), severity + status (`open, resolved`) checked. An open exception blocks approval.

- **Raised only programmatically** — the sole caller of `RaiseException` in scope is the cash-variance path (`shift_close_handlers.go:370`). Despite the close handler computing `meter_rollback_nozzles` and `missing_*` lists (`:179-187`), **no `meter_rollback` or `missing_reading` exception is ever raised** — those conditions instead hard-block the close with a 422. So three of the five exception types are dead enum values with no producer. → **OPS-020 (Info/Low)**.
- **Resolve** (`handleResolveShiftException`, `shift_exceptions_handlers.go:216`): gated `shift.approve` on the exception's shift station — meaning whoever can approve can resolve. `ResolveException` flips `open→resolved` only where `status='open'` (`exceptions.go:87-95`), stamping `resolved_by/at`. tx + audit + commit correct. Already-resolved → 409 (`:265`). Reasonable.
- **SoD also absent here**: the same person who triggered the variance (by submitting cash) can, if they hold `shift.approve`, resolve their own exception and approve — see OPS-002.
- **No reopen / no `dismissed` state.** Resolution is terminal. Acceptable.

## 9. Incidents

DDL `0013`: polymorphic `related_entity_type/id` (no FK — loose pointer, documented `0013:45`), type/severity/status checked, `open → investigating → resolved → closed`.

- **List** (`handleListIncidents`, `incidents_handlers.go:49`): uses `stationReadFilter` to scope to the actor's readable stations (`:55`) — correct tenant+station isolation; a restricted actor can't read incidents outside scope. Status filter validated against `incidentStatuses` (`:61`); **severity filter is NOT validated** (`:67-69`) — an arbitrary `?severity=` is passed straight to the repo (`repo.go:79`), which just yields zero rows. Minor inconsistency, no injection (parameterized). → **OPS-021 (Info)**.
- **Create** (`:92`): gated `incidents.manage` at the body's `station_id` via in-handler `authorizeStation` (`:108`) — the route itself is only under the `station.read` group (`server.go:764-766`), so write authZ relies entirely on the in-handler check. That check is present and correct. Confirms station exists (`:113`), tx + audit + commit. Defaults type→`other`, severity→`medium` in the repo (`repo.go:136-143`).
- **Update status** (`:163`): loads tenant-scoped, authorizes `incidents.manage` on the row's station (`:195`), `UpdateStatus` stamps/clears `resolved_*` via `CASE WHEN resolving` (`repo.go:162-173`). Re-read in same tx after RETURNING. Audit distinguishes `incident.resolved` vs `incident.updated` correctly (`:219-223`).
- **No transition guard.** Any status → any status is allowed (e.g. `closed → open`, `resolved → investigating`). For an issue queue this is arguably fine, but there's no monotonic lifecycle enforcement. Low. → **OPS-022 (Info)**.
- **Tenant isolation** is solid across incidents — every query filters `tenant_id`; `List` uses `database.UUIDStrings(f.StationIDs)` parameterized array (`repo.go:81`). No IDOR.

## 10. Tenant isolation & IDOR (domain-wide)

Every repo method takes `tenantID` first and filters on it (house convention honoured). All id-based handlers load the entity tenant-scoped, then authorize against the row's station — the correct pattern that defeats IDOR. Composite tenant FKs in the DDL prevent cross-tenant references. RLS policies exist on every table but are noted inert at runtime (prior audit); app-layer scoping is the real control and is consistently applied. **No IDOR or cross-tenant read/write found** in this domain. The one soft spot is intra-tenant cross-station attendant assignment (OPS-010).

## 11. me/shift attendant console

`handleMyActiveShift` (`me_shift_handler.go:50`): gated by authentication only (`server.go:196`) — correct, since `ActiveShiftForAttendant` (`myshift.go:26`) only ever returns a shift the actor is an attendant on (`JOIN shift_attendants a ON a.shift_id = s.id WHERE a.user_id = $2`). Denormalized nozzle/dip detail so an attendant needs no station-read. Reads only; no mutation. Sound self-scoping.

- **`ActiveShiftForAttendant` join lacks an explicit tenant predicate on the join condition.** Line `myshift.go:33`: `JOIN shift_attendants a ON a.shift_id = s.id` — joins on shift_id only, with `WHERE s.tenant_id = $1`. Since `s.tenant_id` is filtered and shift_id is a global PK, the attendant row is unambiguously tied; but the join condition itself doesn't carry `a.tenant_id = s.tenant_id` the way `dips.go:150` does (`ON sh.id = d.shift_id AND sh.tenant_id = d.tenant_id`). Defense-in-depth inconsistency, not an exploitable hole (shift_id PK is unique). → **OPS-023 (Info)**.

---

## Findings

| ID | Severity | File:Line | Issue | Fix |
|---|---|---|---|---|
| OPS-001 | High | `internal/readings/repo.go:23,52`; `dips.go:21-23`; `operations/close.go:17-37`; `meter.go:22-41` | Money/litre values scanned into `float64` and arithmetic done in Go float, violating the decimal-string/SQL-arithmetic house rule. DB columns are `numeric(14,2/3)` but every domain struct round-trips through float. | Carry as `string`/`pgtype.Numeric`; compute litres (`closing-opening`), expected value (`litres*price`), totals and variance in SQL inside the insert. |
| OPS-002 | High | `shift_exceptions_handlers.go:48-128`; `0004_rbac.up.sql:164-166` | No separation of duties: a `supervisor` (or anyone with `shift.approve`) can open, attend, read, close, submit cash for, raise/resolve the exception on, and approve the same shift. Self-approval loop with no second pair of eyes on cash. | Add an SoD guard at approval (approver ≠ closer ≠ cash submitter, and approver not an attendant on the shift); make `shift.approve` exclusive of attendant assignment, or require a distinct role. |
| OPS-008 | High | `shift_close_handlers.go:93-226` | Close validates readings/assignments on the pool, then snapshots close lines in a later tx. A concurrent meter/dip correction between validation and insert produces a close-line snapshot inconsistent with active readings. | Re-read active readings inside the tx (or `SELECT ... FOR UPDATE` the shift) and recompute lines transactionally. |
| OPS-007 | Medium | `operating_days_handlers.go:227,243`; `shifts_handlers.go:157` | TOCTOU race closing/locking a day vs. opening a shift: guard counts run before the tx; a shift can open after the open-shift count reads zero, so a closed day can end up with an open shift. | Take a row lock on the operating day (`SELECT ... FOR UPDATE`) inside the tx before re-checking the guard; open-shift must lock+re-check the day status in-tx too. |
| OPS-009 | Medium | `shift_exceptions_handlers.go:74-122` | Open-exception count checked before the approval tx; a concurrently auto-raised `cash_variance` exception can be bypassed. | Move the exception count inside the tx (and lock the shift). |
| OPS-011 | Medium | `internal/operations/shifts.go:258-269`; `0015:103-104` | Unassigning an attendant does not remove their nozzle assignments (attendant FK is RESTRICT to users, cascade only on shift). A nozzle can stay assigned to a non-attendant; readings keyed to that nozzle still validate. | On unassign-attendant, also delete that attendant's `shift_nozzle_assignments` in the same tx; or block unassign while they hold nozzles. |
| OPS-013 | Medium | `internal/readings/meter.go:13-27` | Meter rollover (wrap) treated as a hard error; a legitimate wrap can't be closed without fabricating a closing reading, losing true litres. No `meter_max` on the nozzle. | Add nozzle meter capacity/digits; when `closing < opening` and a wrap is plausible, compute `meter_max - opening + closing` (in SQL), or require an explicit operator-confirmed rollover flag. |
| OPS-015 | Medium | `shift_close_handlers.go:294-396`; `0018:57` | Cash submission is insert-only with a unique `(shift_id)`; no correct/resubmit path. A mistyped tender is unfixable at this layer. | Add a supervisor-gated correction (supersede prior submission, recompute variance, re-evaluate exception) inside a tx. |
| OPS-005 | Medium | `operating_days_handlers.go:91` | Business date defaults to `time.Now().UTC()`; stations east/west of UTC bucket cross-midnight work on the wrong calendar date, corrupting the one-open-day invariant and downstream reconciliation. | Resolve business date in the station's/tenant's timezone. |
| OPS-006 | Low | `operating_days_handlers.go:181-278`; `internal/operations/repo.go:125` | No positive transition table; a `closed` day with approved shifts and posted sales can be reopened, with no un-posting of prior facts. | Add an explicit transition guard; forbid reopening a day whose shifts are approved/posted (or define & document the reopen semantics + compensation). |
| OPS-010 | Low | `shifts_handlers.go:373-413`; `0015:73` | Any tenant user can be assigned as an attendant; no station-membership/role check (FK is to users, not station staff). Intra-tenant cross-station assignment unconstrained. | Validate the user has station access / an operational role before assigning. |
| OPS-012 | Low | `internal/readings/meter.go:22-27`; `meter_readings_handlers.go:106-110` | Closing<opening accepted at capture; only surfaces at close. List endpoint silently skips rolled-back pairs, giving the attendant no feedback. | Flag rollback in the list response; optionally raise a `meter_rollback` exception at capture. |
| OPS-014 | Low | `shift_close_handlers.go:26` | `cashVarianceThreshold = 1000.0` hard-coded, tenant/currency-agnostic. | Make per-station/tenant configurable. |
| OPS-016 | Low | `shift_close_handlers.go:149` | N+1: `nozzles.Get` per assignment inside the close loop. | Batch-load nozzles for the shift's assignments once. |
| OPS-017 | Low | `shift_close_handlers.go:174` | Expected value valued at `nozzle.DefaultPrice`, not a time-effective price; can diverge from recognized revenue. | Confirm intent; if pricing is time-effective, resolve the price at close. |
| OPS-020 | Low | `internal/operations/exceptions.go:42`; `0019:22-24` | `meter_rollback`/`missing_reading`/`late_close` exception types have no producer; only `cash_variance` is ever raised. | Either raise the corresponding exceptions or trim the enum. |
| OPS-018 | Info | `internal/readings/repo.go:59-60` | Comment claims "opening precedes closing" but alphabetic order yields closing-first; harmless (handlers bucket by type). | Fix the comment or order explicitly. |
| OPS-019 | Info | `dip_readings_handlers.go:88-89`; `dips.go` | Water level/temperature captured but never validated or used (e.g. high-water contamination flag). | Add water/temperature validation or a contamination exception if in scope. |
| OPS-021 | Info | `incidents_handlers.go:67-69` | Severity list filter unvalidated (status filter is validated). | Validate severity against an allowed set for consistent 400s. |
| OPS-022 | Info | `incidents_handlers.go:163-182`; `internal/incidents/repo.go:162` | Incident status allows any→any transition (no lifecycle guard). | Add a transition table if a monotonic lifecycle is intended. |
| OPS-023 | Info | `internal/operations/myshift.go:33` | `ActiveShiftForAttendant` join condition omits `a.tenant_id = s.tenant_id` (defense-in-depth; not exploitable as shift_id is a PK). | Add the tenant predicate to the join for consistency with `dips.go:150`. |

## Severity counts

- **Critical:** 0
- **High:** 3 (OPS-001, OPS-002, OPS-008)
- **Medium:** 6 (OPS-005, OPS-007, OPS-009, OPS-011, OPS-013, OPS-015)
- **Low:** 6 (OPS-006, OPS-010, OPS-012, OPS-014, OPS-016, OPS-017, OPS-020) *(7 items)*
- **Info:** 5 (OPS-018, OPS-019, OPS-021, OPS-022, OPS-023)

(Total findings: 21.)

## Top 5 risks

1. **OPS-002 (High) — No separation of duties on shift approval.** `shift_exceptions_handlers.go:48`. One supervisor can self-attend, close, submit cash, resolve the variance exception, and approve the same shift — defeating the entire cash-control purpose of the approve step in a money-handling system.
2. **OPS-001 (High) — Float arithmetic for money & litres.** `internal/readings/repo.go:52`, `operations/close.go`, `shift_close_handlers.go:174,328,341`. Expected cash, litres sold, and variance are computed in IEEE-754 float then stored to `numeric`, producing rounding drift across code paths and a lossy variance-threshold comparison — the headline house-convention breach.
3. **OPS-008 (High) — Close snapshot built outside the transaction.** `shift_close_handlers.go:93-226`. A concurrent reading correction during close yields frozen close lines that don't match the active readings, mis-stating expected cash on an immutable record.
4. **OPS-013 (Medium) — Meter rollover unhandled.** `internal/readings/meter.go:13-27`. A real meter wrap is treated as an error and cannot be closed without fabricating a reading, losing true dispensed litres and corrupting sales/inventory.
5. **OPS-007 / OPS-009 (Medium) — Day-close & approval guard TOCTOU.** `operating_days_handlers.go:227`, `shift_exceptions_handlers.go:74`. Guard checks run before their transactions with no row lock, so a closed day can acquire an open shift and an approval can bypass a concurrently-raised cash-variance exception.
