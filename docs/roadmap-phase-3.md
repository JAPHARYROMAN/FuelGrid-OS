# Phase 3 — Station Operations Core

The phase where FuelGrid OS starts **recording what actually happens** at a station. Phase 1 built the platform; Phase 2 built the static infrastructure catalog (products, tanks, pumps, nozzles, calibration). Phase 3 adds the **temporal operating layer** on top: operating days, shifts, attendant assignments, and the meter + dip readings that capture every litre moved and every shilling collected.

When this phase is done, a station can open a day, run shifts, assign attendants to nozzles, capture opening/closing meter and dip readings, close a shift with a cash reconciliation, and have a supervisor approve it — every step audited and emitted to the outbox.

This is the layer that **produces the numbers** later phases trust. Phase 3 captures readings and computes shift-level figures; it does **not** post to a stock ledger (Phase 4) or price/settle sales (Phase 6). It stays in the operator's mental model: millimetres on a dipstick, litres on a meter, cash in a drawer.

## Stack decisions (carried forward from Phases 1–2)

All Phase-3 work continues to ride the patterns locked in earlier:

| Concern | Continued choice |
|---|---|
| Backend transactions | One tx wraps business change + audit + outbox (Stage-7 helper) |
| Tenant scoping | Every repo query takes `tenantID` first; RLS is the safety net |
| Tenant-bound FKs | Children carry `(tenant_id, …)` composite FKs onto parent unique keys |
| Authorization | `requirePermission(code, scopeExtractor)` for URL-scoped, `authorizeStation(...)` in-handler when the station comes from the body/row, `requirePermissionHeld(code)` for tenant-wide reads |
| Migrations | One concern per file; system permissions seeded inline |
| Numeric precision | Litres `numeric(14, 3)`; money `numeric(14, 2)` |
| Frontend | shadcn-style primitives in `@fuelgrid/ui` + Radix Dialog + react-hook-form; TanStack Query over a hand-written `@fuelgrid/sdk` |

New conventions specific to Phase 3:

