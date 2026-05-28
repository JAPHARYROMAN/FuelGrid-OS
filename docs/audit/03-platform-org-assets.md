# Audit 03 — Platform / Org-Hierarchy / Physical-Assets

Read-only, atomic-level audit of the company → region → station hierarchy and
the physical-asset graph (products, tanks, pumps, nozzles, tank-calibration /
strapping charts, pump-calibration events) of FuelGrid OS. Focus is app-layer
correctness: tenant isolation (IDOR), authZ on mutations, input validation,
state machines, deletion guards, the dip→volume interpolation math, and
adherence to house conventions (decimal-string money, single tx wrapping
business + audit + outbox).

> RLS is defined-but-inert at runtime per the prior audit and is **not**
> re-litigated here. Where the DB-level composite FK / CHECK is the only real
> backstop, that is called out as a risk because the app layer is the live
> enforcement surface.

## Scope (files + LOC)

Data layer (`./internal/**`, 1,679 LOC):

| File | LOC |
|---|---|
| internal/companies/repo.go | 178 |
| internal/regions/repo.go | 148 |
| internal/stations/repo.go | 199 |
| internal/products/repo.go | 182 |
| internal/tanks/repo.go | 199 |
| internal/pumps/repo.go | 162 |
| internal/pumps/calibrations.go | 91 |
| internal/nozzles/repo.go | 181 |
| internal/calibration/interpolate.go | 61 |
| internal/calibration/csv.go | 96 |
| internal/calibration/repo.go | 182 |

HTTP layer (`./services/api/internal/server/**`, 3,158 LOC):

| File | LOC |
|---|---|
| companies_handlers.go | 267 |
| regions_handlers.go | 261 |
| stations_handlers.go | 334 |
| products_handlers.go | 308 |
| tanks_handlers.go | 506 |
| pumps_handlers.go | 626 |
| nozzles_handlers.go | 359 |
| calibration_handlers.go | 316 |
| station_overview_handler.go | 181 |

Supporting reads: policy_middleware.go, server.go routing block (lines
208–304), platform_handlers.go (`isUniqueViolation`), migrations 0001, 0008,
0009–0013; calibration unit tests; phase3/phase4 integration tests.

Total audited surface: ~4,837 LOC.

---

## Findings table

