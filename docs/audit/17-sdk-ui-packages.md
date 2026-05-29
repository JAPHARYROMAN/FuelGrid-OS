# Audit 17 — Shared SDK & UI Packages

**Scope:** Read-only, atomic-level audit of FuelGrid OS shared packages `@fuelgrid/sdk` and `@fuelgrid/ui`. No source was modified; the only write is this report.

**Files & LOC covered:**

| File | LOC |
|---|---|
| `packages/sdk/src/client.ts` | 2953 |
| `packages/sdk/src/index.ts` | 126 |
| `packages/sdk/src/types.ts` | 1343 |
| `packages/sdk/package.json` | 18 |
| `packages/ui/src/index.ts` | 45 |
| `packages/ui/src/lib/cn.ts` | 11 |
| `packages/ui/src/components/badge.tsx` | 32 |
| `packages/ui/src/components/button.tsx` | 47 |
| `packages/ui/src/components/card.tsx` | 58 |
| `packages/ui/src/components/dialog.tsx` | 89 |
| `packages/ui/src/components/input.tsx` | 22 |
| `packages/ui/src/components/label.tsx` | 18 |
| `packages/ui/src/components/pump-card.tsx` | 111 |
| `packages/ui/src/components/states.tsx` | 92 |
| `packages/ui/src/components/table.tsx` | 66 |
| `packages/ui/src/components/tank-visual.tsx` | 180 |
| `packages/ui/package.json` | 32 |
| `packages/{sdk,ui}/tsconfig.json` + `packages/config/tsconfig.base.json` | — |

**Total: ~5,200 LOC.** Both packages export `./src` directly (no build step); `verbatimModuleSyntax`, `strict`, and `noUncheckedIndexedAccess` are all enabled via `@fuelgrid/config/tsconfig/base`. No test files exist anywhere under `packages/**`.

---

## SDK section

### `request<T>` (client.ts:158–208)

The transport core is small and mostly sound (Accept header, conditional Authorization, FormData boundary handling, AbortSignal threading, 204 short-circuit), but it has several real defects:

**SDK-01 (High) — `return parsed as T` is an unchecked cast (client.ts:207).** There is zero runtime validation anywhere in the SDK. Every endpoint method's return type is a *compile-time fiction*: the server could return any shape and the SDK hands it back typed as, e.g., `Tank`. Combined with the hand-maintained `types.ts` (whose own header at types.ts:1–5 admits "mismatches are not the compiler's job to catch yet"), this is a type-safety illusion. A `zod`/`valibot` schema or even a dev-mode shape assertion would close the gap. The absence of SDK tests (see SDK-02) means nothing exercises this path.

**SDK-02 (High) — No SDK tests exist.** There is not a single `*.test.ts` under `packages/`. The recently-fixed "Illegal invocation" bug (fetch bound to the wrong `this`) would have been caught instantly by one test that constructs a `Client` against a mock fetch and calls any method. The whole 2953-line client is unverified.

**SDK-03 (Medium) — Network/abort errors escape as raw `TypeError`/`DOMException`, not `SdkError` (client.ts:184–190).** `this.fetchImpl(...)` is not wrapped in try/catch. A dropped connection throws a bare `TypeError: Failed to fetch`; an aborted request throws an `AbortError` `DOMException`. Callers that branch on `instanceof SdkError` (the documented contract at client.ts:104–106) will miss every network failure and abort. Either wrap and rethrow as `SdkError` (with a sentinel status like 0), or document that callers must also catch non-`SdkError` throwables.

**SDK-04 (Medium) — Non-JSON / malformed success bodies silently become `null` (client.ts:196–207).** `safeParse` (client.ts:2947–2953) returns the raw *string* on `JSON.parse` failure, and `text ? safeParse(text) : null` returns `null` for an empty 200 body. So: (a) an empty-but-not-204 success returns `null as T` (a typed lie — callers expecting an object get `null`); (b) an HTML error page (e.g. a 200 from a misconfigured proxy) is returned as a `string` cast to `T`. No content-type check, no guard. SDK-01 makes this invisible until a runtime crash downstream.

**SDK-05 (Low) — `safeParse` swallows the parse error on error responses (client.ts:199–204, 2947–2953).** On `!res.ok`, if the body isn't JSON, `parsed` is the raw string; the `'error' in parsed` check fails (string has no `'error'`), so the message falls back to `HTTP {status}` and the actual server text is buried in `SdkError.body` as an unstructured string. Acceptable, but the error message loses fidelity.

