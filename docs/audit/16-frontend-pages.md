# Audit 16 — Frontend Dashboard Pages & Feature Components

**Scope:** Every page under `apps/web/src/app/(dashboard)/**` (33 page/layout files, ~8,050 LOC) plus page-local feature components in `apps/web/src/components/*`. The `components/*` tree contains only `auth/` and `layout/` (both out of scope, covered by another audit), so this report is effectively a page-by-page review. Supporting code read for context: `lib/api.ts`, `stores/auth-store.ts`, `stores/tenant-store.ts`, `hooks/use-permissions.ts`, `packages/sdk/src/{client,types}.ts`.

**Method:** Read all pages in full (or in representative slices for the near-identical settings CRUD cluster). Verdicts cite `file:line`. Money is carried from the API as decimal **strings** for most domains (`amount`, `outstanding_amount`, `credit_limit`, `gross_revenue`, `landed_cost_per_litre`, …) but as JS **numbers** for a few (`Product.default_price`/`tax_rate`, shift `expected_cash`/`cash_amount`) — both patterns appear and matter below.

---

## Functional vs Scaffolding Inventory

| Page / route | LOC | Verdict | Notes |
|---|---|---|---|
| `command-center` | 84 | **Scaffolding** | Shows session IDs + literal "No KPIs yet" `EmptyState`. The flagship surface is empty. |
| `customers` | 132 | Functional (read-only) | Lists accounts + credit alerts + scan action. No create/edit despite SDK support. |
| `enterprise` | 131 | Functional | Network metrics, ranking, rebuild action. |
| `enterprise/approvals` | 131 | Functional | Exception queue + approve/reject. |
| `finance` | 153 | Functional | Balance sheet, P&L, journal. Read-only. |
| `finance/close` | 166 | Functional | Checklist + period transitions. |
| `incidents` | 342 | Functional | Filters + create dialog + status advance. |
| `inventory` | 217 | Functional | Per-tank book/physical, variance sparkline. |
| `my-shift` | 383 | Functional | Meter/dip capture + cash submission. Strong. |
| `operations` | 382 | Functional | Day/shift lifecycle, approvals, exceptions. Strong. |
| `procurement` | 323 | Functional (read-only) | PO/receipt/balance dashboards. No PO creation UI. |
| `procurement/receiving` | 484 | Functional | Full receive→invoice→match→approve wizard. Most complex page. |
| `profile` | 214 | Functional | Identity, sessions, password change. |
| `reconciliation` | 345 | Functional | Per-tank run/adjust/seal. Strong. |
| `revenue` | 224 | Functional | Day summary, tender mix, AR aging, close-day. Best-built. |
| `risk` | 139 | Functional | Severity counts + alert resolve + detection. |
| `stations` | 68 | Functional | Grid of station cards. |
| `stations/[stationID]` | 269 | Functional | Shifts/tanks/pumps/incidents. Uses shared `TankVisual`/`PumpCard`. |
| `stations/[stationID]/pumps/[pumpID]` | 292 | Functional | Status change + calibration record/history. |
| `audit` | 187 | Functional | Filterable append-only log. |
| `settings/companies` | 250 | Functional | CRUD dialog. |
| `settings/regions` | 235 | Functional | CRUD (cluster). |
| `settings/stations` | 307 | Functional | CRUD (cluster). |
| `settings/products` | 374 | Functional | CRUD + color. **Float money.** |
| `settings/suppliers` | 336 | Functional | CRUD + deactivate + product links. |
| `settings/tanks` | 421 | Functional | CRUD + capacity sanity checks. |
| `settings/tanks/[tankID]` | 349 | Functional | Calibration chart upload/lookup. Strong. |
| `settings/pumps` | 585 | Functional | Pump + nozzle nested CRUD. Largest page. |
| `settings/users` | 359 | Functional | Invite + role/station grant + suspend. |
| `settings/roles` | 81 | Functional (read-only) | Permission matrix display. |
| `settings/page` | 5 | Plumbing | `redirect('/settings/companies')`. |
| `settings/layout` | 54 | Plumbing | Tab nav. |
| `(dashboard)/layout` | 29 | Plumbing | Shell + `ProtectedRoute`. |

**Quantified verdict:** ~31 of 33 files are real, SDK-wired surfaces with consistent loading/error/empty handling. Exactly **one** page is pure scaffolding (`command-center`, the named "flagship"). The remaining gap is *feature breadth* — several domains are read-only on the frontend (customers, procurement orders) even though the SDK exposes write methods. This is a mature frontend, not a demo skin.

---

## Per-Page / Cluster Walkthrough