| ID | Severity | File:Line | Issue | Fix |
|---|---|---|---|---|
| ORG-01 | High | stations_handlers.go:47–73 | `handleListStations` ignores the actor's station scope — returns every station in the tenant to a station-restricted user (horizontal scope leak / IDOR). | Apply `stationReadFilter` like tanks/pumps/nozzles; thread the resulting id slice into `stations.List`. |
| ORG-02 | High | products/repo.go:23–28, tanks/repo.go:23–26, nozzles/repo.go:27, plus DTOs | Money/litres/density carried as Go `float64`, violating the decimal-string-in / arithmetic-in-SQL house rule. Round-trips `numeric(14,2/3/4)` through binary float. | Scan/serialise as `string` (or `pgtype.Numeric`/decimal); do arithmetic in SQL, never in Go float. |
| ORG-03 | High | tanks_handlers.go:362–432 | Tank delete only guards against live nozzles — **not against on-hand stock / ledger balance**. A tank holding fuel can be soft-deleted, orphaning its stock ledger and dip history. | Block delete when book balance ≠ 0 (or active opening balance / dip readings exist), inside the tx. |
| ORG-04 | High | calibration/repo.go:75–85; calibration_handlers.go:223–231,271 | Active chart is chosen purely by `status='active'`, ignoring `effective_from`. A future-dated chart goes live immediately; a back-dated upload still stamps the prior chart's `effective_until = now()`, breaking the temporal record and producing wrong interpolations *now*. | Select active chart by `effective_from <= now()` (and `effective_until` window); validate `effective_from >= superseded.effective_from`; set superseded `effective_until = new.effective_from`. |
| ORG-05 | Medium | tanks_handlers.go:244–360 | PATCH `/tanks/{id}` lets `capacity_litres`/`safe_*` change with **no re-validation** of `safe_min ≤ safe_max ≤ capacity`. Only the DB CHECK (`chk_tanks_safe_band`) catches it → opaque 500, not a 400. | Re-run the band invariant against the merged (before+patch) values before the UPDATE; return 400. |
| ORG-06 | Medium | products_handlers.go:257–274; tanks_handlers.go:374–398 | Child-guard counts (`CountActiveForProduct`, `CountActiveForTank`) run **outside** the transaction that soft-deletes → TOCTOU: a concurrent tank/nozzle insert races the delete. | Move the count + soft-delete into one tx with `FOR UPDATE` / re-check, or rely on a deferred FK. |
| ORG-07 | Medium | tanks_handlers.go:434–506; pumps_handlers.go:554–626 | Decommission transition does not stamp `decommission_date`; a decommissioned tank/pump keeps a NULL date. Tank decommission is allowed even when the tank still has stock or feeds nozzles. | Set `decommission_date = now()` on the `decommissioned` transition; block decommission of a tank with stock / live nozzles. |
| ORG-08 | Medium | stations_handlers.go:244–259; regions migration 0001:84 | Station `region_id` may be set to a region belonging to a **different company** in the same tenant. The composite FK only enforces same-tenant, not same-company. Region/station/company hierarchy can become inconsistent. | Validate `region.company_id == station.company_id` on create/update. |
| ORG-09 | Medium | companies_handlers.go:208–267; regions_handlers.go:206–261; stations_handlers.go:279–334 | Soft-delete of a company/region/station has **no child guard**. Soft-deleting a parent does not cascade (FK `ON DELETE` never fires on a status flip), orphaning live regions/stations/tanks under a "deleted" parent. | Block soft-delete when non-deleted children exist, or cascade the soft-delete in-tx. |
| ORG-10 | Medium | nozzles_handlers.go:186–298 | Nozzle `status` is a free `*string` with **no state-machine validation** — unlike tanks/pumps it bypasses `checkLifecycleTransition`. Any string in the CHECK set can be set directly, including jumping out of `decommissioned`. | Route nozzle status through the same lifecycle endpoint/validator, or validate transitions inline. |
| ORG-11 | Low | calibration/repo.go:165–181; interpolate.go:31–61 | Interpolation is entirely `float64`; the resolved litre volume is later persisted to `numeric(14,3)`. Linear interpolation of two `numeric` points in float can drift in the 4th+ decimal vs an exact rational result. | Interpolate with `math/big.Rat` or in SQL; keep litres as decimal end-to-end. |
| ORG-12 | Low | nozzles_handlers.go:80–184; tanks_handlers.go:145–242 | Nozzle create does not reject a `decommissioned`/`inactive` pump or tank; tank create does not reject a non-active station. You can wire live equipment onto retired parents. | Reject create when the resolved parent is not `active`. |
| ORG-13 | Low | nozzles_handlers.go:138–143 | Product-default price fallback swallows the product-load error (`else if … err == nil`): a real DB error silently yields price `0`. | Distinguish `ErrNoRows` from a genuine error; 500 on the latter. |
| ORG-14 | Low | stations migration 0001:122–123 | Station `code` uniqueness index is **case-sensitive** (`stations(tenant_id, code)`), unlike companies/regions/products which use `lower(...)`. "MIK-01" and "mik-01" coexist. | Index on `lower(code)` for consistency. |
| ORG-15 | Low | regions/repo.go; regions migration 0001:74,84 | Region `code` has **no uniqueness** at all (only `name` is unique per company). Scope expects per-tenant code uniqueness. | Add partial unique index on `(company_id, lower(code)) WHERE code IS NOT NULL AND status<>'deleted'`, or document the intent. |
| ORG-16 | Low | products migration 0009:39–40 | Product `name` has no uniqueness; only `code` is unique (case-insensitive). Two products may share a name. | Confirm intentional; if not, add a name unique index. |
| ORG-17 | Low | calibration_handlers.go:133–181,158 | `GET /calibrated-volume` reflects the raw `dip_mm` float back and accepts fractional dips even though charts store integer mm. Cosmetic but inconsistent with the CSV "whole-mm" rule. | Document that lookup permits fractional dips for interpolation, or floor to mm. |
| ORG-18 | Info | tanks_handlers.go:362–398 | Tank delete blocks on nozzles but the comment notes calibration charts "don't block" — charts FK is `ON DELETE RESTRICT`, so a real (hard) delete would fail; soft-delete is fine but leaves dangling active charts referencing a deleted tank. | Accept (history), or supersede the active chart on tank delete. |
| ORG-19 | Info | All handlers | Tx-wrapping of business + audit + outbox is correct and consistent; the `txAudit` helper named in conventions is **not used** here — every handler hand-rolls Begin/defer Rollback/Commit. Verbose and error-prone (see ORG-06) but correct. | Consider adopting `txAudit` for uniformity. |
| ORG-20 | Info | regions_handlers.go; companies/stations list | List endpoints have no pagination; tenant-wide catalogue reads return unbounded result sets. | Add limit/offset or keyset pagination before large tenants land. |

