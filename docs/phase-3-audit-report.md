# Phase 3 Audit Report

Date: 2026-05-28
Scope: Phase 3 -- Station Operations Core

## Executive Verdict

Phase 3 is substantially implemented. The repository contains the operating-day, shift, assignment, meter reading, dip reading, close, cash submission, exception, attendant console, supervisor overview, SDK, and station dashboard pieces promised by the roadmap. The codebase also passes the repo-level validation suite.

The phase should be treated as a conditional pass, not a release-clean pass. The critical issue is that the backend enforces station-scoped permissions for attendant write workflows, but does not enforce the roadmap's self-scoped "my assignments only" rule on the write APIs. A station-scoped attendant can write readings and cash against shifts/nozzles they are not assigned to if they know the IDs.

The second release blocker is close-snapshot integrity. Meter and dip corrections remain allowed after a shift is closed, but `shift_close_lines` and cash expected values are frozen when the shift is closed. That can make the approved operational facts diverge from the corrected readings that Phase 4 will use as stock-ledger source material.

Recommended gate: fix the P1 items before Phase 4 posts any stock ledger entries from approved shifts.

## Audit Method

Reviewed:

- Phase 3 roadmap: `docs/roadmap-phase-3.md`
- Phase 3 migrations: `services/api/migrations/0014_*` through `0020_*`
- Backend route table and handlers under `services/api/internal/server`
- Domain repositories under `internal/operations` and `internal/readings`
- Phase 3 SDK types and client methods in `packages/sdk/src`
- Phase 3 UI pages: `/my-shift`, `/operations`, and station overview updates
- API contract in `docs/openapi.yaml`
- Test coverage under `internal/**/*test.go` and `services/api/internal/server/*test.go`

Automated checks run:

| Check | Result | Notes |
| --- | --- | --- |
| `go test ./...` | Pass | Unit/smoke suite passes. No Phase 3 DB-backed integration tests were found. |
| `go build ./...` | Pass | Go packages compile. |
| `pnpm typecheck` | Pass | SDK, UI, and web TypeScript compile. |
| `pnpm format:check` | Pass | Formatting is clean. |
| `pnpm build` | Pass | Next build succeeds. Warns that the Next ESLint plugin is not detected. |
| `pnpm lint` | Pass | `next lint` is deprecated and warns about missing Next ESLint plugin. |
| `npx --yes @redocly/cli@latest lint docs/openapi.yaml` | Pass | Spec is syntactically valid but does not cover Phase 3 endpoints. |

No live DB/API smoke test or browser QA pass was run in this audit. Findings that depend on runtime authorization are code-path findings from handlers, migrations, route registration, and repositories.

## Acceptance Matrix

| Acceptance criterion | Status | Evidence and notes |
| --- | --- | --- |
| Operating days support `open -> closed -> locked` lifecycle | Mostly met | Migration `0014_operating_days` and handlers exist. Closing blocks open shifts; locking blocks unapproved shifts. The API also allows reopening a closed day, which is not in the roadmap state machine. |
| Shifts live inside operating days and support attendants/nozzle assignments | Mostly met | Migration `0015_shifts`, handlers, repo, and SDK exist. Handler checks station and attendant membership, but the DB does not enforce all assignment invariants. |
| Meter readings are append-only with opening/closing and precision validation | Mostly met | Migration `0016_meter_readings`, repo, handlers, and unit tests exist. Capture/correction are append-only. Write scope and post-close correction semantics are not safe enough. |
| Tank dips resolve litres from active calibration charts | Mostly met | Migration `0017_tank_dip_readings` snapshots `volume_litres` and `chart_id`; handlers resolve calibration. Attendant UI does not expose dip capture, and write scope mirrors the meter-reading issue. |
| Shift close snapshots sales and requires readings/dips | Mostly met | Handler validates opening/closing meters and closing dips for assigned nozzles/tanks, then writes close lines. A zero-assignment shift can still close to expected cash `0`. |
| Cash submission and variance exception workflow | Mostly met | Cash submission exists, one submission per shift, high variance raises an exception, and approval blocks on open exceptions. Attendant cash submission is not self-scoped to the actor's shift. |
| Supervisor approval and day lock | Mostly met | Closed shifts approve only when open exceptions are resolved; day lock requires all shifts approved. Needs integration tests around the full chain. |
| Attendant "My Shift" console | Partially met | `/me/active-shift` and `/my-shift` are self-scoped for display and support meter readings plus cash submission. The page does not support tank dip entry, and backend writes are not self-scoped. |
| Supervisor `/operations` dashboard | Partially met | Overview, approve, and resolve-exception actions exist. The page does not expose opening days, opening shifts, assigning attendants/nozzles, or closing shifts, so it is a review/approval screen more than a full "run the day" console. |
| SDK support | Met | `packages/sdk/src/types.ts` and `client.ts` include Phase 3 models and methods. |
| OpenAPI contract | Not met | `docs/openapi.yaml` validates but searches for Phase 3 routes and schemas return no matches. |

