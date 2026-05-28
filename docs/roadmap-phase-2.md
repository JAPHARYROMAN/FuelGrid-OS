# Phase 2 — Fuel Infrastructure Core

The first phase that builds **fuel-specific** functionality on top of the Phase-1 platform. When this phase is done, FuelGrid OS knows what fuel a station stores, in which tanks, dispensed by which pumps and nozzles, with calibration data trusted enough to do real volumetric math against.

No transactions happen yet — that lands in Phase 3 (shifts, readings) and Phase 4 (inventory ledger). Phase 2 is the **static infrastructure catalog** + the **iconic visuals** every later screen reuses.

## Stack decisions (carried forward from Phase 1)

All Phase-2 work continues to ride the patterns that locked in during Phase 1:

| Concern | Continued choice |
|---|---|
| Backend transactions | One tx wraps business change + audit + outbox (Stage 7 helper) |
| Tenant scoping | Every repo query takes `tenantID` first; RLS is the safety net |
| Authorization | `requirePermission(code, scopeExtractor)` middleware |
| Migrations | One concern per file; system permissions seeded inline |
| Frontend forms | shadcn-style primitives in `@fuelgrid/ui` + Radix Dialog + react-hook-form |
| Data fetching | TanStack Query with hand-written `@fuelgrid/sdk` methods |

New conventions specific to Phase 2:

| Concern | Convention |
|---|---|
| Numeric precision | All fuel volumes stored as `numeric(14, 3)` (litres to 3 decimals — meter granularity). Money stays at `numeric(14, 2)`. |
| Status state machines | Each entity ships with explicit `status` enum + CHECK; transitions write audit + outbox. |
| Product visual identity | Per-product `color` hex stored in the row; UI binds to it via `bg-[hsl(var(--color-fuel-*))]` token mapping. |

---

## Category A — Physical model

The catalog operators install. Pure CRUD + wiring; no operational state yet.

### Stage 1 — Products

**Goal:** Every tenant has a product catalogue that the rest of the OS references by id.

- [x] Migration `0009_products`: `products` (id, tenant_id, code, name, category, unit, default_price, tax_rate, density_kg_m3, loss_tolerance_percent, color, status, timestamps)
- [x] Seed system permissions: `products.manage` (tenant-wide)
- [x] Repo `internal/products` with the standard CRUD shape from Phase 1
- [x] Handlers + SDK methods: list / get / create / update / delete
- [x] `/settings/products` admin page with color swatch in the table and a color picker in the create/edit dialog
- [x] Seed three products on the demo tenant: **PMS** (orange), **AGO/Diesel** (blue), **Kerosene** (purple). Wire `color` to existing `--color-fuel-*` tokens.
- [x] Audit + outbox for every mutation (`product.created`, `product.updated`, `product.deleted`)

**Done when:** `/settings/products` lists three seeded products with their visual identities and the demo admin can add a fourth (LPG) through the UI alone.

---

### Stage 2 — Tanks

**Goal:** Every station has a tank inventory bound to products, with capacity limits the loss/overfill engine can reason against.

- [x] Migration `0010_tanks`: `tanks` (id, tenant_id, station_id, product_id, name, code, capacity_litres, safe_min_litres, safe_max_litres, dead_stock_litres, has_water_sensor, has_temp_sensor, status, installation_date, decommission_date, timestamps)
- [x] CHECK: `safe_min_litres <= safe_max_litres <= capacity_litres` and `dead_stock_litres >= 0`
- [x] Permission `tanks.manage` (station-scoped). `station.read` already covers list/get.
- [x] Repo + handlers + SDK methods, filter by `station_id`
- [x] `/settings/tanks` admin page: station picker → tanks table. Create dialog binds to a product, captures all limits.
- [x] Seed: two tanks on `MIK-01` (PMS 30,000L; AGO 30,000L), one on `MSA-01` (PMS 25,000L)
- [x] Audit + outbox for every mutation

**Done when:** The seeded admin can attach a new tank to a station and see the product's color reflected in the row chip.

---

### Stage 3 — Pumps & Nozzles

**Goal:** The dispensing layer is fully configured: every nozzle pulls from one tank and dispenses one product at a configurable price.

- [ ] Migration `0011_pumps_nozzles`:
  - `pumps` (id, tenant_id, station_id, number, name, manufacturer, model, serial_number, status, installation_date, timestamps)
  - `nozzles` (id, tenant_id, station_id, pump_id, tank_id, product_id, number, default_price, meter_decimal_places, status, timestamps)
  - Triggers / DB-level CHECK enforce: `nozzle.product_id = tank.product_id` and `nozzle.station_id = pump.station_id = tank.station_id`