---

## Org hierarchy: company → region → station

### Tenant isolation & cross-tenant IDOR — mostly solid

Cross-tenant linking is blocked at the DB by composite FKs added in
`0008_tenant_integrity.up.sql`: `regions_company_fk` and `stations_company_fk`
reference `companies(tenant_id, id)`, `stations_region_fk` references
`regions(tenant_id, id)`. The handlers also pre-validate parents within the
actor's tenant before insert — `handleCreateRegion` calls
`s.companies.Get(ctx, actor.TenantID, req.CompanyID)` (regions_handlers.go:90)
and `handleCreateStation` validates both company and optional region
(stations_handlers.go:136–153). A cross-tenant `company_id` thus yields a clean
404, not a 500 or a successful cross-tenant write. **This is correct.** Region
update has no `CompanyID` field (regions/repo.go:32–36), so re-parenting across
companies via PATCH is impossible — good.

Every repo query filters `tenant_id = $1` and `status <> 'deleted'`. `Get`,
`Update`, `SoftDelete` all carry the tenant predicate. No raw-id lookups escape
tenant scoping in this domain.

### ORG-01 (High) — `handleListStations` leaks all stations to scoped users

The one real IDOR. Route (server.go:252–253):

```go
r.With(s.requirePermissionHeld("station.read")).
    Get("/stations", s.handleListStations)
```

`requirePermissionHeld` (policy_middleware.go:135–160) calls
`ps.HasPermission(perm)` which **ignores scope** (policy.go:52–56; the codebase
even has `TestHasPermissionIgnoresScope`). The handler then calls
`s.stations.List(ctx, actor.TenantID, regionID)` (stations_handlers.go:62) with
**no station-scope filter**. A station-restricted user (rows in
`user_station_access` for a single station, `TenantWide=false`) who holds
`station.read` will therefore receive **every station in the tenant**, including
ones they may not `GET /stations/{id}` individually (that route *is*
station-scoped via `requirePermission("station.read", stationFromURLParam)` at
server.go:213). The list and the get disagree — a horizontal access leak.

Contrast tanks/pumps/nozzles, which correctly call `stationReadFilter`
(tanks_handlers.go:87, pumps_handlers.go:98, nozzles_handlers.go:46) and pass
the scoped slice to `List`. `handleListStations` should do the same. The
companies/regions list endpoints share the `requirePermissionHeld` pattern, but
those are coarser org entities and arguably acceptable to expose tenant-wide;
**stations are explicitly station-scoped elsewhere**, so the inconsistency is a
defect, not a design choice.

### ORG-09 (Medium) — no child guard on soft-delete of parents

`handleDeleteCompany`, `handleDeleteRegion`, `handleDeleteStation` all simply
flip `status='deleted'` (companies/repo.go:163–175, etc.). Because soft-delete
is a status change, the FK `ON DELETE` actions never fire:

- `stations_region_fk` is `ON DELETE SET NULL` — but a *soft*-deleted region
  leaves `station.region_id` pointing at a now-"deleted" region row
  (0008:36–38). Stations then reference a deleted parent.
