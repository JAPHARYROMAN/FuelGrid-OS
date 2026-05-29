# Phase 2 Audit Report

Date: 2026-05-28
Scope: Phase 2 -- Fuel Infrastructure Core

## Executive Verdict

Phase 2 is substantially implemented across database schema, backend handlers, SDK methods, seed data, admin pages, station dashboard, and reusable tank/pump visuals. The core product, tank, pump, nozzle, calibration, incident, and dashboard flows are present and the repository builds cleanly.

The phase should be treated as a conditional pass, not a release-clean pass. The main blockers before building Phase 3 on top are station-scoped read authorization, missing OpenAPI coverage for the Phase 2 API surface, lack of DB-backed integration tests for Phase 2 invariants, and several data integrity edges around soft deletes and calibration import.

## Audit Method

Reviewed:

- Phase 2 roadmap: `docs/roadmap-phase-2.md`
- Phase 2 migrations: `services/api/migrations/0009_*` through `0013_*`
- Backend route table and handlers under `services/api/internal/server`
- Domain repositories under `internal/products`, `internal/tanks`, `internal/pumps`, `internal/nozzles`, `internal/calibration`, `internal/incidents`
- Transactional audit/outbox helper under `internal/audit` and `internal/events`
- Demo seed data in `services/api/cmd/seed/main.go`
- SDK types and client methods in `packages/sdk/src`
- Phase 2 UI pages and shared UI components under `apps/web/src` and `packages/ui/src`
- API contract in `docs/openapi.yaml`

Automated checks run:

| Check | Result | Notes |
| --- | --- | --- |
| `go test ./...` | Pass | Unit/smoke suite passes. Coverage is thin for Phase 2 DB behavior. |
| `pnpm typecheck` | Pass | SDK, UI, and web TypeScript compile. |
| `pnpm format:check` | Pass | Formatting is clean. |
| `pnpm build` | Pass | Next build succeeds. Warns that the Next ESLint plugin is not detected. |
| `pnpm lint` | Pass | `next lint` is deprecated and also warns about missing Next ESLint plugin. |
| `npx --yes @redocly/cli@latest lint docs/openapi.yaml` | Pass | Spec is syntactically valid but incomplete for Phase 2. |

No live DB/API smoke test was run in this audit. Findings that depend on runtime authorization are code-path findings from the route table, policy middleware, handlers, and repositories.

## Acceptance Matrix

| Acceptance criterion | Status | Evidence and notes |
| --- | --- | --- |
| Product catalogue operator-managed via `/settings/products` with visual identity | Mostly met | Migration, repo, handlers, SDK, seed data, and UI create/edit with color swatch exist. Backend delete exists. UI does not expose delete/deactivate. |
| Physical inventory configurable via admin UI; demo tenant modeled | Mostly met | Tanks, pumps, nozzles migrations/repos/handlers/SDK/pages/seeds exist. Tanks can be created/edited; pumps and nozzles can be created/deleted, but pump/nozzle edit UI is limited. |
| Nozzle product matches tank product, DB-enforced | Met | Composite FKs in `0011_pumps_nozzles.up.sql` enforce station and product consistency. Handler also derives product from tank. |
| Tank calibration charts return interpolated volumes | Mostly met | Interpolation package and API exist. CSV upload/dry-run/list/active/lookup exist. Needs DB-backed endpoint tests and stricter dip parsing/capacity validation. |
| Pump calibration and incident transitions use audit + outbox | Mostly met | Write paths use `audit.WriteWithOutbox` inside the same transaction. Incident intermediate transitions emit `incident.updated`, while terminal transitions emit `incident.resolved`. Needs integration proof. |
| Station dashboard renders tank visuals + pump grid | Met with scoped caveats | `/stations/{stationID}` uses `getStationOverview`, `TankVisual`, and `PumpCard`. Current tank volume remains intentionally empty until Phase 3 readings. |
| Iconic visuals reusable in UI package | Mostly met | `TankVisual` and `PumpCard` are exported from `@fuelgrid/ui`. `/dev/visuals` mock route remains deferred as noted in the roadmap. |

## Major Findings

### P1 - Station-scoped read authorization leaks across Phase 2 catalogue endpoints

Phase 2 write endpoints generally authorize against the relevant station, but many read endpoints only check whether the actor holds `station.read` somewhere. The middleware `requirePermissionHeld` checks `ps.HasPermission(perm)` and does not enforce `PermissionSet.StationIDs`.

Evidence:

- Broad route gates in `services/api/internal/server/server.go`: tanks at line 218, pumps/nozzles at line 229, calibration reads at line 244, pump calibration history at line 255, incidents at line 263.
- `requirePermissionHeld` implementation in `services/api/internal/server/policy_middleware.go` line 68 ignores station scope by design.
- Handlers such as `handleListTanks`, `handleGetTank`, `handleListPumps`, `handleGetPump`, `handleListNozzles`, `handleListCalibrationCharts`, `handleCalibratedVolume`, `handleListPumpCalibrations`, and `handleListIncidents` do not perform a follow-up `authorizeStation(..., "station.read", stationID)` check.

