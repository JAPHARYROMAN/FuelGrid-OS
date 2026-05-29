# Phase 1-10 Deep Web App Audit Report

Date: 2026-05-28
Scope: FuelGrid OS web application and its Phase 1-10 backend/API support
Workspace: `C:\projects\Actual Projects\fuelGrid os`

## Executive Verdict

FuelGrid OS has a much larger implemented backend than the current web application exposes. Phases 1-10 compile, the production Next build succeeds, Go tests pass, and the Phase 2-10 DB-backed integration suite passes against a freshly migrated database. That is a strong base.

The application is not release-clean as a full Phase 1-10 web app. The backend has routes and domain behavior for procurement, revenue, finance, customer/fleet credit, enterprise command, and risk intelligence, but the browser UI is still concentrated around overview pages and a small number of action queues. Important end-user workflows promised by Phases 7-10 are not available as atomic web screens. The app should be described as "backend-complete with partial operational UI" rather than "Phase 1-10 product-complete."

No P0 runtime-crashing defect was found in the reviewed code. The main release blockers are dead navigation routes, incomplete web coverage for major Phase 8-10 workflows, password reset token leakage to logs, a hand-maintained API contract that stops before Phase 4+, and session handling that persists bearer tokens in localStorage without client-side expiry or global 401 handling.

Recommended gate: freeze roadmap expansion and resolve the P1 issues before presenting Phase 1-10 as complete to operators, customers, auditors, or enterprise stakeholders.

## Findings Index

| ID | Severity | Finding | Primary evidence |
| --- | --- | --- | --- |
| F01 | P1 | Main navigation contains live-looking links to routes that do not exist | `apps/web/src/components/layout/sidebar.tsx:40` through `:53`; Next build route list |
| F02 | P1 | Phase 8 customer/fleet UI is only a thin list and alert scan, not the roadmap workflow set | `apps/web/src/app/(dashboard)/customers/page.tsx:32`; API routes in `services/api/internal/server/server.go:406` through `:490` |
| F03 | P1 | Phase 9 enterprise UI exposes dashboards and approvals but not hierarchy, scopes, central pricing, central procurement, stock transfers, consolidated finance, or exports | `apps/web/src/app/(dashboard)/enterprise/page.tsx:25`; `services/api/internal/server/server.go:496` through `:555` |
| F04 | P1 | Phase 10 risk UI exposes detection and one-click resolve, but not rules, signals, investigations, suppressions, governance, evidence, or score explanations | `apps/web/src/app/(dashboard)/risk/page.tsx:24`; `services/api/internal/server/server.go:557` through `:613` |
| F05 | P1 | Password reset tokens are logged whenever an account is found; runtime environment is not checked | `services/api/internal/server/auth_handlers.go:113` through `:120` |
| F06 | P1 | OpenAPI is valid but does not document Phase 4-10 routes | `docs/openapi.yaml`; route table continues through Phase 10 in `services/api/internal/server/server.go` |
| F07 | P1 | Auth tokens are persisted in localStorage, expiry is not enforced in guards, and SDK has no global 401/logout path | `apps/web/src/stores/auth-store.ts:20` through `:39`; `apps/web/src/components/auth/protected-route.tsx:24` through `:43`; `packages/sdk/src/client.ts:199` through `:204` |
| F08 | P2 | Several high-impact mutations have no confirmation, typed reason, or evidence capture | Revenue, finance close, enterprise approvals, projection rebuild, risk detection, alert resolve |
| F09 | P2 | Command palette and insight panel are still placeholders despite later phases being present | `apps/web/src/components/layout/command-palette.tsx:56` through `:60`; `apps/web/src/components/layout/right-panel.tsx:5` through `:20` |
| F10 | P2 | Mobile navigation is absent: the only primary nav is hidden below `lg` | `apps/web/src/components/layout/sidebar.tsx:68` |
| F11 | P2 | Monetary values are repeatedly coerced through JavaScript `Number`, risking precision/format drift | `apps/web/src/app/(dashboard)/customers/page.tsx:20`; `finance/page.tsx:20`; `revenue/page.tsx:21`; `enterprise/page.tsx:20` |
| F12 | P2 | Permission-aware UX is inconsistent; most pages wait for API 403 instead of hiding or disabling controls by permission | `apps/web/src/hooks/use-permissions.ts`; action pages under `apps/web/src/app/(dashboard)` |
| F13 | P2 | Finance UI does not expose most Phase 7 finance operations despite backend support | `apps/web/src/app/(dashboard)/finance/page.tsx:25`; `server.go:628` through `:750` |
| F14 | P2 | Period close UI has no reopen control and globally disables lock based on a tenant checklist rather than per-period intent | `apps/web/src/app/(dashboard)/finance/close/page.tsx:37`; `:140` through `:147` |
| F15 | P2 | Risk alert resolution is hardcoded to `disposition: reviewed` with no investigation or evidence workflow | `apps/web/src/app/(dashboard)/risk/page.tsx:42` through `:44` |
| F16 | P2 | Advanced SDK methods are partly typed as `unknown` / `Record<string, unknown>`, weakening TypeScript protection exactly where Phase 8-10 complexity is highest | `packages/sdk/src/client.ts:1686`; `:2401`; `:2651`; `:2745` through `:2808` |
| F17 | P2 | Security headers are not configured in the web app or API middleware reviewed | `apps/web/src/app/layout.tsx:8` through `:11`; `apps/web/next.config.ts`; `services/api/internal/server/server.go:149` through `:162` |
| F18 | P2 | Linting stack uses deprecated `next lint` and does not load the Next ESLint plugin | `apps/web/package.json:10`; build/lint output |
| F19 | P3 | `pnpm format:check` currently fails on procurement pages | `apps/web/src/app/(dashboard)/procurement/page.tsx`; `apps/web/src/app/(dashboard)/procurement/receiving/page.tsx` |
| F20 | P3 | `golangci-lint run` fails on existing WIP style/lint issues | Phase 4 tests, revenue/payment handlers, procurement repo helpers, gosec false positives |

Severity scale:

- P0: immediate production outage, exploitable cross-tenant data leak, or irreversible data corruption confirmed.
- P1: release blocker for a Phase 1-10 claim; security or product-completeness issue requiring planned remediation before handoff.
- P2: important correctness, UX, maintainability, auditability, or operability issue that should be fixed soon.
- P3: cleanup, CI hygiene, documentation drift, or non-blocking polish.

## Verification Summary

| Check | Result | Notes |
| --- | --- | --- |
| `go test ./...` | Pass | All Go packages compiled and package tests passed. |
| `pnpm --filter @fuelgrid/sdk typecheck` | Pass | SDK TypeScript compiles. |
| `pnpm --filter @fuelgrid/ui typecheck` | Pass | UI package TypeScript compiles. |
| `pnpm --filter @fuelgrid/web typecheck` | Pass | Web TypeScript compiles. |
| `pnpm --filter @fuelgrid/web build` | Pass with warning | Next 15 production build succeeds; warns the Next ESLint plugin is not detected. |
| `pnpm --filter @fuelgrid/web lint` | Pass with warning | `next lint` reports no ESLint errors, but command is deprecated and plugin warning remains. |
| DB-backed `go test ./services/api/internal/server -run TestPhase -count=1 -v` | Pass | Ran after fresh migrations against temp Postgres DB and Redis. Phase 2-10 integration tests passed. |
| `npx --yes @redocly/cli@latest lint docs/openapi.yaml` | Pass | Spec is syntactically valid but incomplete beyond early phases. |
| `pnpm format:check` | Fail | Prettier reports style issues in two procurement web pages. |
| `golangci-lint run` | Fail | Existing lint issues in Phase 4 tests, payment/revenue handler formatting, procurement unused helpers, and gosec false positives. |
| `git diff --check` | Pass | No whitespace errors in current diff. |

The integration test command created a temporary database, ran all migrations, executed phase tests, and dropped the temporary database afterward.

## Current Implementation Shape

The repo now has real backend and domain coverage through Phase 10:

- Phase 5 procurement domain and API are present.
- Phase 6 revenue APIs and revenue dashboard are present.
- Phase 7 finance/accounting APIs are extensive.
- Phase 8 customer credit and fleet APIs are extensive.
- Phase 9 enterprise governance and central commercial APIs are extensive.
- Phase 10 risk, scoring, investigation, and governance APIs are extensive.

The web app, however, exposes a narrower route set. The production build generated these relevant dashboard routes:

- `/command-center`
- `/stations`
- `/stations/[stationID]`
- `/stations/[stationID]/pumps/[pumpID]`
- `/settings`
- `/settings/companies`
- `/settings/regions`
- `/settings/stations`
- `/settings/products`
- `/settings/tanks`
- `/settings/tanks/[tankID]`
- `/settings/pumps`
- `/settings/suppliers`
- `/settings/users`
- `/settings/roles`
- `/my-shift`
- `/operations`
- `/inventory`
- `/reconciliation`
- `/procurement`
- `/procurement/receiving`
- `/revenue`
- `/finance`
- `/finance/close`
- `/customers`
- `/enterprise`
- `/enterprise/approvals`
- `/risk`
- `/incidents`
- `/audit`
- `/profile`

The sidebar exposes additional route names that were not generated by the build:

- `/tanks`
- `/pumps`
- `/sales`
- `/reports`
- `/alerts`
- `/assistant`

This is not just cosmetic. Those links are in the primary navigation and look operational. The comment above the sidebar still says placeholder 404 navigation is intentional, but the app has moved far past early placeholder shell status.

## Phase Coverage Matrix