**SDK-06 (Info) — `credentials: 'omit'` (client.ts:189).** Correct for a bearer-token client (no cookies), but worth flagging: any future cookie-based CSRF/session mechanism will silently not work. Intentional given `getToken`, so Info only.

**SDK-07 (Info) — Per-call header merge can clobber Authorization deliberately (client.ts:180–182).** `Object.assign(headers, opts.headers)` runs last, which is how `createTenant` (client.ts:233) and the platform token override work. Fine, but note that a caller-passed `Content-Type` would override the JSON default and break a JSON body — undocumented foot-gun.

### Money / litre / amount typed as `number` — the decimal-string violation

**SDK-08 (High) — 15 request-body parameters type money/litre/rate values as `number`, forcing JS-float arithmetic on values the API models as decimal strings.** The convention is *known* to the codebase: the very same request bodies type `unit_price`, `freight_amount`, `duty_amount`, `levies_amount`, and `amount` as `string` (client.ts:572–575, 616–617), and `transferStock` types `litres: string` (client.ts:2583) while `central-procurement-plans` types `target_litres: string` (client.ts:2561). That `litres` is `string` in one method and `number` in another is the smoking gun that the `number` typings are bugs, not a design choice. Full census of offending **request params**:

| # | Line | Method | Field | Kind |
|---|---|---|---|---|
| 1 | 525 | `createPurchaseOrder` | `ordered_litres` | litre |
| 2 | 541 | `updatePurchaseOrder` | `ordered_litres` | litre |
| 3 | 569 | `receivePurchaseOrderReceipt` | `volume_litres` | litre |
| 4 | 570 | `receivePurchaseOrderReceipt` | `dip_before_litres` | litre |
| 5 | 571 | `receivePurchaseOrderReceipt` | `dip_after_litres` | litre |
| 6 | 615 | `recordSupplierInvoice` | `invoiced_litres` | litre |
| 7 | 673 | `createTank` | `capacity_litres` | litre |
| 8 | 843 | `createNozzle` | `default_price` | **money** |
| 9 | 1031 | `captureMeterReading` | `reading` | litre (meter) |
| 10 | 1043 | `correctMeterReading` | `reading` | litre (meter) |
| 11 | 1110 | `submitCash` | `cash_amount` | **money** |
| 12 | 1111 | `submitCash` | `mobile_money_amount` | **money** |
| 13 | 1112 | `submitCash` | `card_amount` | **money** |
| 14 | 1113 | `submitCash` | `litres`→`credit_amount` | **money** |
| 15 | 1188 | `adjustReconciliation` | `litres` | litre |

**Count = 15 money/litre request-body params typed as `number`, across 8 methods.** Four of these (`submitCash`, lines 1110–1113) are direct cash figures — the highest-risk because float rounding on cash submission directly corrupts shift reconciliation. `createNozzle.default_price` (843) is a pump price; `setPrice` two screens away correctly uses `unit_price: string` (client.ts:1211), so the same product price is string in one method and number in another.

Borderline (NOT counted): `dip_mm`/`water_mm`/`temperature_c` (client.ts:1066–1068, 1082) are physical mm/°C measurements; `calibratedVolume(dipMM: number)` (727) is a query-param measurement; `payment_terms_days`, `grace_days`, `required_approvals`, `meter_decimal_places`, `limit`, and all `count`/`created`/`rows` figures are genuine integers. These are defensible as `number`.

**SDK-09 (High) — `types.ts` response interfaces carry the same money/litre-as-`number` rot, and within a single interface mix string and number.** Examples: `Product.default_price: number` (types.ts:84) and `tax_rate`/`loss_tolerance_percent: number`; `PurchaseOrderLine.ordered_litres`/`received_litres: number` (111,113) while `unit_price: string` (112); `Delivery.volume_litres`/`dip_*_litres: number` (145–148) **while `freight_amount`/`duty_amount`/`levies_amount`/`landed_cost_*: string` in the same interface** (150–154); `StockMovement.litres`/`balance_after: number` (169–170); `Tank.capacity_litres`/`safe_min_litres`/`safe_max_litres`/`dead_stock_litres`/`current_litres: number` (263–266,273); `Nozzle.default_price: number` (301); `SupplierInvoiceLine.invoiced_litres: number` (208) vs `unit_price`/`amount: string` (209–210). Parsing a decimal-string API response into these `number` fields will lose precision on large litre volumes and any non-terminating decimal. These response-type mismatches are arguably worse than the request ones because they silently corrupt *display* values.

