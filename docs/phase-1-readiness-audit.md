# Phase 1 Readiness Audit

**Date:** 2026-06-03  
**Scope:** Phase 1 - Setup and master data from [feature-improvement-and-addition-plan.md](feature-improvement-and-addition-plan.md).  
**Conclusion:** Phase 1 is partially implemented, but it is not acceptable under the new definition of done. Backend master-data foundations exist; persisted setup-state, dedicated setup routes, opening-stock UI/SDK/approval flow, frontend permission gates, and frontend tests remain gaps.

## Verified Existing Coverage

| Area | Current implementation |
|---|---|
| Backend domains | `internal/companies`, `internal/regions`, `internal/stations`, `internal/products`, `internal/tanks`, `internal/pumps`, `internal/nozzles`, `internal/calibration`, `internal/workforce`, and `internal/identity` exist. |
| Database schema | Migrations exist for companies, regions, stations, products, tanks, pumps, nozzles, calibration charts, stock movements, opening balance uniqueness, workforce, permissions, RLS, and audit immutability. |
| API routes | `/api/v1/companies`, `/regions`, `/stations`, `/products`, `/tanks`, `/pumps`, `/nozzles`, `/tanks/{id}/calibration-charts`, `/tanks/{id}/opening-balance`, and workforce setup routes exist. |
| SDK | The SDK has typed methods for companies, regions, stations, products, tanks, pumps, nozzles, calibration charts, and workforce. |
| Frontend | `/setup` exists and computes live setup progress from API counts. Master-data pages exist under `/settings/companies`, `/settings/regions`, `/settings/stations`, `/settings/products`, `/settings/tanks`, `/settings/pumps`, and `/settings/tanks/[tankID]`. |
| Permissions | Backend routes are permission-gated with current codes such as `companies.manage`, `regions.manage`, `station.manage`, `products.manage`, `tanks.manage`, `pumps.manage`, `tanks.calibrate`, `stock.adjust`, and `station.read`. |
| Audit | Sensitive writes emit audit/outbox records across company, region, station, product, tank, pump, nozzle, calibration, workforce, pricing, and opening-balance flows. |

## Feature Status

| Feature | Status | Notes |
|---|---|---|
| 1.1 Guided setup checklist | Partial - verified | `/setup` uses real API counts, but there is no persisted `setup_steps` or `tenant_setup_state`, no setup checklist API, no saved step completion, and no command-center setup warning contract found. |
| 1.2 Company and region management | Partial - verified | Backend CRUD, DB uniqueness, permissions, audit, SDK, and settings UI exist. Gaps: target `/setup/company` and `/setup/regions` routes are absent, frontend has no explicit permission gate or forbidden state, and frontend tests are missing. |
| 1.3 Station management | Partial - verified | Backend CRUD, unique tenant station code, status field, permissions, audit, SDK, and settings UI exist. Gaps: no dedicated setup route, no explicit frontend permission gate, and no verified test run for DB-backed integration tests in this environment. |
| 1.4 Product management | Partial - verified | Product CRUD, product status, pricing, audit, SDK, and settings UI exist. Gaps: price changes are audited but not approval-gated, product default-price changes emit `product.updated` rather than `product.price_changed`, and frontend permission/test coverage is incomplete. |
| 1.5 Tank, pump, and nozzle setup | Partial - verified | Backend CRUD, station-scoped permissions, DB invariants for pump/tank/nozzle alignment, audit, SDK, and settings UI exist. Gaps: no dedicated `/setup/tanks`, `/setup/pumps`, `/setup/nozzles`; no explicit frontend permission gate; frontend tests missing. |
| 1.6 Opening stock setup | Blocked by gaps | Backend can set a tank opening balance through `/api/v1/tanks/{id}/opening-balance` and the ledger enforces one genuine opening per tank. Gaps: no `/setup/opening-stock` UI, no typed SDK method, OpenAPI says `200` while the handler returns `201`, no approval/lock workflow, no separate opening stock approval table, and no frontend tests. |

## Main Gaps

1. Persisted setup control is missing.
   - Missing `setup_steps` / `tenant_setup_state` or equivalent.
   - Missing `GET/PATCH /setup/checklist`.
   - `/setup` recomputes progress from counts instead of saving completion state.

2. Frontend routes differ from the plan.
   - Current routes are under `/settings/*`.
   - Planned routes are under `/setup/*`.
   - Decide whether `/setup/*` should be real routes or redirects/wrappers over settings pages.

3. Frontend permission readiness is incomplete.
   - Backend permission gates exist.
   - Phase 1 settings pages do not appear to use explicit permission gates or dedicated forbidden states.
   - Generic API error states exist, but the plan requires forbidden UI states.

4. Opening stock is not product-ready.
   - Backend ledger opening exists.
   - Missing setup UI, typed SDK method, approval flow, lock semantics, and OpenAPI contract alignment.

5. Permission codes need alignment.
   - The new planning docs use `setup.*` permissions.
   - Current implementation uses existing codes such as `companies.manage`, `regions.manage`, `station.manage`, `products.manage`, `tanks.manage`, `pumps.manage`, `tanks.calibrate`, and `stock.adjust`.
   - Choose whether to migrate codes or update the planning matrix to reflect current permission vocabulary.

6. Test verification is incomplete.
   - Scoped Go package tests passed.
   - SDK tests and typecheck passed.
   - DB-backed server integration tests skipped because `TEST_DATABASE_URL` and `TEST_REDIS_URL` are not set.
   - No Phase 1 frontend tests or e2e tests were found for setup/settings master-data flows.

## Verification Commands

```text
go test ./internal/companies ./internal/regions ./internal/stations ./internal/products ./internal/tanks ./internal/pumps ./internal/nozzles ./internal/calibration ./internal/workforce ./internal/identity/...
```

Result: passed. Several packages have no test files; `internal/calibration`, `internal/workforce`, and identity packages passed tests.

```text
pnpm --filter @fuelgrid/sdk test
pnpm --filter @fuelgrid/sdk typecheck
```

Result: passed.

```text
go test -v ./services/api/internal/server -run 'TestPhase2|TestPhase4_TankLedger|TestPhase4_Opening|TestPhase4_Delivery'
```

Result: passed with all targeted DB-backed tests skipped because `TEST_DATABASE_URL` and `TEST_REDIS_URL` are unset.

## Recommended Next Implementation Order

1. Add a persisted setup checklist backend.
2. Add setup checklist OpenAPI and SDK methods.
3. Decide route strategy for `/setup/*` versus `/settings/*`.
4. Build `/setup/opening-stock` using the real opening-balance backend.
5. Add a typed SDK method for opening balance and fix OpenAPI response status.
6. Add explicit forbidden states and permission gates to Phase 1 setup/settings pages.
7. Add frontend tests for setup, company, region, station, product, tank, pump/nozzle, and opening-stock flows.
8. Run DB-backed integration tests with `TEST_DATABASE_URL` and `TEST_REDIS_URL`.