| Phase | Backend/API implementation | Web implementation | Audit verdict |
| --- | --- | --- | --- |
| Phase 1 - Platform foundation | Present: auth, tenancy, RBAC, audit/outbox, users, roles, platform tenant provisioning | Present: login, reset password, profile, settings/users/roles/audit | Mostly implemented; session hardening and API contract automation remain weak. |
| Phase 2 - Fuel infrastructure | Present: companies, regions, stations, products, tanks, pumps, nozzles, calibration, incidents | Present: settings pages, station page, pump calibration detail, incidents | Mostly implemented; old Phase 2 audit findings should be rechecked before release. |
| Phase 3 - Station operations | Present: operating days, shifts, readings, close, exceptions | Present: my shift and operations dashboard | Mostly implemented; old Phase 3 audit findings should be rechecked against current code. |
| Phase 4 - Inventory and reconciliation | Present: stock movements, tank ledger, opening balance, reconciliation, inventory overview | Present: inventory and reconciliation pages | Implemented enough for integration tests; web surface is operational but not exhaustive. |
| Phase 5 - Supply chain and procurement | Present: suppliers, purchase orders, receipts, invoices, discrepancies, payables handoff | Present: procurement overview and receiving/invoice workflow | Substantial implementation; current WIP formatting/lint cleanup remains. |
| Phase 6 - Sales, payments, revenue | Present: sales, payments, revenue days, AR aging, pricing hooks | Present: revenue dashboard with close-and-lock action | Partial web coverage; no `/sales` route despite sidebar. |
| Phase 7 - Finance and accounting | Present: accounting periods, chart, journals, payables, banking, statements, expenses, exports, reports | Present: finance overview and period close page | Backend-rich, UI-thin. Many finance workflows are API-only. |
| Phase 8 - Customer credit and fleet | Present: customers, credit profiles, holds, price agreements, vehicles, drivers, credentials, authorizations, limits, odometer, statements, alerts | Present: customers list and open credit alert scan | Not product-complete in web. Core customer/fleet workflows are API-only. |
| Phase 9 - Chain and enterprise command | Present: station groups, scope grants, approval policies/requests, projections, rankings, central pricing/procurement, stock transfers, consolidated reports, exception queue | Present: enterprise overview, ranking, approvals/exception counts | Not product-complete in web. Governance and central control workflows are API-only. |
| Phase 10 - Risk, fraud, intelligence | Present: signals, rules, detection, alerts, scores, investigations, suppressions, governance | Present: risk overview, run detection, open alert list, resolve button | Not product-complete in web. Investigation and governance workflows are API-only. |

## Detailed Findings

### F01 - P1 - Main navigation routes users into 404 pages

Evidence:

- Sidebar registers `Tanks` as `/tanks`, `Pumps` as `/pumps`, `Sales` as `/sales`, `Reports` as `/reports`, `Alerts` as `/alerts`, and `AI Assistant` as `/assistant` in `apps/web/src/components/layout/sidebar.tsx:40` through `:53`.
- The same file states that many entries are placeholders and 404 behavior is intentional in `apps/web/src/components/layout/sidebar.tsx:58` through `:62`.
- The production Next build generated no routes for `/tanks`, `/pumps`, `/sales`, `/reports`, `/alerts`, or `/assistant`.

Impact:

Users will experience first-level navigation failures in a system claiming Phases 1-10 are complete. The issue is especially damaging because working alternatives exist for some areas under different URLs: tanks and pumps are under `/settings/tanks` and `/settings/pumps`, revenue exists under `/revenue`, and risk alerts exist under `/risk`.

Recommendation:

- Remove placeholder nav entries from the production sidebar until their routes exist.
- Redirect `Tanks` and `Pumps` to `/settings/tanks` and `/settings/pumps` if those are the canonical pages.
- Replace `Sales` with `/revenue` or add a true `/sales` page over sales/payment APIs.
- Replace `Alerts` with `/risk` or build `/alerts` as a cross-domain alert queue.
- Hide `AI Assistant` until the assistant route is implemented.
- Add a route/nav test that verifies every sidebar href exists in the Next app route manifest.

### F02 - P1 - Phase 8 customer/fleet UI is not the full Phase 8 product

Evidence:

- The current customer page fetches only customers and open credit alerts: `apps/web/src/app/(dashboard)/customers/page.tsx:32` through `:45`.
- The only write action on the page is `Scan credit alerts`: `apps/web/src/app/(dashboard)/customers/page.tsx:56` through `:58`.
- Customer rows display name, code, credit limit, and status only: `apps/web/src/app/(dashboard)/customers/page.tsx:115` through `:124`.
- Backend routes exist for Phase 8 customer lifecycle, credit profile, holds, price agreements, vehicles, drivers, credentials, authorization, limits, odometer, statements, and credit alerts in `services/api/internal/server/server.go:406` through `:490`.
- The Phase 8 roadmap says the UI should support customer profile, credit position, vehicles, drivers, credentials, limits, authorizations, sales, statements, alerts, and forecourt authorization.

Impact:

The backend may pass Phase 8 integration tests, but the web app does not let operators perform the main Phase 8 tasks. A user cannot create/edit a customer, manage contacts, manage credit profile, place/release holds, register vehicles, create drivers, issue credentials, validate credentials, request/fulfill/cancel authorizations, manage fuel limits, review odometer history, generate/issue statements, acknowledge/resolve credit alerts, or view fleet consumption from the web UI.

Recommendation:

- Split Phase 8 UI into route groups:
  - `/customers`: searchable customer list, status, exposure, hold, overdue, recent activity.
  - `/customers/[id]`: profile, contacts, credit position, holds, price agreements, statements, alerts.
  - `/customers/[id]/fleet`: vehicles, drivers, credentials, limits, odometer, consumption.
  - `/fleet/authorize`: forecourt authorization/credential validation workflow.
  - `/fleet/authorizations`: authorization queue with fulfill/cancel/void.
- Add confirmation and reason capture for hold, suspend, retire, cancel, and void actions.
- Make Phase 8 acceptance depend on browser coverage, not API tests alone.

### F03 - P1 - Phase 9 enterprise command is only partially surfaced

Evidence:

- `/enterprise` fetches only overview and station ranking and offers `Rebuild projections`: `apps/web/src/app/(dashboard)/enterprise/page.tsx:25` through `:59`.
- `/enterprise/approvals` fetches approval requests and exception counts, then offers approve/reject buttons: `apps/web/src/app/(dashboard)/enterprise/approvals/page.tsx:30` through `:43`; `:97` through `:121`.
- Backend routes exist for station groups, group membership, effective station scopes, scope grants, approval policies, approval requests, projections, station ranking, central price rollouts, central procurement plans, stock transfers, consolidated finance, KPI export, and enterprise exceptions in `services/api/internal/server/server.go:496` through `:555`.

Impact:

The web app does not yet provide the enterprise control plane promised by Phase 9. Operators cannot configure station groups, grant enterprise scopes, manage approval policies, raise approval requests, create/approve/publish central price rollouts, create/release central procurement plans, create/approve/receive stock transfers, view consolidated finance, or export station KPIs from the UI.

Recommendation:

- Add `/enterprise/groups` for group CRUD and member management.
- Add `/enterprise/access` for effective-station scope visualization and scope grants.
- Add `/enterprise/policies` for approval policy management.
- Add `/enterprise/price-rollouts` for central price rollout create/approve/activate.
- Add `/enterprise/procurement-plans` for central procurement planning/release.
- Add `/enterprise/stock-transfers` for transfer create/approve/receive.
- Add `/enterprise/finance` and `/enterprise/reports` for consolidated finance and KPI exports.
- Add projection rebuild confirmation and explain its data freshness impact.

### F04 - P1 - Phase 10 risk UI omits most of the risk operating system

Evidence:

- `/risk` fetches overview and open alerts: `apps/web/src/app/(dashboard)/risk/page.tsx:24` through `:33`.
- The detection button runs detection and recomputes scores: `apps/web/src/app/(dashboard)/risk/page.tsx:34` through `:40`.
- Alert resolution is a one-click mutation to `resolve` with hardcoded disposition `reviewed`: `apps/web/src/app/(dashboard)/risk/page.tsx:42` through `:44`.
- Backend routes exist for risk signals, rules, backfill, detection, alert transitions, scores, recompute, investigations, comments, case actions, governance, suppressions, engine pause, rule tuning, and alert suppression in `services/api/internal/server/server.go:557` through `:613`.

Impact:

The risk UI is not an investigative workspace. Users cannot inspect signals, manage rules, dry-run/tune rules, inspect score components, acknowledge/escalate/dismiss alerts, open or attach investigations, add comments or actions, suppress noisy alerts, pause the engine, or review governance/data quality. This makes Phase 10 visible but not operational.

Recommendation:

- Add `/risk/alerts/[id]` for evidence, source links, status history, comments, and related cases.
- Add `/risk/investigations` and `/risk/investigations/[id]` for case workflow.
- Add `/risk/rules` and `/risk/signals` for rule/signal administration.
- Add `/risk/scores` for score breakdowns and top-risk dimensions.
- Add `/risk/governance` for suppressions, engine pause, rule tuning, and data quality.
- Require reason/disposition on resolve/dismiss/suppress and store it visibly.

### F05 - P1 - Password reset tokens are logged without an environment gate

Evidence:

- `handlePasswordResetRequest` calls `RequestPasswordReset` and receives the raw reset token in `services/api/internal/server/auth_handlers.go:107`.
- If `delivered` is true, the handler logs tenant, email, and `token` in `services/api/internal/server/auth_handlers.go:113` through `:120`.
- The comment says production wires email and dev logs the token, but there is no check against `s.cfg.Env`.
- Runtime environment is available as `Config.Env` from `NODE_ENV`: `services/api/internal/config/config.go:18`.