## Major Findings

### P1 - Attendant write APIs are station-scoped, not self-scoped

The roadmap says the attendant console is "strictly scoped: an attendant sees only their own assignments." The display path follows that rule: `/me/active-shift` loads only shifts where the actor is in `shift_attendants`, and only nozzles where `shift_nozzle_assignments.attendant_id` is the actor.

The write paths do not carry that self-scope through. They only require the actor to hold the station-scoped permission on the shift's station:

- `shiftForWrite` authorizes `perm` against `shift.StationID` and does not check shift membership or nozzle assignment: `services/api/internal/server/shifts_handlers.go:195`.
- Meter capture uses `shiftForWrite(..., "reading.edit", true)` and only checks the nozzle belongs to the station, not the shift assignment or actor assignment: `services/api/internal/server/meter_readings_handlers.go:147`.
- Dip capture uses the same station-scoped permission and only checks the tank belongs to the station: `services/api/internal/server/dip_readings_handlers.go:134`.
- Cash submission uses `shiftForWrite(..., "cash.submit", false)` and does not check the actor is assigned to the shift: `services/api/internal/server/shift_close_handlers.go:303`.
- Migration `0020_attendant_perms` grants the attendant role both `reading.edit` and `cash.submit`: `services/api/migrations/0020_attendant_perms.up.sql:9`.

Impact:

An attendant scoped to a station can submit meter readings for another attendant's assigned nozzle, submit dips for any tank at the station, and submit cash for another closed shift if they know the IDs. The UI hides those IDs, but the API is the security boundary.

Recommendation:

- Add explicit self-scope checks for attendant-class write paths:
  - Meter capture/correction: actor must be assigned to the shift and the nozzle, unless they hold an elevated override permission.
  - Dip capture/correction: actor must be assigned to at least one nozzle drawing from that tank on that shift, unless elevated.
  - Cash submission: actor must be assigned to the shift, unless elevated.
- Consider separate permissions for supervisor overrides, for example `reading.override` and `cash.override`, rather than using station-scoped `reading.edit`/`cash.submit` for both attendants and managers.
- Add DB-backed tests where an attendant assigned to nozzle A gets 403/404-equivalent behavior when attempting writes against nozzle B, another shift, or another station.

### P1 - Corrections after close can desynchronize approved shift facts

Meter and dip corrections are allowed until the shift is approved:

- Meter correction comment and guard: "Corrections allowed until the shift is approved" and only `status == "approved"` is blocked in `services/api/internal/server/meter_readings_handlers.go:237`.
- Dip correction has the same rule in `services/api/internal/server/dip_readings_handlers.go:227`.
- Shift close freezes `shift_close_lines` from the current active readings and then closes the shift in `services/api/internal/server/shift_close_handlers.go:189`.
- Cash submission computes expected cash from the frozen close lines in `services/api/internal/server/shift_close_handlers.go:313`.

Impact:

A closed shift can have its active meter or dip reading corrected after close. The close summary, expected cash, variance exception, and approval facts remain based on the old close snapshot. Phase 4 explicitly plans to post stock ledger entries from approved Phase 3 numbers, so this can produce ledger entries from stale or contradictory operational facts.

Recommendation:

- Block meter and dip corrections once a shift is closed, unless a controlled reopen/reclose workflow exists.
- If post-close correction is required, make it transactional: void/invalidate close lines, cash submission, exceptions, and approval eligibility, then force a re-close and re-submit.
- Add integration tests for "correct after close" and "correct after cash submission" to assert the intended behavior.

### P2 - Assignment integrity is not fully enforced by the database

The normal handler path checks important invariants, but the schema does not enforce them:

- `shift_nozzle_assignments.attendant_id` references `users`, not the composite shift membership in `shift_attendants`: `services/api/migrations/0015_shifts.up.sql:103`.
- `handleAssignNozzle` checks the attendant is on the shift before insert: `services/api/internal/server/shifts_handlers.go:422`.
- `UnassignAttendant` deletes only from `shift_attendants`; it does not delete or block existing nozzle assignments for that attendant: `internal/operations/shifts.go:258`.