Impact:

A user scoped to one station but holding `station.read` can likely list or retrieve tanks, pumps, nozzles, calibration metadata, pump calibration history, and incidents for other stations in the same tenant if they know or can discover IDs. The station overview route itself is correctly gated, but adjacent Phase 2 read APIs are not.

Recommendation:

- For list endpoints with a `station_id` filter, require that filter and authorize it, or filter results to the actor's accessible station IDs.
- For get-by-id endpoints, load the row, then authorize `station.read` against the row's `station_id` before returning it.
- For calibration reads and pump calibration history, authorize against the parent tank/pump station before returning chart/history data.
- Add DB-backed tests using the seeded restricted demo operator: MIK-01 should pass and MSA-01 should 403.

### P1 - OpenAPI contract is missing the Phase 2 API surface

`docs/openapi.yaml` validates, but it does not document the Phase 2 endpoints. Searches for Product, Tank, Pump, Nozzle, Incident, Calibration, `/api/v1/products`, `/api/v1/tanks`, `/api/v1/pumps`, `/api/v1/nozzles`, `/api/v1/incidents`, and `/api/v1/stations/{stationID}/overview` return no matches.

Impact:

The hand-maintained API contract no longer represents the implemented API. This weakens external integration, SDK review, CI confidence, and future automatic generation work.

Recommendation:

Add schemas and paths for:

- Products CRUD
- Tanks CRUD and status transition
- Pumps CRUD, status transition, and calibrations
- Nozzles CRUD
- Tank calibration chart upload/list/active/lookup
- Incidents list/create/status transition
- Station overview

### P1 - Phase 2 lacks DB-backed integration tests for the critical invariants

Current tests pass, but they do not prove the Phase 2 behavior that carries the most risk. `services/api/internal/server/server_test.go` only covers health/readiness and request ID middleware. Calibration unit tests cover interpolation, not API upload/lookup with persisted charts.

Missing test coverage:

- Restricted station user gets 403 for out-of-scope Phase 2 read endpoints.
- Nozzle product/station mismatch is rejected at the DB layer.
- Product/tank/pump/nozzle mutations create audit and outbox rows atomically.
- Pump calibration creates row + audit + outbox in one transaction.
- Calibration upload dry-run does not persist.
- Calibration upload supersedes the active chart and preserves old chart history.
- `GET /calibrated-volume` returns expected interpolation from seeded chart.
- Invalid CSV rejects cleanly without partial commit.

Recommendation:

Add a Phase 2 API integration suite that runs migrations against Postgres, seeds the demo dataset, logs in as both restricted operator and admin, and asserts authorization, invariants, and audit/outbox side effects.

### P2 - Soft deletes can leave logical dependents active

Products and tanks are soft-deleted by setting `status = 'deleted'`. Because this is not a hard delete, foreign keys do not prevent active dependent rows from continuing to reference hidden rows.

Evidence:

- Product soft delete: `internal/products/repo.go` line 168 and `services/api/internal/server/products_handlers.go` line 273.
- Tank soft delete: `internal/tanks/repo.go` line 183 and `services/api/internal/server/tanks_handlers.go` line 390.
- Pump delete has an explicit live-nozzle guard in `services/api/internal/server/pumps_handlers.go` line 311, but product and tank deletes do not have equivalent guards.

Impact:

Deleting a product used by a tank hides it from product lists while tanks/nozzles still reference it. Deleting a tank with live nozzles can hide the source tank while nozzles remain active. The station dashboard then falls back to unknown product/tank display data, and later Phase 3/4 operational logic can inherit broken configuration.

Recommendation:

- Block product delete when active tanks reference it.
- Block tank delete when active nozzles or active calibration charts reference it, or require an explicit decommission path first.
- Return 409 with an actionable message instead of allowing logical orphaning.

### P2 - Calibration CSV accepts decimal dip values but persists integer dip values

The CSV parser accepts `dip_mm` via `strconv.ParseFloat`, but `CreateChart` casts `e.DipMM` to `int64` before inserting into `tank_calibration_entries.dip_mm`.

Evidence:

- Float parsing in `internal/calibration/csv.go` line 53.
- Integer truncation in `internal/calibration/repo.go` line 125.
- DB column is integer in `services/api/migrations/0012_tank_calibration.up.sql`.

Impact:

A CSV with decimal dip values can pass dry-run validation, then be silently truncated on commit. In edge cases, two distinct decimals such as `1.2` and `1.8` pass strict-increase validation but collide after truncation, causing commit failure after dry-run success.

Recommendation:

- Parse `dip_mm` as integer, or explicitly reject non-integer floats before dry-run success.
- Keep lookup query `dip_mm` flexible if operators enter decimal values, but persisted chart points should match the schema exactly.
- Add a dry-run/commit parity test for decimal dip values.

### P2 - Calibration import does not validate chart volume against tank capacity

The roadmap calls for strict CSV validation with a sensible volume range. Current validation checks header, row count, monotonic dip, non-negative values, and non-decreasing volume, but it does not compare maximum chart volume against the tank capacity.

Impact:

An uploaded chart can map a tank to a physically impossible volume. That will contaminate Phase 3 dip readings and Phase 4 stock calculations.

Recommendation:

After loading the target tank, reject charts whose max volume exceeds `capacity_litres` by more than a deliberate tolerance. Consider also requiring first volume to be zero or explicitly allowing a non-zero dead-stock model.

### P2 - Status lifecycle is an enum gate, not a state machine

The roadmap says status state machines should be explicit. The current dedicated pump/tank status endpoints accept any target in `active`, `inactive`, `maintenance`, `decommissioned` from any current state. Reason is optional.

Evidence:

- `lifecycleStatuses` in `services/api/internal/server/pumps_handlers.go`.
- `statusChangeRequest` has `reason,omitempty`.
- Tank and pump status handlers write the reason if supplied, but do not require it.

Impact:

Sensitive lifecycle changes can happen without transition policy or required rationale. Later operational phases may assume decommissioned equipment cannot move back to active without a specific workflow.

Recommendation:

Define allowed transitions per entity, require reason for non-trivial transitions, and return 409 for invalid transitions. Keep audit/outbox payloads unchanged.

### P2 - Several validation failures can surface as generic 500s

Some invalid inputs are guarded in the handler, but others rely on DB CHECK/FK errors and return generic internal errors. Examples include invalid product color/category/status, negative tank dead stock on API update, invalid status through general update endpoints, and tank product reassignment when live nozzles reference the old product.

Impact:

Operators receive "internal error" for user-correctable input, and tests cannot reliably distinguish validation behavior from server faults.

Recommendation:

Map CHECK/FK violations to 400 or 409 with domain-specific messages. Prefer handler validation for obvious numeric/status/color constraints.

### P3 - Admin UI coverage is present but not complete CRUD

The backend and SDK expose full CRUD for products, tanks, pumps, and nozzles. The UI covers the most important create/edit/delete paths, but not all of them:

- Products page supports create/edit but no delete/deactivate.
- Tanks page supports create/edit/calibration but no delete/decommission action.
- Pumps page supports create/delete and nozzle create/delete, but no edit dialog for pump/nozzle metadata.
- Pump status changes live on pump detail, but tank status changes have no matching UI surface.

Impact:

The admin UI can model the seeded/demo infrastructure, but it is not yet a complete operator management surface for ongoing corrections and lifecycle work.

Recommendation:

Add explicit lifecycle/decommission actions first, then fill in edit/delete gaps where operationally safe.

### P3 - Sidebar still has Phase 2 dead links

The sidebar points `Tanks` to `/tanks` and `Pumps` to `/pumps`, and the comment states many links intentionally 404. Phase 2 actually has useful pages at `/settings/tanks`, `/settings/pumps`, and station-scoped pump detail routes.

Evidence:

- `apps/web/src/components/layout/sidebar.tsx` lines 34-35 and line 52.

Impact:

Operators can click Phase 2 concepts in the primary nav and land on 404s, even though the corresponding admin/configuration surfaces exist.

Recommendation:

Either route these to working Phase 2 pages or hide them until operator-facing `/tanks` and `/pumps` pages exist.

### P3 - Numeric precision is enforced in the DB but not in the application layer

Migrations use `numeric(14, 3)` for litres and `numeric(14, 2)` for prices, but Go DTOs and repos use `float64` throughout Phase 2.

Impact:

This is tolerable for a static catalogue prototype, but Phase 3/4 meter readings, dip readings, and stock ledger math should not inherit binary floating-point behavior.

Recommendation:

Before Phase 3 ledger-facing code lands, decide on an application-level decimal representation for litres and money. At minimum, normalize/round at API boundaries and test decimal edge cases.

## What Is Strong

- The database model is coherent and tenant-bound with composite FKs for the important nozzle/pump/tank/product invariants.
- Phase 2 mutations consistently follow the Phase 1 transaction pattern: domain write plus audit plus outbox in one transaction.
- Seed data is practical and matches the roadmap: PMS/AGO/KERO, MIK-01 and MSA-01 tanks, MIK-01 pumps/nozzles, and a seeded PMS calibration chart.
- The station overview endpoint avoids frontend N+1 calls and is correctly station-scoped.
- `TankVisual` and `PumpCard` are reusable package exports and are already used on the station dashboard.
- Build, typecheck, lint, format, and Go tests all pass.

## Recommended Phase 3 Gate

Do these before Phase 3 starts writing operational readings against this infrastructure:

1. Fix Phase 2 read authorization so every station-scoped read either authorizes the station or filters to the actor's station scope.
2. Add DB-backed integration tests for authorization, nozzle DB invariants, calibration upload/lookup, and audit/outbox side effects.
3. Update `docs/openapi.yaml` with the full Phase 2 API surface.
4. Add soft-delete dependency guards for products and tanks.
5. Make calibration CSV dry-run and commit semantics identical by rejecting non-integer `dip_mm` chart points and validating volume against tank capacity.
6. Define lifecycle transition rules and require reasons for sensitive status changes.

After those are done, Phase 2 is a solid base for Phase 3 station operations.