Impact:

In a production deployment, a valid password reset token could be emitted to API logs for every real reset request unless the handler is changed or logs are filtered externally. Logs commonly flow to third-party systems and support consoles. That turns an account recovery secret into a log-retained credential.

Recommendation:

- Only log reset tokens when `cfg.Env == "development"` or under an explicit unsafe local flag.
- In non-development, send reset token through the intended email/outbox mechanism and log only non-secret delivery metadata.
- Consider adding a redaction guard/test that fails if keys named `token`, `password`, or `secret` are logged on auth recovery paths.
- Add an integration test that sets `Env: "production"` and confirms no reset token appears in logs.

### F06 - P1 - OpenAPI is valid but incomplete beyond early phases

Evidence:

- `docs/openapi.yaml` validates with Redocly.
- It contains tags and paths through operations-era endpoints, ending around `/api/v1/stations/{stationID}/operations-overview`.
- Searches for Phase 5-10 routes such as `/purchase-orders`, `/supplier-invoices`, `/finance/overview`, `/fleet/vehicles`, `/fuel-authorizations`, `/enterprise/overview`, `/risk/alerts`, and `/investigations` return no matches.
- The server route table includes all those surfaces in `services/api/internal/server/server.go:329` through `:750`.
- `packages/sdk/src/types.ts:1` through `:4` explicitly says types are hand-maintained and mismatches are not compiler-caught.

Impact:

The public API contract does not match the implemented API. This weakens external integration, SDK generation, QA review, contract testing, and change management. It also raises risk that new web pages will depend on SDK types that silently drift from handlers.

Recommendation:

- Update OpenAPI for Phases 4-10 before expanding the web surface further.
- Generate SDK types from OpenAPI or Go route/schema metadata rather than maintaining large TypeScript models by hand.
- Add CI that fails if route table coverage and OpenAPI paths diverge for tagged API groups.
- Keep route-level examples for high-risk flows: close, lock, approve, reject, fulfill, resolve, dismiss, suppress.

### F07 - P1 - Session storage and expiry handling are too weak for a finance/risk system

Evidence:

- The auth store persists `token` and `expiresAt` in localStorage: `apps/web/src/stores/auth-store.ts:20` through `:39`.
- `ProtectedRoute` checks only whether `token` exists, not whether `expiresAt` has passed: `apps/web/src/components/auth/protected-route.tsx:24` through `:43`.
- The SDK attaches bearer tokens but simply throws `SdkError` on non-OK responses: `packages/sdk/src/client.ts:165` through `:204`.
- There is an SDK `refresh` method at `packages/sdk/src/client.ts:253`, but the web app does not use it globally.
- `apps/web/src/app/page.tsx:8` through `:13` calls out the localStorage-only auth contract as temporary.

Impact:

An expired session can remain visually authenticated until the next API call fails. A revoked or expired token does not automatically clear the client session. Because the token lives in localStorage, any XSS exposure can exfiltrate it. For a system handling money, audit, risk, and customer credit, this should not be accepted as the final auth posture.

Recommendation:

- Enforce `expiresAt` in route guards and root redirect.
- Add a global React Query or SDK error hook that clears session and redirects on 401.
- Use refresh proactively or remove refresh until implemented.
- Move toward HttpOnly, Secure, SameSite cookie sessions when server-side middleware is introduced.
- Add CSP and dependency hygiene before relying on browser storage for bearer tokens.

### F08 - P2 - High-impact mutations lack confirmation, reason, and evidence capture

Evidence:

- Revenue close computes and locks a revenue day in one click: `apps/web/src/app/(dashboard)/revenue/page.tsx:52` through `:57`; button at `:128` through `:131`.
- Enterprise projection rebuild is one click: `apps/web/src/app/(dashboard)/enterprise/page.tsx:35` through `:59`.
- Risk detection/recompute is one click: `apps/web/src/app/(dashboard)/risk/page.tsx:34` through `:40`.
- Risk alert resolve is one click with hardcoded disposition: `apps/web/src/app/(dashboard)/risk/page.tsx:42` through `:44`.
- Enterprise approve/reject is one click with no comment field: `apps/web/src/app/(dashboard)/enterprise/approvals/page.tsx:107` through `:120`.
- Finance period close/lock actions are one click: `apps/web/src/app/(dashboard)/finance/close/page.tsx:120` through `:147`.

Impact:

Operators can perform irreversible or audit-sensitive actions without deliberate acknowledgement. The backend may write audit logs, but the frontend is not collecting operator intent. That limits audit usefulness and increases accidental close/approval/resolve risk.

Recommendation:

- Add confirmation modals for close, lock, rebuild, run detection, approve, reject, resolve, dismiss, and suppress.
- Require comments/reasons for reject, resolve, dismiss, suppress, reopen, and manual lock.
- Show computed impact before mutation: rows affected, period/day, station, amount, alert id, requester, and source document.
- Prefer two-step action for destructive state changes: preview then commit.

### F09 - P2 - Command palette and insight panel are placeholders

Evidence:

- Command palette list only renders an empty state: `apps/web/src/components/layout/command-palette.tsx:56` through `:60`.
- Right panel comment says AI assistant and recommended actions land later: `apps/web/src/components/layout/right-panel.tsx:5` through `:9`.
- Right panel body says "Quiet for now" and promises recommended actions/alerts/AI explanations later: `apps/web/src/components/layout/right-panel.tsx:18` through `:20`.

Impact:

The application shell suggests global search, commands, insights, alerts, and intelligence, but these surfaces are not implemented. That conflicts with Phase 9/10 positioning and wastes valuable dashboard width on placeholder content.

Recommendation:

- Either remove these surfaces from production or make them useful now.
- Minimum useful command palette: navigate routes, search stations, search customers, open recent approvals, open risk alerts.
- Minimum useful insight panel: open exceptions, close blockers, risk alerts, approvals waiting, stale projections, unreconciled cash.

### F10 - P2 - Mobile navigation is absent

Evidence:

- Sidebar uses `hidden ... lg:flex`: `apps/web/src/components/layout/sidebar.tsx:68`.
- No alternative mobile navigation component was found in the dashboard layout review.

Impact:

On mobile and tablet widths below `lg`, users lose primary navigation. This is material for forecourt, shift, procurement receiving, incident capture, authorization, and management workflows that may happen away from a desk.

Recommendation:

- Add a mobile drawer or bottom nav.
- Keep route groups concise by role: shift, operations, inventory, procurement, customers, risk, settings.
- Add Playwright viewport checks for mobile navigation existence and no horizontal overflow.

### F11 - P2 - Money display uses JavaScript Number coercion

Evidence:

- `customers/page.tsx:20` through `:23`.
- `enterprise/page.tsx:20` through `:22`.
- `finance/page.tsx:20` through `:22`.
- `revenue/page.tsx:21` through `:24`.
- Procurement pages also use `Number(...)` for decimal display and input normalization.

Impact:

Backend monetary values are stringified decimals to preserve precision. Converting them to `Number` for display can introduce rounding and formatting drift. This may not matter for small values but is the wrong default in finance, procurement, revenue, and credit surfaces.

Recommendation:

- Introduce shared decimal string formatters that preserve exact string value and only add separators/fixed scale.
- Use a decimal library if calculations are needed in the browser.
- Keep browser calculations advisory and let the API remain source of truth for posted amounts.

### F12 - P2 - Permission-aware UX is inconsistent

Evidence:

- `use-permissions.ts` exists and correctly treats frontend permission as a UX hint.
- Many pages simply show actions and handle 403 in query/mutation errors.
- Enterprise, risk, revenue, finance close, operations, and procurement pages expose buttons without visible permission-state gating in the reviewed snippets.

Impact:

Users can see actions they may not be allowed to perform, click them, and then get API errors. That is secure from a backend boundary perspective, but it is poor operator UX and produces avoidable error traffic.

Recommendation:

- Add a small permission wrapper/hook for action buttons.
- Render disabled buttons with tooltip reason when the user lacks permission.
- Keep backend enforcement as the source of truth.
- Add test fixtures for common roles: attendant, station manager, procurement officer, finance officer, auditor, executive, system admin.

### F13 - P2 - Finance UI covers only overview and close

Evidence:

- `/finance` calls only `getFinanceOverview`: `apps/web/src/app/(dashboard)/finance/page.tsx:25` through `:29`.
- The page renders balance sheet summary, P&L summary, control counts, and recent journal entries.
- The server exposes many finance routes: accounts, accounting periods, journals, payables, supplier payments, cash reconciliations, bank accounts, bank deposits, bank statement lines, customer invoices, customer payments, expenses, petty cash, reports, exports, close checklist in `services/api/internal/server/server.go:628` through `:750`.

Impact:

Finance users cannot run most finance workflows from the web app. The backend implementation is not matched by an operating surface for AP, AR, banking, expenses, petty cash, journal adjustments, exports, or report drilldowns.

Recommendation:

- Add finance subroutes:
  - `/finance/accounts`
  - `/finance/journals`
  - `/finance/payables`
  - `/finance/supplier-payments`
  - `/finance/banking`
  - `/finance/customer-invoices`
  - `/finance/customer-payments`
  - `/finance/expenses`
  - `/finance/petty-cash`
  - `/finance/reports`
  - `/finance/exports`
- Keep `/finance` as the overview.
- Gate sensitive workflows by permission and confirmation.

### F14 - P2 - Period close UI is incomplete and may overgeneralize lock readiness

Evidence:

- `PeriodAction` includes `reopen`, but no reopen button is rendered: `apps/web/src/app/(dashboard)/finance/close/page.tsx:37`.
- Lock is only shown for `p.status === 'closed'`, and it is disabled by global `!checklist.data.can_close`: `apps/web/src/app/(dashboard)/finance/close/page.tsx:140` through `:147`.
- The checklist is tenant-level close readiness, while the UI lists multiple periods.

Impact:

Users cannot reopen a closed period from the web. A period-specific lock may also be blocked by checklist items that are not clearly tied to that period, depending on backend checklist semantics. The UI does not explain which period blockers apply to which period.

Recommendation:

- Add explicit reopen UI with reason capture and permission gate.
- Make close blockers period-scoped in the UI.
- Show blocker drilldowns, not only counts.
- Require confirmation for lock and close.
- Distinguish "cannot close this period" from "global finance queue has blockers."

### F15 - P2 - Risk alert resolution loses decision quality

Evidence:

- `resolve` mutation sends `{ disposition: 'reviewed' }` for every alert: `apps/web/src/app/(dashboard)/risk/page.tsx:42` through `:44`.
- The alert row gives only a `Resolve` button: `apps/web/src/app/(dashboard)/risk/page.tsx:123` through `:130`.
- The backend supports acknowledge, investigate, resolve, dismiss, and escalate: `services/api/internal/server/server.go:573` through `:580`.

Impact:

All resolutions look the same from the UI. There is no disposition choice, no user note, no evidence, no case link, no false-positive path, no escalation path, and no investigation path. That weakens auditability and makes later risk governance less useful.

Recommendation:

- Replace one-click resolve with an alert detail drawer.
- Offer status actions: acknowledge, investigate, escalate, resolve, dismiss.
- Require disposition/reason for terminal actions.
- Allow creating or attaching an investigation case.
- Show source facts and score contribution before terminal action.

### F16 - P2 - SDK typing is weakest in later-phase APIs

Evidence:

- File header says SDK types are hand-maintained until OpenAPI generation: `packages/sdk/src/types.ts:1` through `:4`.
- `listFuelLimits` returns `{ items: unknown[]; count: number }`: `packages/sdk/src/client.ts:1686` through `:1691`.
- `addStationGroupMember` returns `Promise<unknown>`: `packages/sdk/src/client.ts:2397` through `:2402`.
- `listRiskSignals` and `listRiskRules` return `unknown[]`: `packages/sdk/src/client.ts:2648` through `:2657`.
- Investigation methods return `Record<string, unknown>` or `unknown`: `packages/sdk/src/client.ts:2742` through `:2808`.

Impact:

TypeScript cannot protect frontend work in the areas with the most complex workflows. This makes runtime data-shape mistakes more likely as the missing Phase 8-10 pages are added.

Recommendation:

- Define proper SDK interfaces for limits, station groups, scope grants, policies, central plans, price rollouts, transfers, signals, rules, scores, investigations, suppressions, and governance.
- Prefer generating these from OpenAPI once F06 is addressed.
- For interim work, add zod schemas or response mappers where pages consume unknown data.

### F17 - P2 - Security headers are not configured

Evidence:

- Web metadata sets only title and description: `apps/web/src/app/layout.tsx:8` through `:11`.
- `apps/web/next.config.ts` has no `headers()` configuration.
- API middleware configures request ID, logging, metrics, recoverer, timeout, and CORS in `services/api/internal/server/server.go:149` through `:162`, but no CSP, HSTS, frame, referrer, or permissions policy headers are configured in the reviewed middleware.

Impact:

The system handles bearer tokens, money, customer credit, audit, and risk data. Browser hardening should not be postponed indefinitely, especially while bearer tokens are in localStorage.

Recommendation:

- Add CSP appropriate for Next, Sentry, and API origin.
- Add `X-Frame-Options` or CSP `frame-ancestors`.
- Add `Referrer-Policy`.
- Add `Permissions-Policy`.
- Add HSTS at ingress/proxy; document if not app-level.
- Add a security header test in deployment smoke checks.

### F18 - P2 - ESLint setup is passing but not future-proof

Evidence:

- `apps/web/package.json:10` uses `next lint`.
- `pnpm --filter @fuelgrid/web lint` passes but warns `next lint` is deprecated and will be removed in Next 16.
- Build/lint output warns the Next.js plugin was not detected.
- Root `eslint.config.mjs` imports `@fuelgrid/config/eslint/base`, not the Next config.

Impact:

The current lint result is less meaningful for a Next app than it should be, and the command will age out. Some Next-specific issues may not be caught.

Recommendation:

- Switch web ESLint to the ESLint CLI.
- Use `@fuelgrid/config/eslint/next` for the web app or merge Next plugin rules into root config.
- Make build warning-free before release.

### F19 - P3 - Prettier check fails on procurement pages

Evidence:

`pnpm format:check` reports:

- `apps/web/src/app/(dashboard)/procurement/page.tsx`
- `apps/web/src/app/(dashboard)/procurement/receiving/page.tsx`

Impact:

This is not a runtime defect, but it means the current worktree fails repository formatting hygiene.

Recommendation:

- Run Prettier on those files when the WIP branch is ready for cleanup.
- Add formatting checks to the regular pre-merge gate.

### F20 - P3 - golangci-lint fails on existing issues

Evidence:

`golangci-lint run` reported:

- Error comparisons in `services/api/internal/server/phase4_integration_test.go:153` and `:202` should use `errors.Is`.
- `services/api/internal/server/payments_handlers.go:48` is not gofmt-formatted.
- `services/api/internal/server/revenue_handlers.go:67` is not gofmt-formatted.
- `internal/procurement/repo.go:31` and `:33` contain unused helpers.
- `internal/fleet/credentials.go:45` and `internal/fleet/credit_profiles.go:53` are gosec `G101` false positives on column-name constants.

Impact:

Go code compiles and tests pass, but stricter lint gates currently fail. The unused helpers and formatting findings are real cleanup. The `G101` findings are likely false positives and should be annotated or configured.

Recommendation:

- Fix gofmt and unused helper issues.
- Replace direct sentinel comparisons with `errors.Is`.
- Add `//nolint:gosec` on safe SQL column constants or tune gosec config.

## Subsystem Audit

### Application Shell

Strengths:

- The dashboard shell is consistent and uses shared layout pieces.
- Sidebar groups a broad operational surface in one place.
- Topbar provides station selector and logout access.
- Route guards prevent unauthenticated rendering after local storage hydration.

Risks:

- Sidebar still treats dead links as intentional placeholders.
- The right insight panel consumes desktop width without operational value.
- Command palette is wired to keyboard shortcut but has no commands.
- Mobile users lose primary navigation.

Release bar:

- Every visible top-level navigation item must resolve to a real page or be hidden behind a feature flag.
- Global command and insight areas should be either useful or absent.

### Authentication and Session

Strengths:

- Login, logout, refresh, password reset, MFA enrollment/verification, current user, permissions, sessions, and password change are all represented on the API side.
- Frontend login validates input and avoids arbitrary off-site `next` redirects.
- Backend password reset returns a generic `202`, avoiding account enumeration.

Risks:

- Reset tokens are logged.
- Client token expiry is not enforced.
- Global 401 handling is absent.
- LocalStorage bearer token storage is risky for a finance/risk app.
- Refresh exists but is not part of a coherent client flow.

Release bar:

- No recovery secret should be logged outside development.
- Expired or revoked sessions should clear the UI session immediately on 401.
- Security headers and XSS hardening should be in place before treating localStorage bearer tokens as acceptable.

### API and Backend Contracts

Strengths:

- Route table is well-organized by phase and uses explicit permission middleware.
- Later-phase APIs are broad and integration-tested.
- RLS and tenant isolation are used extensively in migrations.
- Many state-changing backend flows appear to be audited and transaction-aware.

Risks:

- OpenAPI lags behind implementation.
- SDK types are partly hand-maintained and partly unknown.
- Some advanced handler responses use generic maps, pushing schema risk to clients.

Release bar:

- OpenAPI must cover every supported route.
- SDK should be generated or contract-tested.
- Browser pages should not consume `unknown` for core business objects.

### Data Isolation and Migrations

Strengths:

- Migrations consistently use `tenant_id`.
- RLS policies appear broadly applied.
- Composite tenant keys are used in many areas to prevent cross-tenant links.
- Fresh migrations succeeded in the integration test database.

Risks:

- The audit did not perform a formal table-by-table RLS proof.
- Some older audit findings may still apply around handler-only invariants.
- Down migrations were not exercised in this pass.

Release bar:

- Add a migration audit script that asserts every tenant table has RLS enabled and a tenant isolation policy.
- Add a down/up migration smoke test if rollback support is expected.

### Phase 1 - Platform Foundation

Implemented:

- Tenants, companies, regions, stations.
- Auth, sessions, MFA, password reset.
- Users, roles, permissions, station access.
- Audit logs and outbox.
- Platform tenant provisioning.

Web coverage:

- Login, forgot/reset password, MFA route, profile, users, roles, companies, regions, stations, audit.

Concerns:

- Password reset token logging.
- Client session storage/expiry.
- OpenAPI/SDK drift.

### Phase 2 - Fuel Infrastructure Core

Implemented:

- Products, tanks, pumps, nozzles.
- Tank calibration.
- Pump calibration/status.
- Incidents.
- Station overview.

Web coverage:

- Settings products/tanks/pumps.
- Tank detail calibration.
- Station overview and pump detail calibration.
- Incidents.

Concerns:

- Sidebar duplicates tanks/pumps under dead root paths.
- Recheck old Phase 2 audit findings before release.
- Security headers and permission-aware UI remain broad concerns.

### Phase 3 - Station Operations Core

Implemented:

- Operating days.
- Shifts.
- Attendants and nozzle assignments.
- Meter readings.
- Dip readings.
- Shift close.
- Cash submission.
- Exceptions and approvals.
- My shift and operations overview.

Web coverage:

- `/my-shift`.
- `/operations`.

Concerns:

- Recheck old Phase 3 audit findings against current code.
- Operations page remains a dashboard/action subset, not a complete day-control console.
- High-impact close/approve/resolve actions need stronger confirmation and reason capture.

### Phase 4 - Inventory and Reconciliation

Implemented:

- Stock movements.
- Tank ledger and book balance.
- Opening balances.
- Deliveries from early phase plus newer procurement receipts.
- Reconciliation.
- Inventory and reconciliation overview.

Web coverage:

- `/inventory`.
- `/reconciliation`.

Concerns:

- Reconciliation/adjustment actions should retain strong reason capture and confirmation.
- Inventory precision and display should avoid `Number` coercion where decimals matter.
- Formal RLS/table audit still needed.

### Phase 5 - Supply Chain and Procurement

Implemented:

- Suppliers.
- Purchase orders.
- Goods receipts.
- Procurement discrepancies.
- Supplier invoices.
- Approved invoice to payable handoff.
- Procurement overview and receiving workflow.

Web coverage:

- `/settings/suppliers`.
- `/procurement`.
- `/procurement/receiving`.

Concerns:

- Prettier fails on procurement pages.
- `golangci-lint` reports unused helpers in `internal/procurement/repo.go`.
- Procurement UI is useful but still narrow: supplier settings and receiving are present, but purchase planning/edit/review depth should be compared to the Phase 5 roadmap before declaring product-complete.

### Phase 6 - Sales, Payments, and Revenue

Implemented:

- Sales recognition.
- Payments and tenders.
- Revenue overview.
- Revenue day compute/lock.
- AR aging.
- Pricing APIs.

Web coverage:

- `/revenue`.

Concerns:

- Sidebar advertises `/sales`, but no route exists.
- Revenue day compute and lock happen as one button without confirmation.
- `dayID!` non-null assertion is guarded by UI today, but the mutation itself has no explicit runtime guard.
- Money display uses `Number`.

### Phase 7 - Finance and Accounting Control

Implemented:

- Chart of accounts.
- Accounting periods.
- Journal entries and reversals.
- Payables and supplier payments.
- Cash reconciliations.
- Bank accounts, deposits, statement matching.
- Customer invoices and payments.
- Expenses and petty cash.
- Financial reports and exports.
- Close checklist.

Web coverage:

- `/finance`.
- `/finance/close`.

Concerns:

- Most finance workflows are API-only.
- Close/lock/reopen experience is incomplete.
- Finance reports are not navigable/drillable from the web app.
- Export workflows are API-only.

### Phase 8 - Customer Credit and Fleet Fuel OS

Implemented:

- Customer master.
- Credit profile and position.
- Credit holds.
- Customer price agreements.
- Vehicles.
- Drivers.
- Credentials.
- Fuel authorizations.
- Fuel limits.
- Odometer.
- Fleet consumption.
- Statements.
- Credit alerts.

Web coverage:

- `/customers` shows customers and open alerts, and can scan alerts.

Concerns:

- This is the largest backend/UI mismatch in the current app.
- No customer detail route.
- No fleet route.
- No forecourt authorization route.
- No credential management.
- No limit management.
- No statement issue flow.
- No credit alert acknowledge/resolve flow.

### Phase 9 - Chain and Enterprise Command

Implemented:

- Station groups and memberships.
- Enterprise scope grants and effective stations.
- Approval policies and requests.
- Enterprise projections and ranking.
- Central price rollouts.
- Central procurement plans.
- Stock transfers.
- Consolidated finance.
- KPI exports.
- Enterprise exceptions.

Web coverage:

- `/enterprise`.
- `/enterprise/approvals`.

Concerns:

- Enterprise configuration and central commercial control are API-only.
- Approval decisions lack comment/reason capture.
- Exception queue shows counts but not drilldowns or resolution workflows.
- Projection rebuild lacks confirmation and freshness explanation.

### Phase 10 - Risk, Fraud, and Intelligence

Implemented:

- Risk signals.
- Risk rules.
- Backfill.
- Detection.
- Alert transitions.
- Risk scores and overview.
- Investigations.
- Case comments and actions.
- Suppressions.
- Governance summary.
- Engine pause and rule tuning.

Web coverage:

- `/risk`.

Concerns:

- Risk center is not an investigation workspace.
- Alert state transitions are mostly not exposed.
- Evidence/source links are not surfaced.
- Investigation cases are API-only.
- Rule and suppression administration are API-only.
- Governance controls are API-only.

### SDK

Strengths:

- Broad client method coverage exists.
- Browser `fetch` default is bound to avoid illegal invocation.
- SDK exposes many later-phase APIs already.

Risks:

- Types are hand-maintained.
- Advanced APIs use `unknown` and `Record<string, unknown>`.
- No global 401 callback or refresh behavior exists at SDK level.

Release bar:

- SDK generation or schema validation should land before adding many new Phase 8-10 web pages.

### UX and Accessibility

Strengths:

- Shared UI components give consistent visual rhythm.
- Pages use loading and error states.
- Many forbidden states are recognized and rendered as "No access" style messages.

Risks:

- No mobile nav.
- Many action buttons are text-only high-impact mutations without confirmation.
- Some pages use dense flex rows that may overflow with long station/customer names.
- Placeholder surfaces make the app feel unfinished.

Release bar:

- Add viewport QA for mobile and desktop.
- Add route and action confirmation tests for critical flows.
- Add role-based UI stories or fixtures.

## Release Risk Assessment

### Security

Highest risk: password reset token logging and localStorage bearer token posture.

The API appears to have strong tenant isolation patterns, but auth/token handling needs hardening. Treat F05 and F07 as security release blockers.

### Data Integrity

Highest risk: user-triggered close/lock/approve/resolve actions without confirmation or reason capture.

Backend tests pass, but the browser should collect operator intent for financial, revenue, approval, and risk state changes.

### Product Completeness

Highest risk: Phase 8-10 UI mismatch.

The app should not be marketed internally as "Phase 1-10 implemented" without qualifying that the later phases are primarily backend/API and integration-test implemented.

### Maintainability

Highest risk: OpenAPI/SDK drift.

As more pages consume later-phase APIs, hand-maintained types and missing OpenAPI coverage will become a compounding source of regressions.

### Operability

Highest risk: incomplete command/insight/alert surfaces.

The application has enough data to begin surfacing operational queues, but key shell surfaces are placeholders.

## Recommended Remediation Plan

### Gate 1 - Stop visible chaos

1. Fix sidebar dead routes.
2. Hide or implement command palette actions.
3. Hide or implement insight panel.
4. Add mobile navigation.
5. Fix procurement formatting and Go lint cleanup that is already known.

### Gate 2 - Security hardening

1. Stop password reset token logging outside development.
2. Enforce token expiry in the frontend guard.
3. Add global 401 session clear.
4. Add security headers.
5. Decide whether localStorage bearer tokens are temporary or acceptable with documented controls.

### Gate 3 - Contract stabilization

1. Update OpenAPI for Phases 4-10.
2. Generate or validate SDK models from the contract.
3. Replace `unknown` later-phase SDK responses.
4. Add route/OpenAPI drift check.

### Gate 4 - Later-phase web completion

1. Build Phase 7 finance subroutes for AP, AR, banking, expenses, journals, reports, exports.
2. Build Phase 8 customer detail, fleet, authorization, limits, statements, alerts.
3. Build Phase 9 groups, scopes, policies, central pricing, central procurement, transfers, consolidated finance.
4. Build Phase 10 alert detail, investigations, rules, signals, scores, suppressions, governance.

### Gate 5 - Audit UX and confirmation patterns

1. Add confirmation modal for all close/lock/approve/reject/resolve/dismiss/rebuild/run actions.
2. Require reasons for terminal or override actions.
3. Show source, amount, station, period, and effect before commit.
4. Record operator-entered reasons in audit/outbox payloads where backend supports it.

## Acceptance Criteria for a True Phase 1-10 Web Claim

Before calling the entire web app Phase 1-10 complete, all of the following should be true:

1. Every sidebar route resolves to a real, useful page.
2. Every backend phase has at least one complete user workflow in the browser, not only list/overview pages.
3. Phase 7 finance users can run AP, AR, banking, expenses, journals, reports, exports, and period close from the UI.
4. Phase 8 users can manage customer profiles, credit profiles, vehicles, drivers, credentials, authorizations, limits, statements, and credit alerts from the UI.
5. Phase 9 users can manage enterprise groups, scopes, approval policies, approvals, central pricing, central procurement, stock transfers, consolidated finance, and enterprise exceptions from the UI.
6. Phase 10 users can inspect signals/rules, run detection, review evidence, manage alerts, create investigations, manage actions, tune rules, suppress alerts, and review governance from the UI.
7. Every close/lock/approve/reject/resolve/dismiss/suppress/rebuild action requires deliberate confirmation.
8. Password reset tokens are never logged in production.
9. Token expiry and 401 handling are coherent.
10. OpenAPI covers the implemented API.
11. SDK types are generated or exhaustively typed.
12. Production build, typechecks, format check, lint, Go tests, phase integration tests, and OpenAPI lint all pass.
13. Mobile navigation exists and critical mobile workflows are tested.
14. Security headers are configured and verified.
15. Role-specific UX is tested for common roles.

## Appendix A - Commands Run