| Concern | Convention |
|---|---|
| State machines | `operating_day`: `open → closed → locked`. `shift`: `open → closed → approved`. Every transition writes audit + outbox. |
| Reading precision | A pump meter reading is validated against the nozzle's `meter_decimal_places` before it is accepted; a mismatched scale is rejected, not silently rounded. |
| Reading immutability | Readings are append-only. A correction creates a new reading that supersedes the prior one (gated by `reading.edit`); there is no silent `UPDATE` of a captured reading. |
| Dip → volume | Opening/closing dips resolve to litres via the Phase-2 calibrated-volume lookup **at capture time**, and the resolved volume is snapshotted on the reading row (so a later re-strap can't rewrite history). |
| Litres sold | Per nozzle = `closing_meter − opening_meter`. Never computed if either reading is missing. |
| Expected cash | Computed from litres sold × the nozzle's `default_price` at capture time. This is an operational estimate; the real pricing engine lands in Phase 6 and will swap the price source without changing the shift-close shape. |

---

## Category A — Operating cadence

The temporal skeleton: when work happens. Pure lifecycle + assignment; no readings yet.

### Stage 1 — Operating days

**Goal:** A station's work is bucketed into operating days that can be opened, closed, and locked, so every later record (shift, reading, cash) hangs off a known business date.

- [x] Migration `0014_operating_days`: `operating_days` (id, tenant_id, station_id, business_date, status, opened_by, opened_at, closed_by, closed_at, locked_by, locked_at, notes, timestamps)
- [x] CHECK: `status IN ('open', 'closed', 'locked')`; partial unique index — at most one non-locked day per `(station_id, business_date)`
- [x] Composite tenant FKs to `stations` and `users`; expose `uq_operating_days_tenant_id (tenant_id, id)` as the FK target for shifts
- [x] Permission `operations.manage_day` (station-scoped) — open/close/lock a day.
- [x] Repo + handlers + SDK: list, get, open, `PATCH …/status` (close/reopen), `PATCH …/lock`
- [x] Guard: a day can't be **closed** while it has open shifts; can't be **locked** until every shift is approved *(wired in Stage 2 once the shifts table existed)*
- [x] Audit + outbox: `operating_day.opened`, `operating_day.closed`, `operating_day.locked`
- [x] Seed: an `open` operating day for `MIK-01` on the demo tenant's "today"

**Done when:** `POST /api/v1/stations/{id}/operating-days` opens a day; closing it is rejected (409) while a shift is open, and locking is rejected until all shifts are approved. *(Day open/close/lock + locked-terminal verified live; the shift-dependent guards land in Stage 2.)*

---

### Stage 2 — Shifts & assignments

**Goal:** Within an open day, supervisors open shifts and assign attendants to the nozzles they'll operate, so every reading and sale traces to a person.

- [x] Migration `0015_shifts`:
  - `shifts` (id, tenant_id, station_id, operating_day_id, name, status, opened_by, opened_at, closed_by, closed_at, approved_by, approved_at, notes, timestamps)
  - `shift_attendants` (shift_id, user_id, tenant_id, assigned_by, assigned_at) — who is on this shift
  - `shift_nozzle_assignments` (id, tenant_id, shift_id, nozzle_id, attendant_id, assigned_at) — which attendant runs which nozzle
  - Composite tenant FKs throughout; `shift.operating_day_id → operating_days(tenant_id, id)`, `nozzle_id → nozzles`, `attendant_id → users`
  - CHECK: `shift.status IN ('open', 'closed', 'approved')`
- [x] Permissions: reuse `shift.open`, `shift.close`, `shift.approve`. Add `shift.assign` (station-scoped) for managing attendant/nozzle assignments.
- [x] Repo + handlers + SDK: list shifts (by day/station), get, open, close, assign/unassign attendant, assign/unassign nozzle
- [x] Guards: a shift can only open inside an `open` day; a nozzle can be assigned to at most one attendant per shift; assignments are frozen once the shift is `closed`
- [x] Also wires the deferred Stage-1 day guards: a day can't close with open shifts; can't lock until every shift is approved
- [x] `/stations/{id}` dashboard gains a **Shifts** strip (active shift, attendants, assignment summary)
- [x] Audit + outbox: `shift.opened`, `shift.closed`, `shift.attendant_assigned`, `shift.nozzle_assigned`
- [x] Seed: one `open` shift on the demo day with the demo operator assigned to `MIK-01`'s PMS nozzles

**Done when:** A supervisor opens a shift in the seeded day, assigns the demo operator to two nozzles, and the assignments show on the station dashboard; opening a shift in a `closed` day returns 409. *(All verified live.)*

---

## Category B — Field readings

What the meters and dips actually say. These stages consume Phase 2's calibration charts and meter-precision config.

### Stage 3 — Pump meter readings

**Goal:** Every nozzle's opening and closing meter is captured per shift, validated against the nozzle's configured precision, and turned into litres dispensed.

- [x] Migration `0016_meter_readings`: `meter_readings` (id, tenant_id, shift_id, nozzle_id, reading_type, reading, recorded_by, recorded_at, supersedes_id, status, timestamps)
  - `reading_type IN ('opening', 'closing')`; `reading numeric(14, 3)`
  - Partial unique: at most one active opening and one active closing per `(shift_id, nozzle_id)`
  - Composite tenant FKs to `shifts` and `nozzles`; `supersedes_id` self-FK for corrections
- [x] Reuse permission `reading.edit` (station-scoped) for capture and correction
- [x] `internal/readings` package: `LitresDispensed(opening, closing) (litres, error)` (rejects closing < opening — meter rollover handled explicitly), and a precision validator against `nozzle.meter_decimal_places`
- [x] Endpoints:
  - `POST /api/v1/shifts/{id}/meter-readings` — capture an opening/closing reading (precision-validated)
  - `GET /api/v1/shifts/{id}/meter-readings` — list, with computed litres per nozzle where both ends exist
  - `POST …/meter-readings/{id}/correct` — supersede a reading (audited)
- [x] Reject (422) a reading whose decimal scale doesn't match the nozzle's `meter_decimal_places`
- [x] Audit + outbox: `meter_reading.captured`, `meter_reading.corrected`
- [x] Seed: opening meter readings for the demo shift's assigned nozzles

**Done when:** Capturing a closing reading with the wrong decimal scale is rejected 422; a valid opening+closing pair yields `litres_dispensed = closing − opening` in the list response. *(All verified live; correction supersedes + recomputes.)*

---

### Stage 4 — Tank dip readings

**Goal:** Opening and closing tank dips are captured in millimetres and resolved to litres via the calibration chart, with water and temperature recorded alongside.

- [x] Migration `0017_tank_dip_readings`: `tank_dip_readings` (id, tenant_id, shift_id, tank_id, reading_type, dip_mm, volume_litres, water_mm, temperature_c, chart_id, recorded_by, recorded_at, supersedes_id, status, timestamps)
  - `reading_type IN ('opening', 'closing')`; volume + dip `numeric(14, 3)`
  - Partial unique: at most one active opening and one active closing per `(shift_id, tank_id)`
  - Composite tenant FKs to `shifts` and `tanks`; `chart_id` records which calibration chart resolved the volume
- [x] Reuse permission `reading.edit` (station-scoped)
- [x] Capture path calls the Phase-2 `calibration.Lookup(tankID, dip_mm)` and **snapshots** the resolved `volume_litres` + `chart_id` on the row (a later re-strap leaves history intact)
- [x] Endpoints:
  - `POST /api/v1/shifts/{id}/dip-readings` — capture an opening/closing dip (volume resolved server-side)
  - `GET /api/v1/shifts/{id}/dip-readings` — list with resolved volumes
  - `POST …/dip-readings/{id}/correct` — supersede (audited)
- [x] Reject (422) a dip outside the active chart's range; 409 if the tank has no active chart
- [x] Tank visual on `/stations/{id}` reads the latest dip's `volume_litres` to render a **real fill level** (replacing the Phase-2 "awaiting reading" placeholder)
- [x] Audit + outbox: `dip_reading.captured`, `dip_reading.corrected`
- [x] Seed: an opening dip for `MIK-01`'s PMS tank that resolves against the seeded 51-point chart

**Done when:** Capturing a dip of 1240 mm on the PMS tank stores `volume_litres ≈ 12400` (via the calibration API) and the station dashboard's tank visual animates to that level. *(API capture 1240 → 12400 and overview current_litres=12400 verified live; the visual is fed that value — animation not browser-confirmed in this environment.)*

---

## Category C — Close & reconciliation

Turning raw readings into trusted, signed-off shift numbers.

### Stage 5 — Shift close & cash reconciliation

**Goal:** Closing a shift computes litres sold, expected cash, and the variance against the cash the attendant actually submits.

- [x] Migration `0018_shift_close`:
  - `cash_submissions` (id, tenant_id, shift_id, expected_cash, cash_amount, mobile_money_amount, card_amount, credit_amount, submitted_total, variance, submitted_by, submitted_at, notes, timestamps) — money `numeric(14, 2)`
  - `shift_close_lines` (id, tenant_id, shift_id, nozzle_id, opening_reading, closing_reading, litres_sold, unit_price, expected_value) — the per-nozzle snapshot frozen at close
- [x] Permission `cash.submit` (station-scoped) for attendants; close stays on `shift.close`
- [x] Close handler (one tx): require a closing meter + dip reading for every assigned nozzle/tank; snapshot per-nozzle litres × `default_price` into `shift_close_lines`; sum `expected_cash`; flip shift to `closed`
- [x] Cash submission handler: record the tender breakdown, compute `submitted_total` and `variance = submitted_total − expected_cash` (shortage/excess)
- [x] Endpoints: `POST /api/v1/shifts/{id}/close`, `POST /api/v1/shifts/{id}/cash-submission`, `GET /api/v1/shifts/{id}/close-summary`
- [x] Guard: close is rejected (422) listing exactly which nozzles/tanks are missing a closing reading
- [x] Audit + outbox: `shift.closed` (with the close snapshot), `cash.submitted`
- [x] Seed: leave the demo shift `open` so the flow is exercisable end-to-end in the UI

**Done when:** Closing the seeded shift after capturing closing readings produces a `close-summary` with per-nozzle litres sold and an `expected_cash`; submitting cash records `variance` = submitted − expected. *(All verified live; missing-reading close 422s with the exact lists.)*

---

### Stage 6 — Approval, exceptions & day close

**Goal:** A supervisor reviews and approves closed shifts; anomalies surface as exceptions; once every shift is approved the day is closed and locked.

- [ ] Migration `0019_shift_exceptions`: `shift_exceptions` (id, tenant_id, shift_id, type, severity, detail, status, raised_at, resolved_by, resolved_at, timestamps)
  - `type IN ('missing_reading', 'cash_variance', 'meter_rollback', 'late_close', 'other')`
- [ ] Permissions: reuse `shift.approve`; reuse `operations.manage_day` (Stage 1) for day close/lock
- [ ] Approval handler (one tx): flips `closed → approved`, stamps `approved_by/at`; refuses approval while unresolved blocking exceptions remain
- [ ] Auto-raise exceptions at close: cash variance over a station threshold, a closing meter below opening (rollback), or a missing reading that was force-overridden
- [ ] Endpoints: `PATCH /api/v1/shifts/{id}/status` (approve), `GET /api/v1/shifts/{id}/exceptions`, `PATCH /api/v1/shift-exceptions/{id}/status`, `PATCH …/operating-days/{id}/lock`
- [ ] Audit + outbox: `shift.approved`, `shift_exception.raised`, `shift_exception.resolved`, `operating_day.locked`
- [ ] Day-close guard: locking refuses (409) until all of the day's shifts are `approved`

**Done when:** Approving the seeded shift moves it to `approved`; a deliberately large cash variance raises a `cash_variance` exception that blocks approval until resolved; locking the day succeeds only once every shift is approved.

---

## Category D — Operator surfaces

The daily UX for the people actually running the forecourt.

### Stage 7 — Attendant shift console

**Goal:** An attendant opens a dead-simple, mobile-first "My Shift" screen and does only what they need: see assigned nozzles, enter readings, submit cash.

- [ ] Route `/my-shift` (mobile-first, single column): current shift, assigned nozzles, opening/closing reading entry, cash submission form, shift status
- [ ] Big-touch-target reading inputs that enforce the nozzle's decimal precision client-side (server still authoritative)
- [ ] "Submit cash" flow with the tender breakdown and a live expected-vs-submitted preview
- [ ] Strictly scoped: an attendant sees only their own assignments and only `shift.open`/`reading.edit`/`cash.submit` actions (no admin surfaces)
- [ ] Empty/locked states: "no active shift", "shift already closed", "awaiting approval"

**Done when:** The demo operator logs in, sees only their assigned `MIK-01` nozzles, enters readings, and submits cash from a phone-width screen — with no access to settings or other stations.

---

### Stage 8 — Supervisor operations dashboard

**Goal:** A supervisor opens one screen and runs the day: active shifts, who's assigned where, pending approvals, expected-vs-submitted cash, and open exceptions.

- [ ] Route `/operations` (or a tab on `/stations/{id}`): active-day summary, per-shift cards (attendants, litres sold, cash status), approval actions, exceptions queue
- [ ] Backend: `GET /api/v1/stations/{id}/operations-overview` — the day + its shifts (each with assignments, close summary, cash status, exception count) in one call (avoids N+1)
- [ ] Inline approve / open-exception / resolve-exception actions wired to Stage 5–6 endpoints
- [ ] Permission gate: `station.read` to view, `shift.approve` for the approve action
- [ ] Mobile responsive: shift cards stack below 768px

**Done when:** `/operations` for `MIK-01` shows the open day, its shift with assigned attendant and live cash status, and a one-click approve that flips the shift to `approved` within the publisher's tick.

---

## Phase 3 acceptance criteria

Phase 3 is complete when **all** of the following are true:

1. A station can open an operating day, open shifts within it, and assign attendants to specific nozzles.
2. Opening/closing pump meter readings are captured per nozzle with precision validation; litres dispensed is computed from the delta.
3. Opening/closing tank dips resolve to litres via the Phase-2 calibration API and are snapshotted on the reading row.
4. Shift close computes litres sold and expected cash; cash submission records the tender breakdown and the shortage/excess variance.
5. A supervisor approves a closed shift, exceptions block approval until resolved, and the day locks only once every shift is approved.
6. Every reading, transition, and cash submission rides audit + outbox like every Phase-1/2 sensitive write.
7. Attendants run a mobile-first "My Shift" console; supervisors get an operations dashboard of active shifts, approvals, and exceptions.

---

## Out of scope for Phase 3 (intentionally)

Reserved for later phases — don't let scope creep pull them in:

- **Stock ledger / book-vs-physical variance / opening + closing stock reconciliation** — Phase 4 (Inventory & Reconciliation Engine). Phase 3 *captures* readings; Phase 4 *reconciles* them against deliveries and sales.
- **Real pricing, price history, payment processing & settlement** — Phase 6 (Sales, Payments & Revenue). Phase 3's expected cash is an estimate off `nozzle.default_price`.
- **Deliveries / supplier receiving (truck intake)** — later supply-chain phase. Phase 3's day workflow leaves a slot for it but doesn't implement it.
- **Expenses, banking, and full cash-office reconciliation** — Finance phase.
- **Variance scoring, fraud signals, anomaly intelligence** — Phase 10 (Risk, Fraud & Intelligence). Phase 3 raises only mechanical exceptions (missing reading, raw variance, rollback).
- **Native mobile + offline capture** — Phase 14 (Mobile & Offline OS). The Phase-3 attendant console is a mobile-*responsive web* surface, online-only.

---

## Cross-phase considerations

A few Phase-3 decisions lock shape for later phases:

- **Meter-delta litres and snapshotted dip volumes** are the inputs Phase 4's stock ledger reconciles. Capturing them cleanly now (append-only, person-attributed, calibration-snapshotted) is what makes Phase-4 reconciliation trustworthy.
- **Expected cash uses the static `nozzle.default_price`.** Phase 6's pricing engine will replace the price *source* feeding `shift_close_lines.unit_price`; the close-summary shape stays the same.
- **`operating_day_id` and `shift_id` become the grouping keys** for Phase-4 stock movements and Phase-6 sales — every downstream transaction will reference the shift it happened in.
- **Reading immutability (supersede, never overwrite)** is the audit foundation regulators and Phase-10 fraud detection will lean on; if it's bypassed now, later phases inherit untrustworthy history.

If any of these change, the migration story for Phase 4+ will need careful sequencing.