- [ ] Permissions: `pumps.manage` (station-scoped), `nozzles.manage` (folds into `pumps.manage` for now)
- [ ] Hierarchical admin UI at `/settings/pumps`: station picker → pumps list → expand a pump to see its nozzles
- [ ] "Add nozzle" dialog filters tanks by station and locks the product field to the chosen tank
- [ ] Seed: two pumps at `MIK-01` — pump 1 has two PMS nozzles, pump 2 has two AGO nozzles. Default price comes from the product.
- [ ] Audit + outbox: `pump.*` and `nozzle.*`

**Done when:** The demo station shows two pumps with four nozzles total, each correctly wired; reassigning a nozzle to a tank of a different product is rejected at the DB layer.

---

## Category B — Calibration & status

How we trust the numbers the operational layers will start producing in Phase 3.

### Stage 4 — Tank calibration charts

**Goal:** Given a dip reading in millimetres, the API returns the corresponding volume in litres. Charts are versioned so a re-strap doesn't lose history.

- [ ] Migration `0012_tank_calibration`:
  - `tank_calibration_charts` (id, tenant_id, tank_id, name, effective_from, effective_until, status, source, timestamps)
  - `tank_calibration_entries` (id, chart_id, dip_mm, volume_litres) — sparse rows; lookups interpolate
  - Constraint: at most one `status='active'` chart per tank at a time (partial unique index)
- [ ] Permission `tanks.calibrate` (station-scoped)
- [ ] `internal/calibration` package with a `Lookup(chartID, dipMM) (litres, error)` function that linearly interpolates between the two surrounding entries (and refuses extrapolation)
- [ ] Endpoints:
  - `POST /api/v1/tanks/{id}/calibration-charts` — multipart CSV upload (header: `dip_mm,volume_litres`)
  - `GET /api/v1/tanks/{id}/calibration-charts` — list
  - `GET /api/v1/tanks/{id}/calibration-charts/active` — return the current chart
  - `GET /api/v1/tanks/{id}/calibrated-volume?dip_mm=N` — interpolation result (the API the Phase-3 dip-reading handler will call)
- [ ] CSV import: parse with strict validation (monotonic dip_mm, no duplicates, sensible volume range), preview, then commit. Reject rather than partial-commit on the first malformed row.
- [ ] Tank detail page shows the active chart name + effective dates + a "Replace chart" action
- [ ] Audit + outbox: `tank_calibration.chart_uploaded`, `tank_calibration.chart_superseded`
- [ ] Seed: a 50-entry chart for MIK-01's PMS tank (0..3000mm in 60mm steps)

**Done when:** `GET /api/v1/tanks/{MIK-PMS-id}/calibrated-volume?dip_mm=1240` returns a linearly-interpolated litre figure; replacing the chart preserves the old one as `status='superseded'`.

---

### Stage 5 — Pump calibration & status lifecycle

**Goal:** Pump calibration events are a first-class audit record, and pump/tank lifecycle transitions go through the same audit + outbox pipeline as every other sensitive write.

- [ ] Migration `0013_pump_cal_and_incidents`:
  - `pump_calibrations` (id, tenant_id, pump_id, performed_at, performed_by, notes, tolerance_percent, status, timestamps)
  - `incidents` (id, tenant_id, station_id, related_entity_type, related_entity_id, type, severity, description, status, opened_at, opened_by, resolved_at, resolved_by, timestamps)
- [ ] Permissions: `pumps.calibrate`, `incidents.manage`
- [ ] Endpoints:
  - `POST /api/v1/pumps/{id}/calibrations` — record a calibration event
  - `GET /api/v1/pumps/{id}/calibrations` — history
  - `PATCH /api/v1/pumps/{id}/status` — transition status with reason
  - `PATCH /api/v1/tanks/{id}/status` — same for tanks
  - `POST /api/v1/incidents` + `PATCH /api/v1/incidents/{id}/status`
- [ ] Pump detail page (`/stations/{id}/pumps/{pumpID}`): calibration history table + "Record calibration" form + status toggle with reason capture
- [ ] Incidents queue at `/incidents` with severity filters and an "Open new incident" dialog
- [ ] Every transition writes audit + outbox: `pump.status_changed`, `tank.status_changed`, `pump.calibrated`, `incident.opened`, `incident.resolved`

**Done when:** Recording a pump calibration creates the `pump_calibrations` row + audit log + `PumpCalibrated` outbox event in one transaction; the pump detail page shows the entry within the publisher's tick.

---

## Category C — Surfaces

The daily operator UX. Read-only views aimed at station managers / supervisors, not admins.

### Stage 6 — Station infrastructure dashboard

**Goal:** A station manager opens `/stations/{id}` and sees their physical inventory at a glance — every tank, every pump, every nozzle, every active incident.