Impact:

Unassigning an attendant can leave `shift_nozzle_assignments` rows pointing at a user who is no longer a shift attendant. Direct DB writes, imports, future maintenance scripts, or handler regressions can also create nozzle assignments for users not on the shift.

Recommendation:

- Add a composite FK from `shift_nozzle_assignments(tenant_id, shift_id, attendant_id)` to `shift_attendants(tenant_id, shift_id, user_id)`.
- Make attendant unassignment either cascade/delete their nozzle assignments or refuse with a clear conflict until nozzles are unassigned.
- Add tests for unassigning an attendant with active nozzle assignments.

### P2 - Station and shift consistency is still mostly handler-enforced

The schema has tenant-bound FKs, but it does not fully enforce that related operational records share the same station:

- `shifts` has independent FKs to `stations` and `operating_days`; it does not enforce that the shift station equals the operating day station.
- `shift_nozzle_assignments` references `shifts` and `nozzles`; it does not enforce that the nozzle station equals the shift station.
- `meter_readings` references `shifts` and `nozzles`; it does not enforce that the nozzle is assigned to the shift.
- `tank_dip_readings` references `shifts` and `tanks`; it does not enforce that the tank belongs to assigned nozzles for that shift.

The handlers cover much of this today, but Phase 3 is foundational accounting data. Handler-only invariants are easier to break during imports, admin repair scripts, or future API paths.

Recommendation:

- Add station-bearing composite constraints where practical, or store `station_id` on operational child rows and enforce `(tenant_id, station_id, id)` relationships.
- Add constraints or triggers for "reading target is part of the shift assignment" if pure FK design becomes too awkward.
- Keep handler checks, but treat DB constraints as the final guard for accounting data.

### P2 - Zero-assignment shifts can close with zero expected cash

`handleCloseShift` iterates nozzle assignments and records missing readings only for assigned nozzles. If `assignments` is empty, no missing meter or dip errors are produced, `lines` remains empty, and the shift is closed with expected cash `0`.

Evidence:

- Assignments are loaded at `services/api/internal/server/shift_close_handlers.go:93`.
- The validation loop starts at line 140 and only validates rows that exist.
- Close lines are inserted from `lines`, then `CloseShift` runs regardless of line count at line 196.

Impact:

A supervisor can close an operationally empty shift accidentally. That creates an approved-looking day path with no sales lines, no cash expectation, and no exception. If zero-sales shifts are a legitimate business case, they need an explicit reason and audit trail.

Recommendation:

- Require at least one nozzle assignment before close.
- If zero-sales shifts are allowed, require an explicit close type/reason and surface it in approvals and audit.

### P2 - Phase 3 API is missing from OpenAPI

`docs/openapi.yaml` validates, but it does not document the Phase 3 API surface. Searches for `OperatingDay`, `MeterReading`, `DipReading`, `CashSubmission`, `ShiftException`, `/api/v1/me/active-shift`, `/api/v1/stations/{stationID}/operations-overview`, `/operating-days`, `/meter-readings`, `/dip-readings`, and `/cash-submission` return no matches in the OpenAPI file.

Impact:

The hand-maintained API contract no longer represents the implemented backend. This weakens SDK review, external integration, generated-client readiness, and CI contract value.

Recommendation:

- Add Phase 3 paths, request/response schemas, error responses, and auth notes to `docs/openapi.yaml`.
- Add a lightweight coverage check that compares registered route patterns to OpenAPI paths, even if it starts as an allowlist.

### P2 - Phase 3 lacks DB-backed integration coverage

The only DB-backed integration suite is still named and scoped to Phase 2:

- `services/api/internal/server/phase2_integration_test.go` describes Phase 2 coverage at lines 3-8.
- It is gated behind `TEST_DATABASE_URL` and `TEST_REDIS_URL`, and skips by default at line 74.
- Searches across `*test.go` files found no Phase 3 route tests for operating days, shifts, readings, dips, cash submission, approval, or `/me/active-shift`.

Impact:

The highest-risk Phase 3 behaviors are only protected by code review and a small readings unit test. There is no automated proof for the full operating-day path, self-scope rules, close/cash/exception atomicity, or day-lock guards.

Recommendation:

Add Phase 3 integration tests for:

- Open day -> open shift -> assign attendant/nozzle -> capture opening/closing readings -> capture closing dip -> close shift -> submit cash -> resolve exception -> approve shift -> lock day.
- Attendant self-scope forbidden cases.
- Correction after close/cash submission.
- Zero-assignment close.
- Unassign attendant with assigned nozzles.
- Cross-station attempts for day, shift, nozzle, tank, and reading IDs.

### P2 - Supervisor and attendant UI coverage is incomplete

The UI compiles and contains useful Phase 3 pages, but it does not expose the full operational workflow:

- `/my-shift` calls `api.myActiveShift`, `api.captureMeterReading`, and `api.submitCash`; it does not call `api.captureDipReading`.
- `/operations` calls `api.getOperationsOverview`, `api.approveShift`, and `api.resolveShiftException`; it does not call open day, open shift, assign attendant, assign nozzle, close shift, or submit cash methods.
- The sidebar still links `Shifts` to `/shifts`, which has no implemented route, while shifts are now a Phase 3 core concept.

Impact:

The backend can run more of the workflow than the UI can. Supervisors can review and approve, but not fully run the day from `/operations`. Attendants can enter meter readings and cash, but not tank dips from the mobile console.

Recommendation:

- Add dip capture to `/my-shift`, grouped by tanks behind the actor's assigned nozzles.
- Expand `/operations` with the missing day/shift/assignment/close actions, or create a dedicated shift detail page and link to it.
- Replace placeholder nav entries with real routes or hide them until implemented.

### P2 - Station tank fill can show stale cross-day dip data

Station overview sets `current_litres` from `LatestDipVolumesForStation`, which selects the most recent active dip for each tank across all shifts and days:

- `services/api/internal/server/station_overview_handler.go:105`
- `internal/readings/dips.go:124`

Impact:

The dashboard can display a historical dip as the current physical level, without exposing the reading timestamp, business date, or whether it came from opening or closing. This may be acceptable as a placeholder, but it is not yet a reliable "current stock" signal.

Recommendation:

- Return dip metadata with the volume: recorded time, reading type, shift, operating day, and status.
- Prefer the latest reading from the active operating day, or define the exact rule for "current" before Phase 4 ledger reconciliation depends on it.

### P3 - Operating day reopening is implemented but not specified

The roadmap describes an `open -> closed -> locked` lifecycle. The handler allows updating a closed day back to `open` and emits `operating_day.reopened`:

- `services/api/internal/server/operating_days_handlers.go:197`
- `services/api/internal/server/operating_days_handlers.go:238`
- `internal/operations/repo.go:123`

Impact:

This may be a useful operational escape hatch, but it changes the lifecycle semantics and should be explicit before approvals, audit exports, and Phase 4 posting rules are built on top.

Recommendation:

Either document reopening as an intentional state transition with permissions and constraints, or remove it and keep the roadmap's simpler lifecycle.

### P3 - Numeric precision is good enough for workflow testing, not final accounting

The DB uses `numeric`, but the API/SDK/UI move meter readings, dips, litres, prices, cash, and variances through `float64`/JavaScript `number`. `ValidateScale` uses float math for decimal precision.

Impact:

This is acceptable for early operational workflow testing, but Phase 4 stock and finance ledgers will need deterministic decimal behavior.

Recommendation:

Use decimal/string request parsing for accounting-grade values before ledger posting and finance reconciliation depend on these numbers.

## What Is Strong

- The Phase 3 route surface is broad and coherent.
- Write paths consistently use audit/outbox for major operational events.
- Readings and dips are append-only through supersede rows rather than silent updates.
- Shift close uses a transaction to snapshot close lines and transition the shift.
- Approval blocks on open exceptions, and day lock blocks on unapproved shifts.
- The SDK is in lockstep with the implemented Phase 3 backend surface.
- The repo-level build, typecheck, lint, format, Go test, Go build, and OpenAPI lint checks are green.

## Recommended Release Gate Before Phase 4

Before Phase 4 begins posting stock ledger entries from approved shifts:

1. Enforce attendant self-scope on reading and cash write endpoints.
2. Lock or transactionalize corrections after close so close lines, cash, exceptions, and approvals cannot diverge from active readings.
3. Add Phase 3 DB-backed integration tests for the full day workflow and failure cases.
4. Add OpenAPI coverage for Phase 3 endpoints.
5. Decide and document whether operating days can reopen.

Phase 3 can continue as an internal working implementation, but it should not be treated as accounting-grade input for Phase 4 until the first two items are fixed.