- Deleting a company with live regions/stations leaves them orphaned under a
  deleted company; the lists keep returning them (they filter their own status,
  not the parent's).

No handler checks for non-deleted children. A station can also be assigned (via
PATCH) a region whose `status='deleted'`, since the composite FK validates row
existence, not status. Recommend an explicit in-tx child-existence guard
(409 with a clear message) on each parent delete.

### ORG-08 (Medium) — region/company consistency on stations

`handleCreateStation` validates the region belongs to the tenant
(stations_handlers.go:144–153) but never checks
`region.company_id == req.CompanyID`. Likewise `handleUpdateStation` passes
`req.RegionID` straight through (stations_handlers.go:245) with no company-match
check. A station under company A can carry a region under company B (same
tenant). The hierarchy invariant "a station's region belongs to the station's
company" is unenforced at every layer (the DB only binds tenant). This corrupts
any region→company rollup reporting.

### ORG-14 / ORG-15 — code uniqueness inconsistencies

- Stations: `idx_stations_tenant_code ON stations(tenant_id, code)` is
  **case-sensitive** (0001:122). Companies (`lower(name)`), regions
  (`lower(name)`), products (`lower(code)`) are all case-insensitive. Either
  make stations consistent or document why.
- Regions have **no code uniqueness** (only `idx_regions_company_name` on
  `lower(name)`, 0001:84). The scope explicitly asks about per-tenant code
  uniqueness; regions have none. The `code` column is decorative.

### Per-file notes — companies/regions/stations handlers

- AuthZ: companies/regions writes are middleware-gated by
  `requirePermission("companies.manage"/"regions.manage", nil)` and stations by
  `requirePermission("station.manage", nil)` (server.go:238–258). Mutating
  routes are correctly gated. **No mutating route is unprotected.**
- Tx + audit + outbox: every create/update/delete opens a tx, calls the repo on
  the tx, then `audit.WriteWithOutbox` on the *same* tx, then commits, with
  `defer tx.Rollback`. Atomicity is correct.
- `handleUpdateCompany`/`Region`/`Station` re-read `before` with the pool
  (`s.companies.Get`), then `Update` on the tx with `RETURNING` — matching the
  "re-read via the same tx after UPDATE…RETURNING" convention (the UPDATE itself
  returns the after-row in-tx). Fine.
- Error mapping: 401/400/404/500 are handled; `companies` create does **not**
  map `isUniqueViolation` (a duplicate company name → 500 instead of 409),
  whereas products/tanks/pumps/nozzles do. Minor inconsistency (companies have a
  unique name index `idx_companies_tenant_name`). Worth a 409.

---

## Products

`numeric` types in DB are correct (`default_price numeric(14,2)`,
`tax_rate numeric(5,2)`, `density_kg_m3 numeric(10,3)`,
`loss_tolerance_percent numeric(5,2)`; CHECKs enforce tax 0–100, loss ≥ 0,
density > 0, color hex, category/unit enums — 0009:20–35). **Code uniqueness**
is case-insensitive per tenant (`idx_products_tenant_code`, 0009:39–40, partial
on `status<>'deleted'`), so deleted codes can be reused — good. `isUniqueViolation`
→ 409 on create and update (products_handlers.go:128,215).

### ORG-02 (High) — money/density as float64

`products.Product.DefaultPrice/TaxRate/DensityKgM3/LossTolerancePercent` are all
`float64` (products/repo.go:23–28); the DTO and request structs are `float64`
too (products_handlers.go:25–28, 91–94). The values are scanned from `numeric`
into binary float and serialised to JSON as floats. This is the canonical
money-as-float violation the house rules forbid ("Money = numeric strings,
arithmetic in SQL, NEVER float"). The same applies to `tanks.*Litres` and
`nozzles.DefaultPrice`. Today the values are only stored/echoed (no Go-side
arithmetic in this domain), so corruption is latent — but downstream phases that
read these structs and compute in Go will inherit silent precision loss. Fix at
the repo boundary: scan into `string`/`pgtype.Numeric`.

### ORG-06 (Medium) — TOCTOU on product delete

`handleDeleteProduct` (products_handlers.go:257–274) loads the product and calls
`s.tanks.CountActiveForProduct` **before** `Begin(ctx)` (line 276). A concurrent
`handleCreateTank` binding this product can slip in between the count and the
soft-delete commit, leaving a live tank pointing at a deleted product. The FK is
`ON DELETE RESTRICT` but soft-delete doesn't trip it. Same shape as ORG-03/ORG-06
for tanks. Move the guard inside the tx (and ideally `SELECT … FOR UPDATE` the
product) or lean on a real FK.

Validation: create requires non-empty code+name; everything else defaults in the
repo (`fuel`/`litre`/`#64748b`). No bound on `default_price`/`tax_rate` in the
handler, but DB CHECKs catch tax range. Note (ORG-16) product **name** is not
unique.

---

## Tanks / pumps / nozzles

### Creation constraints

- **Tanks** (tanks_handlers.go:145–242): requires station_id, product_id, name,
  code; `capacity_litres > 0`; `safe_min ≤ safe_max ≤ capacity`
  (lines 160–166). Station-scoped authZ `tanks.manage` *before* work
  (line 175). Validates station + product within tenant (lines 183–198). Unique
  (station, lower(code)) → 409. Composite FK `tanks_station_fk` /
  `tanks_product_fk` are the DB backstop. **Well-formed.**
- **Pumps** (pumps_handlers.go:152–226): requires station_id and `number > 0`;
  authZ `pumps.manage`; validates station; unique (station, number) → 409.
  Good.
- **Nozzles** (nozzles_handlers.go:80–184): requires pump_id, tank_id,
  `number > 0`; `meter_decimal_places ∈ [0,4]`; authZ `pumps.manage` on the
  pump's station; **enforces tank.station == pump.station**
  (lines 132–135) before insert. The repo derives `station_id`/`product_id` from
  the chosen tank so the row is always consistent (nozzles/repo.go:1–6) — and
  the composite FKs `nozzles_pump_fk` (tenant,pump,station) and `nozzles_tank_fk`
  (tenant,tank,station,product) enforce both invariants at the DB
  (0011:86–93). **The nozzle→pump→tank wiring integrity is the strongest part of
  this domain.** Update preserves it (nozzles_handlers.go:241–258: a tank
  reassignment re-derives station+product, re-checks same-station).

### ORG-12 (Low) — no "parent must be active" check at create

Nothing stops a nozzle being created on a `decommissioned` pump or a
`decommissioned`/`inactive` tank, nor a tank on a `suspended`/`closed` station.
The status CHECK sets include all these states; create only proves existence.
Live equipment can be wired onto retired parents.

### Status state machine (ORG-07, ORG-10)

`checkLifecycleTransition` (pumps_handlers.go:47–68) is shared by tanks and
pumps and is **correct**: it rejects unknown targets (400), no-op self
transitions (409 "already in status"), illegal moves per `lifecycleTransitions`
(409), and demands a reason for `maintenance`/`decommissioned` (400).
`decommissioned` is terminal (empty allowed-set, lines 31–36) — retired
equipment cannot silently revive. `deleted` is excluded from the status
endpoint (removal is via DELETE). This is solid.

Two gaps:

- **ORG-07**: transitioning a tank/pump to `decommissioned` does **not** set
  `decommission_date` (handleUpdateTankStatus only writes `Status`,
  tanks_handlers.go:478; pumps have no decommission_date column). And a tank can
  be decommissioned while still holding stock and feeding nozzles — the status
  endpoint has no inventory/nozzle guard, unlike DELETE.
- **ORG-10**: **nozzles bypass the state machine entirely.** `updateNozzleRequest`
  carries a free `Status *string` (nozzles_handlers.go:191) passed straight to
  `nozzles.Update` (line 236). Any value in the CHECK set can be set directly,
  including transitions the tank/pump machine would forbid (e.g.
  `decommissioned → active`). There is no `PATCH /nozzles/{id}/status` with
  `checkLifecycleTransition`. Inconsistent and unsafe.

### Deletion guards (ORG-03, ORG-06)

- **Tank delete** (tanks_handlers.go:362–432): blocks on live nozzles
  (`CountActiveForTank > 0` → 409). It does **NOT** check on-hand stock / ledger
  balance / dip history. The scope question "can you delete a tank with stock?"
  → **yes**. A tank holding fuel can be soft-deleted, severing its stock ledger
  and dip readings from the active inventory view. This is the headline assets
  bug (ORG-03). Also, the nozzle count runs before `Begin` (ORG-06 TOCTOU).
- **Pump delete** (pumps_handlers.go:324–396): lists live nozzles for the pump
  (`s.nozzles.List(..., &id)`) and blocks if any → 409. Correct guard, but again
  outside the tx (TOCTOU).
- **Nozzle delete** (nozzles_handlers.go:300–359): soft-delete, no guard needed
  (leaf). Fine. Authorized `pumps.manage` on the nozzle's station.

### ORG-05 (Medium) — PATCH /tanks drops the safe-band invariant

`handleUpdateTank` (tanks_handlers.go:260–360) accepts `capacity_litres`,
`safe_min_litres`, `safe_max_litres` as patches but performs **no**
`safe_min ≤ safe_max ≤ capacity` re-check (the create handler does, at 164). A
client can lower `capacity_litres` below the existing `safe_max_litres`; the DB
CHECK `chk_tanks_safe_band` rejects it as a constraint error → the handler logs
"update tank" and returns a generic **500**, not a 400. Re-validate the merged
values in-handler.

### Per-repo notes

- AuthZ on writes: tanks/pumps/nozzles/pump-cal/status are **not**
  middleware-gated (server.go:277–304 register them without `requirePermission`);
  each handler calls `authorizeStation(...)` against the station from the body
  (create) or the loaded row (update/delete/status). Every mutating handler in
  this set does call `authorizeStation` — verified create/update/delete/status
  for tanks, pumps, nozzles, and pump-cal. **No mutating asset route is
  unauthorized.** This is the documented "in-handler station scope" pattern and
  it is applied consistently.
- N+1: `station_overview_handler.go` deliberately batches — one List per
  child type per station, then a `nozzlesByPump` map (lines 100–104). No N+1.
  Shift attendant/assignment loops (lines 153–171) are per-open-shift, bounded.
  Acceptable.
- Indexes: every FK column is indexed (idx_tanks_station_id/product_id,
  idx_pumps_station_id, idx_nozzles_{station,pump,tank,product}_id). List
  queries filter on indexed columns. No missing-index hotspots found in this
  domain.
- `tanks.List` / `pumps.List` / `nozzles.List` use
  `($2::uuid[] IS NULL OR station_id = ANY($2))` with `database.UUIDStrings` —
  correct nil-vs-empty handling for "all" vs "these stations".
- Dead code: none material. `requirePermissionHeld` and `stationFromURLParam`
  carry `//nolint:unparam` for single-caller generality — acceptable.

---

## Calibration / strapping charts

### CSV parsing & validation (`internal/calibration/csv.go`) — strong

`ParseCSV` (csv.go:24–88) is strict and all-or-nothing:

- Header must be exactly `dip_mm,volume_litres`, case-insensitive
  (isExpectedHeader, lines 90–96). `FieldsPerRecord = 2` rejects wrong column
  counts.
- Each `dip_mm` must be a **whole number** (`dip != math.Trunc(dip)` → error,
  lines 60–61) — correct, since the column is `integer` in the DB
  (0012:56), preventing silent truncation.
- Non-negative dip and volume (line 67).
- **dip strictly increasing** (`dip <= prev.DipMM` → error, line 73): enforces
  sorted + no duplicates, which protects the interpolation's monotone search.
- **volume non-decreasing** (`vol < prev.VolumeLitres` → error, line 76): "more
  depth never means less fuel."
- At least two data rows (line 84).

Edge cases handled: empty file → error; trailing/leading spaces trimmed; bad
numbers name the offending line. The unit tests (csv_test.go) cover header,
non-integer dip, non-monotonic dip, decreasing volume, negative, too-few-rows,
non-numeric volume. **This is the best-validated input path in the audit.**

Minor: a CSV with a *blank trailing line* would be caught by `FieldsPerRecord=2`
(a single empty field ≠ 2) — acceptable. A duplicate header row would fail the
strict-increase check on the data side. No injection surface (numeric parse only).

### Interpolation MATH (`internal/calibration/interpolate.go`) — correct, float-based

`Interpolate` (interpolate.go:31–61):

- Empty → `ErrEmptyChart`; copies and sorts ascending (so callers needn't
  pre-sort; tested by `TestInterpolateUnsortedInput`).
- **Refuses extrapolation**: `dip < min || dip > max` → `ErrOutOfRange`
  (lines 40–42). Tested with 99.9, 200.1, -5.
- `sort.Search` finds the first entry `>= dip` (line 46). Exact match returns
  its exact volume (lines 48–51) — no rounding for charted points.
- Otherwise `lo = sorted[i-1]`; `i-1` is safe because the range check excludes
  `dip < sorted[0]` so `i==0` is impossible at this branch (the comment at 53 is
  accurate).
- Degenerate `span == 0` guarded (lines 55–58) — though strict-increase parsing
  makes duplicate dips unreachable via the API, this defends direct repo use.
- Linear formula `lo.V + ratio*(hi.V - lo.V)` (line 60) is correct, with no
  off-by-one. Midpoint test (`1240 → 12400`) and wide-gap test (`2130 → 21300`)
  pass.

The math is **logically correct and well-tested**. The reservation is ORG-11:
it is `float64` throughout, while inputs are `numeric` and the result is
persisted to `numeric(14,3)`. For typical strapping charts the float error is
sub-millilitre and below the stored precision, but it violates the
decimal-discipline rule and could in principle differ from an exact rational at
the 3rd decimal for adversarial point spacing. Low severity; flag for the same
reason ORG-02 is flagged.

### Active-chart selection & temporal model (ORG-04, High)

`ActiveChart` selects `WHERE … status = 'active'` (repo.go:75–85). The partial
unique index `idx_tcc_one_active` (0012:33–34) guarantees at most one active
chart per tank, so the singleton read is safe. **But selection ignores
`effective_from` entirely.** Consequences:

1. **Future-dated charts go live instantly.** `handleUploadCalibrationChart`
   accepts an arbitrary `effective_from` (calibration_handlers.go:223–231) and
   inserts the chart with `status='active'`. A chart dated next month is the
   active chart *today* and is used by every dip→volume `Lookup` immediately.
2. **Back-dated uploads corrupt history.** `SupersedeActive` unconditionally
   stamps the prior chart `effective_until = now()` (repo.go:90–97) regardless
   of the new chart's `effective_from`. Upload a chart with
   `effective_from` in the past and you get a prior chart that "ended" *after*
   the new one "began" — overlapping/contradictory validity windows.
3. There is **no DB constraint** that `effective_until > effective_from`, nor
   that successive charts' windows are contiguous/non-overlapping
   (0012:11–34 has none).

Because dip readings resolve litres against the active chart, a mis-dated
upload produces **wrong volumes right now** and an unreconstructable history.
Fix: select the active chart by `effective_from <= now() AND (effective_until IS
NULL OR effective_until > now())`; reject `effective_from` earlier than the
current active chart's; set the superseded `effective_until = new.effective_from`
(not `now()`).

### Upload handler — otherwise sound

`handleUploadCalibrationChart` (calibration_handlers.go:183–316):

- AuthZ: `authorizeStation(..., "tanks.calibrate", tank.StationID)` on the
  tank's station (line 196) — correct station scoping; route is **not**
  middleware-gated (server.go:304) by design.
- Memory bound: `http.MaxBytesReader` + `ParseMultipartForm(4MB)` (lines
  202–203) — DoS-safe.
- **Capacity sanity**: rejects a chart whose max volume exceeds
  `capacity_litres * 1.01` (lines 242–246) → 422. Good (the 1% tolerance for
  ullage/rounding is reasonable and documented).
- **Dry-run preview**: `?dry_run=true` or form `dry_run=true` returns
  entry_count + min/max dip + min/max volume without persisting (lines
  249–259). Correct — it validates everything (parse, capacity) first.
- Tx integrity: SupersedeActive + CreateChart + (conditional supersede audit) +
  upload audit + commit, all on one tx (lines 262–315). CreateChart uses
  `COPY FROM` for entries (repo.go:127–133) — efficient, and casts
  `int64(e.DipMM)` to match the `integer` column (the float was already
  validated whole by ParseCSV). Atomic and correct.

One nit: `CreateChart`'s CTE `SELECT chartColumns FROM ins c` recomputes
`EntryCount` via a correlated subquery that returns 0 (entries not yet inserted
at that point), then overwrites `c.EntryCount = len(entries)` in Go
(repo.go:134) — harmless but the subquery is wasted work for the insert path.

### Pump-calibration events (`internal/pumps/calibrations.go`)

`CreateCalibration` (calibrations.go:71–91) defaults status `passed`, defaults
`performed_at` to now, runs on the caller's tx. Handler
(pumps_handlers.go:464–543) validates status ∈ {passed,failed,adjusted},
parses RFC3339 `performed_at`, authZ `pumps.calibrate` on the pump's station,
audit+outbox in-tx. `performed_by` is forced to `actor.UserID` (not client
supplied) — good. `ListCalibrations` is tenant+pump scoped, newest-first.
`tolerance_percent` is `*float64` (same ORG-02 float concern, but it's a
tolerance, not money). No issues beyond the float type.

---

## Tests

- **Unit**: `interpolate_test.go` and `csv_test.go` give good coverage of the
  pure math/parsing (happy path, extrapolation refusal, empty, unsorted, all
  CSV rejection cases). These are the strongest tests in scope.
- **Integration**: `phase3_integration_test.go` exercises the *operational*
  layer (days/shifts/readings) and only uses calibration charts as a
  prerequisite (uploads a 2-point chart). `phase4` seeds tanks/charts/dips via
  **raw SQL** (lines 675–724) rather than the API. Consequently **none** of the
  org/asset CRUD edge cases found here are covered by tests: no test for the
  station-list scope leak (ORG-01), tank-delete-with-stock (ORG-03), the
  future/back-dated chart problem (ORG-04), PATCH safe-band (ORG-05), nozzle
  status bypass (ORG-10), or cross-company region assignment (ORG-08). This is a
  meaningful coverage gap for a domain this central.

---

## Severity counts

| Severity | Count | IDs |
|---|---|---|
| Critical | 0 | — |
| High | 4 | ORG-01, ORG-02, ORG-03, ORG-04 |
| Medium | 6 | ORG-05, ORG-06, ORG-07, ORG-08, ORG-09, ORG-10 |
| Low | 7 | ORG-11, ORG-12, ORG-13, ORG-14, ORG-15, ORG-16, ORG-17 |
| Info | 3 | ORG-18, ORG-19, ORG-20 |
| **Total** | **20** | |

No Critical: cross-tenant write IDOR is blocked by the 0008 composite FKs and
in-handler tenant guards, and every mutating route is authorized. The High
items are a same-tenant horizontal scope leak, the float-money convention
break, a destructive delete gap, and a temporal-correctness bug in the
calibration that feeds inventory math.

## Top-5 risks

1. **ORG-04 (High)** — calibration active-chart ignores `effective_from`;
   future/back-dated uploads go live immediately and corrupt the validity
   timeline, producing wrong dip→volume results *now*
   (calibration/repo.go:75–85; calibration_handlers.go:271). Inventory
   reconciliation depends on this.
2. **ORG-03 (High)** — a tank holding stock can be soft-deleted (only nozzles
   block it), orphaning its stock ledger and dip history
   (tanks_handlers.go:362–398).
3. **ORG-01 (High)** — `handleListStations` returns every station in the tenant
   to a station-restricted user, contradicting the station-scoped
   `GET /stations/{id}` (stations_handlers.go:47–73 vs server.go:213).
4. **ORG-02 (High)** — money/litres/density carried as Go `float64` across
   products/tanks/nozzles, breaking the decimal-string house rule and seeding
   latent precision loss for downstream Go-side arithmetic
   (products/repo.go:23–28; tanks/repo.go:23–26; nozzles/repo.go:27).
5. **ORG-10 (Medium)** — nozzle `status` is a free string that bypasses the
   `checkLifecycleTransition` state machine, allowing illegal transitions
   (incl. reviving a decommissioned nozzle) that tanks/pumps forbid
   (nozzles_handlers.go:186–298).