- [ ] Replace the Stage-5 placeholder `GET /api/v1/stations/{id}` page (currently just JSON) with a real operational dashboard route at `/stations/{id}`
- [ ] Backend: `GET /api/v1/stations/{id}/overview` — single endpoint that returns the station + its tanks + its pumps (each with nested nozzles) + open incidents. Avoids N+1 from the frontend.
- [ ] Frontend route `/stations/{stationID}` with three regions:
  - **Tanks strip** — one card per tank (product color stripe + capacity)
  - **Pumps strip** — one card per pump, expandable to nozzles
  - **Open incidents** — table with severity badges
- [ ] Sidebar's "Stations" entry already exists; wire the click target to this page (currently 404)
- [ ] Permission gate: `station.read` for the station the user is hitting
- [ ] Empty states for "no tanks yet", "no pumps yet" with deep links to the relevant admin pages

**Done when:** `/stations/MIK-01` renders all seeded tanks + pumps + nozzles correctly wired; a forbidden-station id returns 403 via the existing policy middleware.

---

### Stage 7 — Iconic visuals: tank fill + pump grid

**Goal:** Deliver the signature surfaces from the UI/UX doc (§11.3 Tank View, §11.4 Pump and Nozzle View). This is the moment the product stops looking like an admin tool and starts looking like a command center.

- [ ] `packages/ui/src/components/tank-visual.tsx`:
  - SVG-based vertical fill bar
  - Animated fill on mount (Framer Motion) with product color tinting
  - Safe-min / safe-max markers
  - Ullage badge (capacity minus current)
  - Empty placeholder content for current-volume (filled in Phase 3 from dip readings)
- [ ] `packages/ui/src/components/pump-card.tsx`:
  - Header: pump number + status pulse (green / amber / grey)
  - Body: nozzle rows with product chip + tank source + price
  - Click → pump detail
- [ ] Replace the Stage-6 plain tank cards with `<TankVisual>` on `/stations/{id}`
- [ ] Per-product color tokens already exist in `globals.css` (PMS, Diesel, Kerosene, LPG, Lubricants, AdBlue); `<TankVisual>` accepts a `color` prop that resolves to the right CSS variable
- [ ] Storybook-style mock on `/dev/visuals` *(deferred to a Phase-11+ UI workshop)* — for now, the visuals exist on the live station page only
- [ ] Mobile responsive: tanks stack vertically below 768px; pump cards become single-column

**Done when:** The station dashboard at `/stations/MIK-01` shows animated tank visuals with the right product colors and a clickable pump grid that matches the mood-board screenshots in `docs/ui-ux.md` §11.

---

## Phase 2 acceptance criteria

Phase 2 is complete when **all** of the following are true:

1. The product catalogue is operator-managed via `/settings/products` with visual identity per product.
2. Every station's physical inventory (tanks, pumps, nozzles) is configurable via the admin UI; the demo tenant has its full infrastructure modelled this way.
3. Every nozzle's product matches its tank's product, enforced at the database layer.
4. Tank calibration charts return interpolated volumes via the API for any in-range dip.
5. Pump calibration events and incident transitions go through audit + outbox like every other Phase-1 sensitive write.
6. The station dashboard at `/stations/{id}` renders tank visuals + pump grid with the iconic UI surfaces from `docs/ui-ux.md` §11.

---

## Out of scope for Phase 2 (intentionally)

These are reserved for later phases — don't let scope creep pull them in:

- **Tank dip readings + pump meter readings** — Phase 3 (Station Operations Core)
- **Stock ledger / opening + closing stock** — Phase 4 (Inventory & Reconciliation Engine)
- **Price changes, price history, pricing rules** — Phase 6 (Sales, Payments & Revenue) and a follow-on pricing engine. Phase-2 `nozzle.default_price` is a static field that becomes the seed for the pricing engine; not a live price.
- **Tank gauge / pump controller integrations** — Phase 13 (Hardware & External Integrations)
- **Loss detection / variance scoring** — Phase 10 (Risk, Fraud & Intelligence)
- **Mobile attendant flows** — Phase 14 (Mobile & Offline OS)
- **Reorder forecasting** — Phase 11 (Forecasting & Automation)

---

## Cross-phase considerations

A few decisions in Phase 2 lock shape for later phases:

- **`numeric(14, 3)` for litres** — Phase 4's stock ledger uses the same scale so subtraction doesn't lose precision.
- **`nozzle.product_id` enforced equal to `tank.product_id`** — Phase 6's pump-sale handler can assume the product without re-resolving.
- **Tank calibration is dip-mm to litres, not volume-only** — Phase 3's dip-reading entry stays in the operator's mental model (millimetres on a dipstick, not litres).
- **Pump `meter_decimal_places`** — Phase 3's meter-reading handler validates that the inbound reading matches the configured precision before posting to the stock ledger.

If any of these change, the migration story for Phase 3+ will need careful sequencing.