### Endpoint methods — typing & consistency

**SDK-10 (Medium) — 13 methods return `{ items: unknown[]; count: number }`, `Record<string, unknown>`, or `unknown`, defeating type safety for whole feature areas.** Offenders: `listFuelLimits` (1689), `addStationGroupMember`→`unknown` (2401), `listApprovalPolicies` (2433), `listProcurementPlans` (2554), `listRiskSignals` (2651), `listRiskRules` (2656), `listRiskScores` (2731), `listInvestigations` (2745), `getInvestigation`→`Record<string,unknown>` (2750), `createInvestigation` (2757), `attachAlertToInvestigation`→`unknown` (2761), `setInvestigationStatus`→`Record<string,unknown>` (2808), `listRiskSuppressions` (2822). Several of these have real domain types available (`RiskAlert` is imported and used elsewhere), so the `unknown[]` is laziness, not a missing type. Consumers of investigations/risk-rules get no IntelliSense and no compile-time field checks.

**SDK-11 (Low) — `reading` capture is inconsistently typed across two reading endpoints.** `captureMeterReading`/`correctMeterReading` use `reading: number` (1031,1043) but `recordOdometerReading` uses `reading: string` (1724). Same verb, same concept, opposite type — reinforces that the `number` choice is accidental.

**SDK-12 (Low) — `createTank`/`createCompany`/`updateNozzle` use `Partial<T> & {…}` / `Pick<T,…>` intersections that can conflict with `types.ts`.** `createTank(req: Partial<Tank> & { …; capacity_litres: number })` (667–674): `Tank.capacity_litres` is already `number` (types.ts:263) so it happens to agree — but the moment SDK-09 is fixed (Tank → string), this intersection becomes `string & number = never` and breaks silently. The `Partial<Tank>` spread also lets callers send read-only server fields (`id`, `tenant_id`, `created_at`) that the API will reject — the request type is looser than the endpoint accepts.

**SDK-13 (Info) — Query-param building is inconsistent but functionally correct.** Three idioms coexist: `URLSearchParams` (e.g. listPurchaseOrders 503–508, getFleetConsumption 1743), manual `?key=${encodeURIComponent(...)}` (listTanks 659, listCustomers, price-history 1238), and ternary `${q ? '?'+q : ''}`. All paths and the manual query values use `encodeURIComponent`, so no injection/breakage found. The inconsistency is a maintenance smell only. `${action}` interpolations (e.g. 1486, 1681, 2549, 2599, 2712) are unencoded but constrained to literal-union args, so safe.

**SDK-14 (Info) — `index.ts` re-export list is hand-maintained and partially drifts from `client.ts` imports.** `index.ts` exports types like `RevenueSummary`, `RevenueTenderBreakdown`, `MyShiftNozzle`, `OperationsAttendant` etc. that `client.ts` does not import; conversely both lists are long and ordered ad-hoc. Not a bug, but a generated barrel (or `export type * from './types'`) would eliminate the drift risk.

---

## UI section

### a11y

**SDK-15 (Medium) — `PumpCard` role=button has no accessible name and an unsafe non-null assertion (pump-card.tsx:50–62).** When interactive, the root `div` gets `role="button"` + `tabIndex={0}` + Enter/Space handling — good — but there is **no `aria-label`**; a screen reader announces an empty button. The visible "Pump {number}" text is inside, but it's not wired as the accessible name for the role=button container, and the nested content (product names, prices) will be read as the button label, which is noisy. Add `aria-label={`Pump ${number}`}`. Also `onActivate!()` at line 59 is a non-null assertion that is only safe because the handler is only attached when `interactive` is true — brittle if refactored.

**SDK-16 (Low) — `Input` and `Label` are not associated; `Label` lacks `htmlFor` plumbing.** `Label` (label.tsx) is a bare `<label>` with `peer-disabled:` styles, and `Input` (input.tsx) forwards `id` but nothing in the library links them. The `peer` Tailwind class on Label implies a sibling-`peer` pattern, but `Input` does not set `className="peer"`, so `peer-disabled:` on the label never fires. Consumers must manually pair `htmlFor`/`id`; easy to forget, and there is no `aria-invalid`/`aria-describedby` support for error states — a gap for a form-heavy app.