### command-center (`command-center/page.tsx`)
The only stub. Renders `user_id`/`tenant_id`/`mfa` from `api.me()` and an `EmptyState title="No KPIs yet"` (line 76-80) with copy referencing "Stage 8"/"Phase 2". The most-trafficked landing page shows nothing actionable. The shell's default route lands users here, so first impression is empty. (PAGE-001)

### Data-heavy read dashboards (finance, enterprise, revenue, risk, inventory, procurement, customers)
These share an excellent pattern: `useQuery` with `{ signal }` cancellation, `LoadingState`/`ErrorState`/`EmptyState`, and **403-aware error copy** ("You don't have the finance.read permission", `finance/page.tsx:56-62`). `revenue/page.tsx` is the gold standard: station `<select>`, `actionError` banner (line 92-96), close-day chained mutation (`computeRevenueDay` → `lockRevenueDay`, line 53-63) with `onError`. 

Weaknesses:
- **String-money via `Number()`** for display: a `money()` helper duplicated verbatim across `customers:20`, `finance:19`, `enterprise:21`, `revenue:21`, `procurement:29`. For pure display this only risks float artifacts in `toLocaleString` (e.g. `"1234567890123.45"` losing low digits), tolerable. The real defect is `procurement/page.tsx:156-164`, which **sums** money strings via `Number(b.outstanding_amount)` then re-stringifies — float accumulation across suppliers can drift cents. (PAGE-002)
- `enterprise/approvals/page.tsx:101` renders `a.amount` raw with no `money()` and no currency symbol; the approve/reject buttons share one `decide.isPending` so clicking one disables the whole queue, and there is **no error surfacing** if a decision fails (line 39-43). (PAGE-005, PAGE-008)
- `risk/page.tsx` `resolve`/`detect` mutations have no `onError` → silent failures; same shared-`isPending`-disables-all-rows issue (line 126). (PAGE-008)

### my-shift, operations, reconciliation
The strongest cluster. All three: `actionError` state populated by every mutation's `onError`, success clears it, queries invalidated on the **scoped** key (`overviewKey = ['…-overview', stationID]`). `reconciliation/page.tsx` gates sealing behind tolerance + shift-approval, validates adjustment input (`canAdjust`, line 226-227), and uses litres-as-number formatting correctly. `my-shift` uses `meter_decimal_places` to set input `step` (line 124) and disables Save when empty (double-submit guard). `operations` drives the full day→shift→close→approve lifecycle. Money in these is the `number`-typed `expected_cash`/`variance` (SDK contract), formatted via `fmtMoney` — consistent within the page.

### procurement/receiving
The most ambitious page (484 LOC): a 3-card wizard (receive goods → record invoice → resolve/approve) with five chained mutations, all with `onError` → `submitError`. **Correctly sends money as strings** (`freight_amount: freight || '0'`, `unit_price: invoicePrice || selectedLine.unit_price`, line 132-160) — only litres are `Number()`. Cascading `useEffect` auto-selection of PO/line/tank (line 91-112) is intricate; a stale `lineID` guard exists but the chained effects could thrash on fast station switches. Minor: `recordInvoice` doesn't require a prior receipt, and `lastReceipt`/`invoice` are local state not refetched, so a page reload loses the wizard position.

### stations + pump detail
`stations/[stationID]/page.tsx` cleanly composes shared `TankVisual`/`PumpCard` from `@fuelgrid/ui` and builds a nozzle-label map. The pump detail page (`pumps/[pumpID]/page.tsx`) has two solid forms (status change, calibration record) with per-form error state. Two defects: (a) line 289 renders `Station: {stationID}` — a raw UUID dumped as debug-grade UI; (b) the "Back" link goes to `/settings/pumps` (line 102) even though the page was reached from `/stations/{id}` — broken mental-model routing. (PAGE-006, PAGE-007)

### audit
Clean draft-vs-applied filter split (`filters` vs `appliedFilters`, line 41-42) so typing doesn't refetch; converts `datetime-local` → ISO. Hardcoded `limit: 100` with no pagination/"load more" (line 54) — silently truncates. (PAGE-009)

### profile
Sessions table + revoke + password change. `revoke` mutation has **no error surfacing** (line 38-41). Password form claims "Minimum 12 characters" (line 162) but only validates "both fields required" (line 169) — the length rule is server-only, so users hit a round-trip error for something checkable client-side. (PAGE-010)

### Settings CRUD cluster (companies, regions, stations, products, suppliers, tanks, pumps, users)
A single copy-pasted template: `useQuery` list → `Table` → `Dialog` form → `create`/`update` mutations invalidating one key. Consistency is high but so is **drift risk** — eight near-identical ~250-400 LOC files with no shared `<CrudPage>` / `<EntityForm>` abstraction (PAGE-003). Cross-cutting findings:

- **No zod / react-hook-form anywhere in scope.** The stack advertises `react-hook-form + zod`, but the only usage is `components/auth/login-form.tsx` (out of scope). Every settings/dialog form is hand-rolled `useState` + a one-line `if (!x) setError('… required')`. No field-level errors, no schema reuse with the SDK. (PAGE-004)
- **Products use float money.** `Product.default_price`/`tax_rate` are typed `number` in the SDK (`types.ts:84-85`); the form does `Number(input.default_price)` (`products:77`) and displays `p.default_price.toFixed(2)` (`products:193`). Prices are floats end-to-end — a genuine money-precision liability for a fuel-pricing field, distinct from the string-money domains. (PAGE-011)
- **Users scope dialog goes stale.** `settings/users/page.tsx` captures `scope` (a `UserSummary`) at open time (line 92-94). The role/station toggle buttons read `scope.roles.includes(...)` (line 289) but after `grantRole`/`revokeRole` invalidate `['users']`, the captured `scope` object is **not** updated — chips don't reflect the toggle until the dialog is closed and reopened. The grant/revoke/status mutations also have **no `onError`** (line 62-90) → silent failures. (PAGE-012, PAGE-008)
- `settings/roles` is intentionally read-only (system roles) — fine, clearly labeled (line 38-40).