```powershell
go test ./...
pnpm --filter @fuelgrid/sdk typecheck
pnpm --filter @fuelgrid/ui typecheck
pnpm --filter @fuelgrid/web typecheck
pnpm --filter @fuelgrid/web build
pnpm --filter @fuelgrid/web lint
pnpm format:check
golangci-lint run
npx --yes @redocly/cli@latest lint docs/openapi.yaml
git diff --check
```

Integration test command:

```powershell
$db = 'fuelgrid_audit_' + (Get-Date -Format 'yyyyMMddHHmmss')
docker exec fuelgrid-postgres createdb -U fuelgrid $db
$env:DATABASE_URL = "postgres://fuelgrid:fuelgrid@localhost:5433/$db`?sslmode=disable"
$env:TEST_DATABASE_URL = $env:DATABASE_URL
$env:REDIS_URL = 'redis://localhost:6379/0'
$env:TEST_REDIS_URL = 'redis://localhost:6379/0'
go run ./services/api/cmd/migrate up
go test ./services/api/internal/server -run TestPhase -count=1 -v
docker exec fuelgrid-postgres dropdb -U fuelgrid --force $db
```

## Appendix B - Passing Phase Integration Tests

The DB-backed phase test run passed the following test groups:

- Phase 2: read authorization, nozzle/product invariant, calibration upload/lookup/supersede, audit/outbox atomicity, soft delete guards, status transitions.
- Phase 3: day workflow, attendant self-scope, post-close correction lock, zero assignment close, cross-station reading, unassign cascade.
- Phase 4: stock ledger, opening balance, delivery, sales on approval, reconciliation, overviews.
- Phase 5: procurement flow.
- Phase 6: revenue flow.
- Phase 7: accounting foundation, payables, reports, cash/banking, receivables, expenses/petty cash, exports/close.
- Phase 8: credit foundation, fleet identity, authorization, odometer/consumption, statements/alerts.
- Phase 9: governance, dashboards, central commercial, consolidated finance, exception queue.
- Phase 10: signals/rules/detection, scoring/dashboard, investigations, governance.

This is strong evidence that backend phase flows work against a real migrated database.

It is not evidence that the web app exposes all those flows.

## Appendix C - Immediate Fix Checklist

- [ ] Replace or remove dead sidebar links.
- [ ] Stop password reset token logging outside development.
- [ ] Add frontend expiry check and global 401 logout.
- [ ] Update OpenAPI through Phase 10.
- [ ] Replace unknown SDK responses in Phase 8-10 methods.
- [ ] Add confirmation/reason dialogs for high-impact state transitions.
- [ ] Add mobile navigation.
- [ ] Implement useful command palette actions or hide the palette.
- [ ] Implement useful insight panel queues or hide the panel.
- [ ] Add Phase 7 finance subroutes.
- [ ] Add Phase 8 customer/fleet subroutes.
- [ ] Add Phase 9 enterprise control subroutes.
- [ ] Add Phase 10 risk investigation/governance subroutes.
- [ ] Fix Prettier failures.
- [ ] Fix `golangci-lint` failures or annotate false positives.

## Appendix D - Audit Limits

This audit reviewed local source code, route manifests, docs, SDK types, backend route registration, selected handlers, migrations, and automated verification results. It did not include:

- Manual browser walkthrough with screenshots.
- Full accessibility audit with screen reader tooling.
- Formal penetration test.
- Formal RLS proof for every table.
- Load/performance test.
- Production deployment review.
- External integration test.
- Review of user-managed infrastructure secrets or ingress configuration.

Those should be separate gates before production launch.

## Appendix E - Atomic Web Route Ledger

This appendix records the current web routes as audited from source and the production build output. The point is to make the gap between "route exists" and "workflow complete" explicit.

### `/`

Purpose: root redirector.

Observed behavior: client-side redirect to `/command-center` when a token exists and `/login` otherwise.

Evidence: `apps/web/src/app/page.tsx:8` through `:23`.

Atomic audit:

- The route is intentionally client-only because auth is currently localStorage-only.
- It cannot perform server-side auth decisions.
- It does not check `expiresAt`.
- It inherits the localStorage token risk from F07.

Release expectation:

- Acceptable for the current auth architecture.
- Should be revisited when cookie-backed sessions or middleware arrive.

### `/login`

Purpose: authenticate user into a tenant.

Observed behavior: login form posts to the SDK and stores token/expiresAt on success.

Atomic audit:

- Input validation exists.
- Same-origin `next` handling is present.
- MFA-required path is represented.
- Session persistence is localStorage based.
- There is no global session lifecycle manager after login.

Release expectation:

- Keep login, but harden session expiry and 401 behavior before production claims.

### `/forgot-password` and `/reset-password`

Purpose: request and confirm password reset.

Observed behavior: frontend pages exist; backend reset request avoids enumeration but logs tokens when delivered.

Atomic audit:

- Frontend surfaces are present.
- Backend token handling is the blocker.
- The recovery channel is not production-safe until F05 is fixed.

Release expectation:

- Fix token logging first.
- Add non-development delivery path before real users depend on this flow.

### `/mfa`

Purpose: MFA entry/setup surface.

Observed behavior: route builds as a small page.

Atomic audit:

- Backend MFA APIs exist.
- This audit did not perform a browser walkthrough of MFA enrollment and verify paths.

Release expectation:

- Add browser QA for enroll, verify, login with OTP, and invalid OTP.

### `/command-center`

Purpose: first authenticated dashboard.

Observed behavior: basic command center route exists and calls `api.me`.

Atomic audit:

- It is useful as a landing page only if it surfaces role-specific next actions.
- It should consume operational queues from phases 3-10.
- Current shell placeholders make it feel less mature than the backend.

Release expectation:

- Add "work to do" cards: active shift, open operating day, unresolved reconciliation, pending approvals, close blockers, open risk alerts, procurement receiving, credit holds.

### `/stations`

Purpose: station index.

Observed behavior: route exists and lists stations.

Atomic audit:

- It is a valid early navigation page.
- It should become the station control entry point.
- It should respect accessible station scope visually, not just via API.

Release expectation:

- Add filters by company/region/status when enterprise scopes are active.

### `/stations/[stationID]`

Purpose: station overview.

Observed behavior: route exists and fetches station overview and products.

Atomic audit:

- Useful for Phase 2 infrastructure visibility.
- Should evolve into an operator's station homepage with live day, inventory, risk, incidents, and finance status.

Release expectation:

- Add cross-links to operations, inventory, reconciliation, procurement receiving, revenue, incidents, and risk for the selected station.

### `/stations/[stationID]/pumps/[pumpID]`

Purpose: pump detail and calibration.

Observed behavior: route exists and supports pump calibration/status actions.

Atomic audit:

- Good infrastructure-level detail route.
- Should be tested for station-scope enforcement, calibration history, and status transitions.

Release expectation:

- Add confirmation and reason capture for status change.

### `/settings`

Purpose: settings index/layout.

Observed behavior: settings area exists.

Atomic audit:

- Useful separation for admin/master-data workflows.
- The sidebar should point tanks and pumps here if these are canonical admin paths.

Release expectation:

- Add a settings index with clear cards/links and permission-aware visibility.

### `/settings/companies`

Purpose: company master data.

Observed behavior: list/create/update company flow.

Atomic audit:

- Supports Phase 1 hierarchy.
- Needs permission-aware action visibility.
- Should show dependent regions/stations before destructive/deactivation actions if those actions exist.

Release expectation:

- Keep as admin page, not primary operations nav.

### `/settings/regions`

Purpose: region master data.

Observed behavior: list/create/update region flow.

Atomic audit:

- Supports enterprise hierarchy foundation.
- Later enterprise views should reuse region filters.

Release expectation:

- Add validation and clear relationship display to companies and stations.

### `/settings/stations`

Purpose: station master data.

Observed behavior: list/create/update station flow.

Atomic audit:

- Supports Phase 1.
- Should not be confused with `/stations`, which is operational.

Release expectation:

- Keep admin mutations in settings and operational visibility in `/stations`.

### `/settings/products`

Purpose: fuel product catalogue.

Observed behavior: product list/create/update flow.

Atomic audit:

- Supports Phase 2 and downstream pricing.
- Numeric input conversion uses `Number`, which is acceptable for form payload construction only if backend validates and stores decimals correctly.

Release expectation:

- Add guardrails around tax, density, tolerances, and downstream references.

### `/settings/tanks`

Purpose: tank master data.

Observed behavior: tank list/create/update flow.

Atomic audit:

- Canonical tank admin route exists.
- Sidebar still points a top-level `Tanks` link to `/tanks`, which does not exist.

Release expectation:

- Redirect or update nav.
- Add permission-aware actions.

### `/settings/tanks/[tankID]`

Purpose: tank detail/calibration volume lookup.

Observed behavior: tank detail and calibration chart workflows.

Atomic audit:

- Useful Phase 2 route.
- Calibration upload/active chart needs browser QA, not only backend tests.

Release expectation:

- Add preview validation and clear supersede history.

### `/settings/pumps`

Purpose: pump/nozzle admin.

Observed behavior: pump and nozzle create/delete/configure workflows.

Atomic audit:

- Canonical pump admin route exists.
- Sidebar still points top-level `Pumps` to `/pumps`, which does not exist.

Release expectation:

- Redirect or update nav.
- Add explicit product/tank/nozzle invariant messaging.

### `/settings/suppliers`

Purpose: supplier master data.

Observed behavior: list/create/update/deactivate suppliers.

Atomic audit:

- Supports Phase 5.
- New WIP route exists and builds.
- Formatting check failure is elsewhere in procurement pages, not this page.

Release expectation:

- Add supplier detail route if invoices, purchase orders, balances, and products need drilldown.

### `/settings/users`

Purpose: tenant user administration.

Observed behavior: users, roles, station grants, status updates.

Atomic audit:

- Core admin route.
- High-risk changes need confirmation and audit reason patterns.
- Role/station grant UX should clearly explain station-scoped vs tenant-wide permissions.

Release expectation:

- Add role-change confirmation and post-change permission summary.

### `/settings/roles`

Purpose: role catalogue.

Observed behavior: read-only role list.

Atomic audit:

- Useful for transparency.
- Does not manage custom roles if those are later introduced.

Release expectation:

- Keep read-only until custom role editing is deliberately designed.

### `/my-shift`

Purpose: attendant workbench.

Observed behavior: active shift, readings, dips, cash submission.

Atomic audit:

- Strong operator-centered route.
- Should be mobile-first because it is used at the station.
- Needs browser QA for no active shift, assigned nozzles, reading entry, dip entry, cash submission, errors, and offline/slow API states.

Release expectation:

- Add mobile nav and viewport tests before treating this as production station workflow.

### `/operations`

Purpose: supervisor operations overview.

Observed behavior: station selection, operations overview, open day/shift, close/approve/resolve actions.

Atomic audit:

- Useful Phase 3 control surface.
- Still needs confirmation/reason on approve/resolve/close where appropriate.
- Permission-aware disabling would reduce failed actions.

Release expectation:

- Add drilldowns for shifts, attendants, assignments, exceptions, and day lock status.

### `/inventory`

Purpose: inventory overview.

Observed behavior: station inventory overview.

Atomic audit:

- Good Phase 4 visibility surface.
- It should link to tank ledger, opening balance, reconciliation, procurement receipts, and risk exceptions.

Release expectation:

- Add drilldowns and historical filters.

### `/reconciliation`

Purpose: tank/day reconciliation.

Observed behavior: reconciliation overview and actions.

Atomic audit:

- Important control route.
- Adjust/seal actions should use confirmation and reason capture.
- Role-gated UI should be explicit.

Release expectation:

- Add evidence view: opening balance, receipts, sales, closing dip, variance, threshold, adjustment history.

### `/procurement`

Purpose: procurement dashboard.

Observed behavior: station procurement overview and outstanding payables summary.

Atomic audit:

- Useful Phase 5 overview.
- Formatting check fails on this file.
- Does not alone expose full purchase order planning/lifecycle.

Release expectation:

- Add purchase order list/detail/create/edit/submit/approve routes or ensure they are covered in receiving workflow.

### `/procurement/receiving`

Purpose: receive fuel, record invoices, resolve discrepancies, approve invoice.

Observed behavior: station, tank, product, supplier, purchase order, receipt, invoice, discrepancy actions.

Atomic audit:

- One of the more complete later-phase web workflows.
- Formatting check fails on this file.
- Needs browser QA because the form has many dependent selectors and state transitions.

Release expectation:

- Add confirmation for invoice approval and discrepancy resolution.

### `/revenue`

Purpose: revenue overview and day close.

Observed behavior: station selector, revenue overview, AR aging, close and lock day action.

Atomic audit:

- Covers a slice of Phase 6.
- Does not expose `/sales`.
- Day close/lock is too compressed for a financial control action.

Release expectation:

- Add sales/payment drilldowns and a preview-before-lock workflow.

### `/finance`

Purpose: finance overview.

Observed behavior: balance sheet, P&L, control counts, recent journal entries.

Atomic audit:

- Useful dashboard but not a finance workspace.
- Many backend finance operations are not routed.

Release expectation:

- Treat it as an overview route only; add subroutes for actual finance work.

### `/finance/close`

Purpose: period close checklist and period transitions.

Observed behavior: checklist counts and open/closing/closed period action buttons.

Atomic audit:

- Useful start.
- Reopen action is typed but not rendered.
- Lock disabled state may be too global.
- No confirmation/reason.

Release expectation:

- Add per-period blockers, reopen, reason capture, and close/lock confirmation.

### `/customers`

Purpose: customer list and credit alerts.

Observed behavior: list customers, show open credit alerts, scan alerts.

Atomic audit:

- Only a small Phase 8 surface.
- No customer detail or fleet workflows.

Release expectation:

- Build customer/fleet route tree before Phase 8 is considered web-complete.

### `/enterprise`

Purpose: enterprise overview and station ranking.

Observed behavior: network overview, ranking, rebuild projections.

Atomic audit:

- Good command read model.
- Projection rebuild lacks confirmation.
- Enterprise management workflows are absent.

Release expectation:

- Add subroutes for groups, scopes, policies, price rollouts, procurement plans, transfers, consolidated finance.

### `/enterprise/approvals`

Purpose: approvals and exception counts.

Observed behavior: exception count grid and pending approval rows with approve/reject.

Atomic audit:

- Useful queue starter.
- Reject has no comment.
- Approve has no source detail preview.
- Exception counts do not drill down.

Release expectation:

- Add approval detail drawer and exception queue detail pages.

### `/risk`

Purpose: risk overview and open alert list.

Observed behavior: open alerts by severity, run detection, resolve alert.

Atomic audit:

- Good starter dashboard.
- Not an investigation center.
- Most backend risk workflows are hidden.

Release expectation:

- Add alert detail, investigations, rules, signals, scores, suppressions, and governance.

### `/incidents`

Purpose: station incident queue.

Observed behavior: list/create/status update incident workflow.

Atomic audit:

- Supports Phase 2 station operations.
- Should eventually feed Phase 10 risk signals and enterprise exceptions visibly.

Release expectation:

- Add linkage to risk cases once investigations are surfaced.

### `/audit`

Purpose: audit log search.

Observed behavior: audit log query/filter page.

Atomic audit:

- Important control page.
- Should become more central as high-risk actions collect reason/evidence.

Release expectation:

- Add entity-specific audit links from customer, finance, risk, procurement, and enterprise detail pages.

### `/profile`

Purpose: current user profile, sessions, password.

Observed behavior: me, sessions, revoke, change password.

Atomic audit:

- Useful self-service route.
- Should display token/session expiry if sessions remain user-visible.

Release expectation:

- Add clear "current session" state and global logout-on-revoke behavior.

## Appendix F - API Surface Without Equivalent Web Surface

This appendix lists backend route groups that exist but do not have a complete browser workflow.

### Customer Credit

Backend coverage:

- Create/update customer.
- Customer status.
- Contacts.
- Credit profile.
- Credit position.
- Credit hold.
- Customer price agreements.
- Statements.
- Credit alerts.

Web gap:

- `/customers` shows only list and open alerts.
- There is no customer detail page.
- There is no contact management UI.
- There is no credit profile or hold UI.
- There is no statement issue UI.
- There is no alert acknowledge/resolve UI.

Business risk:

- Credit control cannot be run from the app without API tooling.
- Operators may assume Phase 8 is complete because the customer page exists, but the critical actions are absent.

### Fleet Identity

Backend coverage:

- Vehicles.
- Drivers.
- Driver PIN reset.
- Fuel credentials.
- Credential validation.
- Vehicle status.
- Driver status.
- Credential status.

Web gap:

- No fleet route.
- No vehicle/driver/credential tables.
- No credential issue/suspend workflow.
- No driver PIN reset UI.
- No credential validation UI.

Business risk:

- Fleet customers cannot be onboarded or administered from the browser.
- Forecourt staff cannot validate credentials through a dedicated UI.

### Fuel Authorization

Backend coverage:

- Request authorization.
- List/get authorization.
- Fulfill authorization.
- Cancel/void.
- Limits.
- Odometer.
- Fleet consumption.

Web gap:

- No forecourt authorization page.
- No authorization queue.
- No fulfillment UI.
- No cancel/void reason capture.
- No fuel limits UI.
- No fleet consumption report.

Business risk:

- Credit/fleet sale governance is not operationally usable from the web app.

### Enterprise Governance

Backend coverage:

- Station groups.
- Group members.
- Effective station scopes.
- Scope grants.
- Approval policies.
- Approval requests.

Web gap:

- No group management UI.
- No scope grant UI.
- No approval policy UI.
- No approval-request create UI.
- Existing approval decision UI lacks comments and source detail.

Business risk:

- Enterprise access and approval governance require API tooling.

### Central Commercial Control

Backend coverage:

- Central price rollouts.
- Price rollout approve/activate.
- Central procurement plans.
- Procurement plan release.
- Stock transfers.
- Transfer approve/receive.

Web gap:

- No central price rollout UI.
- No central procurement plan UI.
- No stock transfer UI.

Business risk:

- Phase 9's main "control plane over many stations" is not usable from the browser.

### Consolidated Finance and Enterprise Reports

Backend coverage:

- Consolidated finance.
- Station KPI export.

Web gap:

- No enterprise finance page.
- No enterprise report/export UI.

Business risk:

- Executives cannot perform the cross-station finance/reporting workflows promised by Phase 9.

### Risk Signals and Rules

Backend coverage:

- List signals.
- Backfill signals.
- List rules.
- Create rules.
- Set rule status.
- Tune rules.

Web gap:

- No signal browser.
- No rule admin page.
- No dry-run, tune, activate, or pause UI.

Business risk:

- Risk engine behavior cannot be understood or governed from the browser.

### Risk Alerts

Backend coverage:

- List/get alert.
- Acknowledge.
- Investigate.
- Resolve.
- Dismiss.
- Escalate.
- Suppress.

Web gap:

- Current UI lists open alerts only.
- Only resolve is exposed.
- Resolution has no disposition choice.
- No detail/evidence route.

Business risk:

- Alert workflow is too shallow for audit, governance, and fraud operations.

### Investigations

Backend coverage:

- List cases.
- Get case timeline.
- Create case.
- Attach alert.
- Add comment.
- Add action.
- Set action status.
- Set case status.

Web gap:

- No investigations route.
- No case detail/timeline.
- No actions/comments.
- No linkage from alert to case.

Business risk:

- Phase 10 stops at detection; it does not provide the investigation operating loop in the UI.

### Risk Governance

Backend coverage:

- Governance summary.
- Suppressions.
- Pause all rules.
- Create suppression.

Web gap:

- No governance page.
- No suppression workflow.
- No engine pause confirmation.

Business risk:

- Risk users cannot control noise or document why alerts are suppressed.

### Finance Operations

Backend coverage:

- Accounts.
- Periods.
- Journals.
- Payables.
- Supplier payments.
- Cash reconciliations.
- Bank accounts.
- Deposits.
- Bank statements.
- Customer invoices/payments.
- Expenses.
- Petty cash.
- Reports.
- Exports.

Web gap:

- `/finance` is dashboard-only.
- `/finance/close` is close-only.
- No AP workspace.
- No AR workspace.
- No banking workspace.
- No expenses workspace.
- No journal workspace.
- No export workspace.

Business risk:

- Finance officers cannot operate the finance system from the browser despite Phase 7 backend completion.

## Appendix G - Atomic Release Test Plan

The following tests should be added or run before a Phase 1-10 web release claim.

### Route integrity tests

- Assert every sidebar href exists as a Next route or redirect.
- Assert no visible route points to `_not-found`.
- Assert route labels match destination semantics.
- Assert settings-only pages are not duplicated as broken top-level routes.

### Auth/session tests

- Login success stores token and expiry.
- Expired token redirects before API call.
- API 401 clears session and redirects to login.
- Logout clears session even if backend logout fails.
- Password reset request never logs token in production env.
- Password reset confirm rejects expired token.
- MFA-required login routes through OTP flow.
- Session revoke removes revoked session from list.

### Permission UX tests

- Attendant does not see finance/admin/risk actions.
- Procurement officer sees procurement actions but not finance lock.
- Finance officer sees finance actions but not risk governance.
- Auditor sees read-only audit/reporting surfaces.
- Executive sees enterprise dashboards but not station setup mutations unless granted.
- System admin sees all admin actions.

### Mobile tests

- Login fits mobile viewport.
- Dashboard has mobile navigation.
- My Shift can capture readings on mobile.
- Procurement receiving form remains usable on tablet.
- Risk alert list is readable on mobile.
- No primary layout has horizontal overflow at 375px width.

### Finance tests

- Close period requires confirmation.
- Lock period requires confirmation.
- Reopen period requires reason.
- Close blockers drill down to source records.
- Journal detail totals balance.
- Export action records audit and downloadable result.

### Procurement tests

- Create supplier.
- Create purchase order.
- Submit/approve purchase order.
- Receive against purchase order.
- Record invoice.
- Create discrepancy on mismatch.
- Resolve discrepancy.
- Approve invoice.
- Verify payable appears in finance/AP.

### Revenue tests

- Sales recognition appears after shift approval.
- Revenue day preview shows source shifts, tenders, COGS, margin, and AR.
- Lock revenue day requires confirmation.
- Locked day blocks mutation.
- AR aging links to customers/invoices.

### Customer/fleet tests

- Create customer.
- Update credit profile.
- Put customer on credit hold.
- Create vehicle.
- Create driver.
- Issue credential.
- Validate credential.
- Request authorization.
- Fulfill authorization.
- Cancel authorization with reason.
- Record odometer.
- Generate and issue statement.
- Acknowledge and resolve credit alert.

### Enterprise tests

- Create station group.
- Add/remove station group member.
- Grant enterprise scope.
- View effective stations.
- Create approval policy.
- Raise approval request.
- Approve request with evidence.
- Reject request with comment.
- Create central price rollout.
- Approve and activate rollout.
- Create central procurement plan.
- Release plan.
- Create stock transfer.
- Approve and receive transfer.
- View consolidated finance.
- Export station KPI report.

### Risk tests

- Backfill signals.
- List signals by type.
- Create risk rule.
- Dry-run/tune rule if supported.
- Activate/pause rule.
- Run detection.
- Open alert detail.
- Acknowledge alert.
- Escalate alert.
- Create investigation from alert.
- Add investigation comment.
- Add investigation action.
- Complete action.
- Resolve case.
- Dismiss false positive with reason.
- Create suppression with expiry.
- Pause risk engine with confirmation.

### Contract tests

- Every server route has OpenAPI path coverage.
- Every OpenAPI path has SDK method coverage or an intentional exclusion.
- SDK response schemas match handler response payloads.
- Frontend pages do not cast later-phase core objects from `unknown`.

### Data isolation tests

- Restricted station user cannot read out-of-scope station resources.
- Tenant A cannot access Tenant B resources by ID guessing.
- Enterprise scope grants expand only intended stations.
- Customer account data does not leak across customers.
- Fleet credential validation is tenant-scoped.
- Risk investigation data is tenant-scoped.

### Audit tests

- Every state transition writes audit.
- Audit entries include actor, tenant, entity, previous value, new value, and reason when supplied.
- Terminal financial/risk/procurement actions include user-entered reason.
- Audit log filters find actions by entity and actor.

## Appendix H - Remediation Backlog by Phase

### Phase 1 backlog

- Remove production reset-token logging.
- Enforce frontend token expiry.
- Add global 401 handling.
- Add security headers.
- Add mobile navigation.
- Add route existence tests.
- Migrate lint from `next lint` to ESLint CLI.
- Decide auth storage future: localStorage with controls or HttpOnly cookies.

### Phase 2 backlog

- Fix sidebar tank/pump route destinations.
- Re-run old Phase 2 audit findings and close remaining items.
- Add browser tests for product/tank/pump/nozzle/calibration flows.
- Add calibration upload preview QA.
- Add status-change confirmation and reason capture.
- Add route-level permission visibility.

### Phase 3 backlog

- Re-run old Phase 3 audit findings and close remaining items.
- Add mobile QA for `/my-shift`.
- Add confirmation for approve/resolve/close where needed.
- Add drilldowns on `/operations`.
- Add browser tests for attendant and supervisor roles.

### Phase 4 backlog

- Add inventory ledger drilldown.
- Add opening balance workflow clarity.
- Add reconciliation evidence panel.
- Add adjustment reason enforcement in UI.
- Add historical filters.
- Add risk/variance handoff links.

### Phase 5 backlog

- Fix Prettier failures in procurement pages.
- Fix unused procurement helpers or remove them.
- Add purchase order detail/edit/approval UI if not fully covered.
- Add supplier detail route.
- Add invoice approval confirmation.
- Add discrepancy evidence and reason capture.
- Add payable handoff visibility.

### Phase 6 backlog

- Add `/sales` or remove nav link.
- Add sale/payment detail routes.
- Add revenue day preview before lock.
- Add close/lock confirmation.
- Add exact decimal formatting.
- Add AR aging drilldown to customers/invoices.

### Phase 7 backlog

- Add accounts route.
- Add accounting periods route separate from close console.
- Add journals route.
- Add payables route.
- Add supplier payments route.
- Add banking route.
- Add customer invoices route.
- Add customer payments route.
- Add expenses route.
- Add petty cash route.
- Add reports route.
- Add exports route.
- Add reopen period UI with reason.

### Phase 8 backlog

- Add customer detail route.
- Add customer contacts UI.
- Add credit profile/position UI.
- Add credit hold UI.
- Add customer price agreement UI.
- Add vehicle management UI.
- Add driver management UI.
- Add credential management UI.
- Add forecourt authorization UI.
- Add authorization queue.
- Add fuel limit UI.
- Add odometer UI.
- Add fleet consumption report.
- Add statement issue UI.
- Add credit alert workflow UI.

### Phase 9 backlog

- Add station group UI.
- Add group membership UI.
- Add enterprise access/scope UI.
- Add approval policy UI.
- Add approval request create/detail UI.
- Add approval decision comments.
- Add central price rollout UI.
- Add central procurement plan UI.
- Add stock transfer UI.
- Add consolidated finance UI.
- Add station KPI export UI.
- Add enterprise exception drilldowns.

### Phase 10 backlog

- Add risk signal browser.
- Add risk rule admin.
- Add detection run history.
- Add alert detail route.
- Add alert status transition workflow.
- Add investigation list.
- Add investigation detail timeline.
- Add investigation comments/actions.
- Add score explanation view.
- Add suppression UI.
- Add risk governance UI.
- Add engine pause confirmation.
- Add risk audit drilldowns.

## Appendix I - Definition of Done for Future Phase Claims

For each future phase, require these artifacts before marking the phase done:

1. Roadmap acceptance criteria checked line-by-line.
2. Migrations with up/down review.
3. Backend route table review.
4. Permission and station/tenant scope review.
5. Domain repository tests.
6. DB-backed API integration tests.
7. SDK methods and generated or validated types.
8. OpenAPI paths and schemas.
9. Web route(s) for every user-facing workflow.
10. Role-aware UI behavior.
11. Browser QA for desktop and mobile.
12. Confirmation/reason pattern for high-risk actions.
13. Audit/outbox evidence for state transitions.
14. Formatting, lint, typecheck, build, tests passing.
15. Documentation update explaining what is intentionally out of scope.

The key lesson from this audit is that backend/API completion and web product completion must be tracked separately. Phase 8-10 are the clearest examples: the backend has real depth, while the web app still exposes only a narrow control layer. Future phase signoff should use two gates:

- Backend gate: schema, domain logic, API, permissions, tests, contract.
- Product gate: routes, workflows, UX, browser tests, role behavior, audit evidence.

Only when both gates pass should a phase be called complete.