**SDK-17 (Info) — Dialog focus trap & labelling delegated to Radix (dialog.tsx).** `DialogContent` wraps `@radix-ui/react-dialog`, which provides focus trapping, `aria-modal`, and Escape handling for free. The Close button has `aria-label="Close"` (49). One soft gap: Radix warns at runtime if no `DialogTitle`/`Description` is rendered, but the library does not enforce it; a consumer can ship an unlabelled dialog. Info only.

**SDK-18 (Low) — `Table` is presentationally fine but offers no `<caption>`/scope affordance (table.tsx).** `TableHead` renders `<th>` without `scope="col"`. Semantically a screen reader can usually infer it, but for a data-dense fuel app, explicit `scope` on header cells (or a prop) would improve navigation. The `overflow-x-auto` wrapper div (7) is not focusable, so keyboard users cannot scroll a wide table horizontally without a mouse — add `tabIndex={0}` + `role="region"` + `aria-label` to the scroll container.

### Domain components — logic & math

**SDK-19 (High) — `TankVisual` and `PumpCard` consume money/litres as `number`, hardcoding the SDK-08/09 precision bug into the render layer.** `TankVisualProps` types `capacityLitres`, `safeMinLitres`, `safeMaxLitres`, `currentLitres: number` (tank-visual.tsx:13–22) and `PumpCardNozzle.price: number` (pump-card.tsx:13). `PumpCard` then calls `n.price.toFixed(2)` (pump-card.tsx:104) — if a caller passes the API decimal string it crashes (`string.toFixed` is not a function); if they pre-convert with `Number()`, they lose precision. The components *force* the float conversion. They should accept strings and format with `Intl.NumberFormat`.

**SDK-20 (Medium) — `TankVisual` divides by capacity without guarding zero/negative, and clamps silently (tank-visual.tsx:39–62).** `levelY` guards `capacity > 0 ? litres/capacity : 0` (40) but `fillFrac = clamp01(currentLitres! / capacityLitres)` (56) does **not** — if `capacityLitres` is 0 (a misconfigured tank), this is `NaN`, and `clamp01(NaN)` returns `Math.max(0, Math.min(1, NaN)) = NaN`, producing `fillH = NaN` and a broken SVG rect. Also `ullage = max(0, capacity - current)` (62) can't go negative but an over-full tank (current > capacity) silently renders a 100% fill with no overflow indicator — for a fuel tank, an over-capacity reading is exactly the anomaly you want surfaced, not hidden.

**SDK-21 (Low) — `TankVisual` safe-min/safe-max marker lines render even when the values are 0 or exceed capacity (tank-visual.tsx:60–61,137–141).** `levelY(safeMaxLitres, capacity)` will place the "max" line at the very top (y=BODY_TOP) if `safeMaxLitres >= capacity`, and at the bottom if a field is 0/unset — with no visual distinction from a legitimate marker. There is no validation that `safeMin < safeMax <= capacity`. Misconfigured tanks produce misleading dashed lines.

**SDK-22 (Info) — `PumpCard.pulseClasses` and `TankVisual` status use hardcoded string literals (pump-card.tsx:27–37, tank-visual.tsx:171).** Statuses `'active'`/`'maintenance'`/`'inactive'`/`'decommissioned'` are string-compared inline; the `default` branch greys out anything unrecognised. No shared enum/const with the SDK's status values, so a server-side status rename would silently fall through to grey without a type error. Low-risk but a drift vector.

### Library plumbing

**SDK-23 (Low) — `Badge` is a plain function component, not `forwardRef`, breaking the library's own convention (badge.tsx:28).** Every other primitive (`Button`, `Input`, `Label`, `Card*`, `Table*`, `Dialog*` content) uses `React.forwardRef`. `Badge` and the `states.tsx` components do not, so a consumer cannot attach a ref (e.g. for a tooltip anchor or measurement). `DialogHeader`/`DialogFooter` (dialog.tsx:57,86) are likewise plain functions with a `.displayName` set on a non-forwardRef function (harmless but pointless). Inconsistent.