### Permission-gated UI — the single biggest systemic gap
`hooks/use-permissions.ts` defines a **correct, well-documented** `usePermission(code, { stationID })` that mirrors `policy.Can()` (loading→`null`, station-scoping, tenant-wide short-circuit). **Zero dashboard pages import it** (grep for `usePermission`/`hasPermission` across `(dashboard)/**` → no matches). Consequence: *every* action button (Approve, Lock period, Seal, Invite user, Suspend, Rebuild projections, Delete) renders for **all** users; the UI relies entirely on the API returning 403 after the click. Several pages decode 403 in `ErrorState` for *reads*, but no mutation control is hidden/disabled by permission. This is a UX defect (users see buttons they can't use and only learn on failure) and a polish/least-surprise gap, not a security hole (backend is authoritative). (PAGE-013)

### Auth/tenant stores
`auth-store` holds only `token`/`expiresAt` — no cached roles/permissions, reinforcing the above. `tenant-store` exposes `activeStationID`/`activeRegionID`/`activeCompanyID`, but **no in-scope page uses it** — instead inventory/operations/revenue/reconciliation/procurement each re-implement a local `useState('')` station selector with an identical `useEffect` defaulting to `items[0]` (e.g. `inventory:38-41`, `operations:48-53`, `revenue:36-39`). Five+ copies of the same selector logic, none sharing the global station context the store was built for. (PAGE-014)

---

## Findings

| ID | Severity | File:Line | Issue | Fix |
|---|---|---|---|---|
| PAGE-013 | **High** | `app/(dashboard)/**` (all); `hooks/use-permissions.ts` unused | No page uses the existing `usePermission` hook; every mutation button shows to everyone, relying on API 403 post-click. | Wrap/disable action controls with `usePermission(code, { stationID })`; hide or disable + tooltip when `false`. |
| PAGE-011 | **High** | `settings/products/page.tsx:77,193`; SDK `types.ts:84` | Product `default_price`/`tax_rate` are JS floats end-to-end — money precision risk on a pricing field. | Carry product price as a decimal string like other money domains; format without `Number`/`toFixed`. |
| PAGE-002 | **High** | `procurement/page.tsx:156-164` | Sums money **strings** via `Number(b.outstanding_amount)` then re-stringifies — float accumulation can drift cents across suppliers. | Sum with a decimal-safe reducer (integer cents or decimal lib); or have the API return the aggregate. |
| PAGE-008 | **High** | `risk:42-48,126`; `enterprise/approvals:39-43`; `settings/users:62-90`; `profile:38-41` | Multiple mutations have **no `onError`** → silent failures (resolve, approve/reject, grant/revoke role & station, suspend, revoke session). | Add `onError` → visible banner/toast on each, matching the `actionError` pattern used in operations/revenue. |
| PAGE-001 | **Medium** | `command-center/page.tsx:76-80` | The "flagship" default landing page is pure scaffolding ("No KPIs yet"). | Wire to enterprise/revenue overview SDK calls or redirect to a populated surface until built. |
| PAGE-004 | **Medium** | all settings & dialog forms | Stack-mandated `zod`/`react-hook-form` unused; forms hand-roll `useState` + single-condition validation, no field errors. | Introduce shared zod schemas (reuse SDK request types) + RHF for consistent, field-level validation. |
| PAGE-003 | **Medium** | `settings/{companies,regions,stations,products,suppliers,tanks,pumps,users}` | 8 copy-pasted ~250-585 LOC CRUD files; high drift risk, no shared abstraction. | Extract a generic `CrudResource`/`EntityDialog` to centralize list/table/form/error logic. |
| PAGE-014 | **Medium** | `inventory:38-41`, `operations:48-53`, `revenue:36-39`, `reconciliation:36-39`, `procurement:63-66` | Station-selector `useState`+`useEffect` duplicated 5×; the global `tenant-store.activeStationID` is never used. | Extract `useActiveStation()` backed by `tenant-store` so selection persists across pages. |
| PAGE-012 | **Medium** | `settings/users/page.tsx:92-94,289` | Scope dialog reads a `scope` snapshot captured at open; role/station chips don't update after grant/revoke until reopened. | Re-derive `scope` from the live `users` query data each render (find by id), not a captured object. |
| PAGE-005 | **Medium** | `enterprise/approvals/page.tsx:101` | Approval `amount` rendered raw — no money formatting, no currency. | Format with the shared money helper + currency code. |
| PAGE-006 | **Low** | `stations/[stationID]/pumps/[pumpID]/page.tsx:289` | Raw `Station: {stationID}` UUID printed as debug-grade UI. | Remove or render station name via lookup. |
| PAGE-007 | **Low** | `stations/[stationID]/pumps/[pumpID]/page.tsx:102` | "Back" goes to `/settings/pumps`, not the originating `/stations/{id}`. | Link back to the station, or use `router.back()`. |
| PAGE-010 | **Low** | `profile/page.tsx:162,169` | "Minimum 12 characters" advertised but only "both required" validated client-side. | Add length check before submit to avoid a server round-trip. |
| PAGE-009 | **Low** | `audit/page.tsx:54` | Hardcoded `limit: 100`, no pagination — silently truncates results. | Add cursor/"load more"; or surface a "showing first 100" notice. |
| PAGE-015 | **Low** | `finance/page.tsx:40-45` | "Period close →" uses raw `<a href>` (full reload) instead of Next `<Link>`. | Replace with `<Link href="/finance/close">`. |
| PAGE-016 | **Low** | `finance/close/page.tsx:37` | `PeriodAction` includes `'reopen'` but no button ever triggers it (dead branch). | Add the reopen control or drop the unused action. |
| PAGE-017 | **Info** | `customers`, `procurement` | Read-only surfaces despite SDK `createCustomer`/`createPurchaseOrder` etc. — feature gap, not a bug. | Add create/edit flows when the workflow is prioritized. |
| PAGE-018 | **Info** | settings selects (`incidents`, `revenue`, …) | Native `<select>` for station/status — accessible and labeled (good); just noting they bypass the design-system control if one exists. | Optional: adopt a `Select` from `@fuelgrid/ui` for visual consistency. |

### Severity counts
- **Critical:** 0
- **High:** 4 (PAGE-013, PAGE-011, PAGE-002, PAGE-008)
- **Medium:** 6 (PAGE-001, PAGE-004, PAGE-003, PAGE-014, PAGE-012, PAGE-005)
- **Low:** 6 (PAGE-006, PAGE-007, PAGE-010, PAGE-009, PAGE-015, PAGE-016)
- **Info:** 2 (PAGE-017, PAGE-018)

### Top-5 Risks
1. **Permission-blind UI (PAGE-013):** a correct `usePermission` hook exists but is unused everywhere; all action buttons render for all users, learning via 403 only after clicking.
2. **Float money on products (PAGE-011):** fuel pricing carried as JS floats end-to-end (`products:77,193`) — precision liability on the most price-sensitive entity.
3. **String-money summed via `Number()` (PAGE-002):** `procurement:156-164` accumulates decimal-string balances as floats, risking cent drift.
4. **Silent mutation failures (PAGE-008):** role grant/revoke, station scope, approve/reject, risk resolve, suspend, and session revoke have no error surfacing — failures look like no-ops.
5. **Empty flagship + CRUD duplication (PAGE-001/PAGE-003/PAGE-004):** the default landing page is a stub, and eight hand-rolled CRUD pages (no zod/RHF, no shared abstraction) carry significant drift and validation-consistency risk.

**Overall:** The dashboard is substantially real, not cosmetic — strong, consistent loading/error/empty handling and good money discipline in the *string*-money domains (revenue, receiving). The headline issues are (1) an unwired permission layer, (2) two concrete money-precision defects, and (3) systemic silent-failure + form-validation gaps from skipping the project's own zod/RHF/permission tooling.