**SDK-24 (Info) — `cn` util is correct (lib/cn.ts).** `twMerge(clsx(inputs))` is the standard shadcn pattern; conflict-dedupe works as documented. No defect.

**SDK-25 (Info) — `states.tsx` components are sound.** `LoadingState` has `role="status"` + `aria-live="polite"`, `ErrorState` has `role="alert"`. Good a11y. Minor: `EmptyState` has no role (acceptable — it's not a live region).

### Packaging & versioning

**SDK-26 (High) — `@fuelgrid/ui` declares a `./styles` export pointing to `./src/styles.css`, but that file does not exist (ui/package.json:10).** `find packages/ui -name "*.css"` returns nothing. Any consumer doing `import '@fuelgrid/ui/styles'` (the documented styling entry) gets a module-not-found error at build time. The components reference Tailwind tokens (`bg-accent`, `text-card-foreground`, `border-border`, `text-danger`, etc.) that must be defined somewhere — the missing stylesheet means the package ships unstyled unless the host app happens to define every token. This is a shipping defect.

**SDK-27 (Medium) — Both packages are version `0.0.0` and `src`-exported with no build step (sdk/package.json:3,6–10; ui/package.json:3).** `"main"`/`"types"`/`"exports"` all point at raw `./src/*.ts(x)`. Implications: (a) every consumer must transpile the packages themselves (fine inside the Next.js monorepo via `transpilePackages`, fatal for any external/SSR consumer that doesn't); (b) `0.0.0` everywhere means no semver signal — consumers cannot pin or detect breaking changes, and `workspace:*` masks it internally; (c) `declaration`/`declarationMap` in the base tsconfig are pointless with `noEmit:true` and no `dist`. For an internal monorepo this is a deliberate tradeoff, but it locks the SDK/UI to the Next build and blocks publishing.

**SDK-28 (Low) — `framer-motion` is a hard `dependency` of `@fuelgrid/ui` pulled in solely by `TankVisual` (ui/package.json:20, tank-visual.tsx:4).** Any consumer importing *any* UI component transitively bundles framer-motion even if they never render a tank. With `src`-export + tree-shaking it may drop, but the `'use client'` + named import pattern makes that fragile. Consider lazy-loading the motion import inside `TankVisual` or making it a peer/optional dep.

---

## Severity summary

| Severity | Count | IDs |
|---|---|---|
| Critical | 0 | — |
| High | 6 | SDK-01, SDK-02, SDK-08, SDK-09, SDK-19, SDK-26 |
| Medium | 6 | SDK-03, SDK-04, SDK-10, SDK-15, SDK-20, SDK-27 |
| Low | 8 | SDK-05, SDK-11, SDK-12, SDK-16, SDK-18, SDK-21, SDK-23, SDK-28 |
| Info | 8 | SDK-06, SDK-07, SDK-13, SDK-14, SDK-17, SDK-22, SDK-24, SDK-25 |
| **Total** | **28** | |

## Top-5 risks

1. **SDK-08 / SDK-09 — Money & litres typed as `number` (15 request params + pervasive in `types.ts`).** Float arithmetic on cash (`submitCash`), prices (`default_price`), and litre volumes will produce reconciliation drift and display-precision errors in a financial system. The codebase already types most amounts as `string`, proving these are bugs.
2. **SDK-26 — `@fuelgrid/ui` `./styles` export targets a non-existent `src/styles.css`.** A documented import path is broken; the component library ships effectively unstyled / build-breaking for any consumer using the styles entry.
3. **SDK-01 + SDK-02 — `request<T>` does `parsed as T` with zero runtime validation and zero tests.** The entire 2953-line typed surface is a compile-time illusion over un-validated JSON; the just-fixed "Illegal invocation" bug proves nothing exercises the transport.
4. **SDK-19 — `PumpCard`/`TankVisual` force money/litre to `number` and call `.toFixed()` (pump-card.tsx:104).** Hardcodes the precision bug into rendering; passing the API's decimal string throws at runtime.
5. **SDK-03 / SDK-04 — Transport error handling gaps.** Network/abort errors escape as raw `TypeError`/`AbortError` (not `SdkError`), and non-JSON or empty-non-204 success bodies silently resolve to `null`/raw-string cast as `T`, surfacing as confusing downstream crashes.

**Money/litre-as-`number` SDK request parameters found: 15** (across 8 methods; 5 are cash/price money fields, 10 are litre/meter quantities).
