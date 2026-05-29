# Audit 18 — Testing Strategy & Coverage

**Subject:** FuelGrid OS (Go + chi + pgx/v5 + Postgres 16 backend; Next.js 15 / React 19 frontend; TypeScript SDK).
**Mode:** Read-only, atomic-level. No source was modified; the only write is this report.
**Auditor stance:** Brutally honest. Where I am uncertain I say so. Citations are `file:line` against the repo at audit time.

---

## 1. Scope & inventory of what exists

### 1.1 Test files discovered (the complete set)

Go test files — **16 total**, **69 `Test*` functions**:

| File | `Test*` funcs | Kind | Executes in CI? |
|---|---|---|---|
| `services/api/internal/server/server_test.go` | 2 | HTTP unit (no deps) | **Yes** |
| `services/api/internal/server/phase2_integration_test.go` | 6 | DB+Redis integration | No (skipped) |
| `services/api/internal/server/phase3_integration_test.go` | 6 | DB+Redis integration | No (skipped) |
| `services/api/internal/server/phase4_integration_test.go` | 6 | DB+Redis integration | No (skipped) |
| `services/api/internal/server/phase5_integration_test.go` | 1 | DB+Redis integration | No (skipped) |
| `services/api/internal/server/phase6_integration_test.go` | 1 | DB+Redis integration | No (skipped) |
| `services/api/internal/server/phase7_integration_test.go` | 7 | DB+Redis integration | No (skipped) |
| `services/api/internal/server/phase8_integration_test.go` | 5 | DB+Redis integration | No (skipped) |
| `services/api/internal/server/phase9_integration_test.go` | 5 | DB+Redis integration | No (skipped) |
| `services/api/internal/server/phase10_integration_test.go` | 4 | DB+Redis integration | No (skipped) |
| `internal/calibration/interpolate_test.go` | 4 | pure unit | **Yes** |
| `internal/calibration/csv_test.go` | 2 | pure unit | **Yes** |
| `internal/readings/meter_test.go` | 2 | pure unit | **Yes** |
| `internal/identity/totp/totp_test.go` | 4 | pure unit | **Yes** |
| `internal/identity/password/hasher_test.go` | 5 | pure unit | **Yes** |
| `internal/identity/policy/policy_test.go` | 9 | unit (fake loader) | **Yes** |

**41 of the 69 test functions are integration tests gated behind `TEST_DATABASE_URL` + `TEST_REDIS_URL`.** Neither var is set anywhere in CI (`grep TEST_DATABASE_URL .github/workflows/ci.yml` → no match). Therefore in CI the entire phase 2–10 suite calls `t.Skip()` at `phase2_integration_test.go:74` and never runs. Only **28 functions** (2 server + 26 pure unit) actually execute under `go test -race ./...`.

Frontend / SDK / packages tests — **ZERO**. Globs for `apps/web/**/*.{test,spec}.*`, `packages/**/*.{test,spec}.*`, `**/{jest,vitest,playwright,cypress}.config.*`, and `**/__tests__/**` (excluding `node_modules`) return nothing in first-party code. The only `__tests__` hits are inside vendored `node_modules`.

### 1.2 What CI actually runs (`.github/workflows/ci.yml`)

Five jobs:

- **`node`** — `format:check`, `lint`, `typecheck`, **`test`**, `build`. Critically, root `package.json:18` defines `"test": "pnpm -r --if-present test"`, and **none** of the workspace packages (`apps/web`, `packages/sdk`, `packages/ui`, `packages/config`) define a `test` script (`apps/web/package.json:6-12`, `packages/sdk/package.json:11-13`, `packages/ui/package.json:12-14`). So **the `pnpm test` CI step is a guaranteed no-op** — green every time, asserting nothing.
- **`openapi`** — `redocly lint docs/openapi.yaml`. Lints the spec; does **not** verify the running API matches it (no contract test).
- **`go`** — `go mod tidy` check, `go vet`, `golangci-lint`, **`go test -race -count=1 ./...`**, `go build`. The race detector and `-count=1` (no caching) are good hygiene, but as established this exercises only 28 functions; the 41 DB-backed tests skip.
- **`migrations`** — the real workhorse. Spins up Postgres 16 + Redis, runs `migrate up → version → down-all → up`, seeds demo data, boots the API binary, and runs a battery of **bash/curl/psql assertions**: `/readyz`/`/metrics`, auth login/logout/`/me`, session revocation authority, station-scope 200-vs-403, **a second-tenant RLS isolation block** (`ci.yml:312-434`), cross-tenant FK rejection, audit+outbox atomicity on role grant, and platform tenant provisioning. This shell suite is where tenant isolation, RLS, and audit guarantees are genuinely verified — **not** in Go tests.
- **`docker`** — builds and smoke-tests the API image (`/healthz`, `/readyz`, `/metrics`).

There is **no coverage measurement** anywhere (`go test -cover`/`-coverprofile` absent; no Codecov/coverage gate). There is no load/perf job, no fuzz job, no frontend e2e job.

### 1.3 How the Go integration suite bootstraps

`setupHarness` (`phase2_integration_test.go:69-132`): connects to an **already-migrated** local Postgres via `database.Connect` and Redis via `cache.Connect`; if either env var is unset it skips. It seeds a fresh tenant (`seedTenant`, lines 160-205), spins the real `server.New(...)` on a free localhost port (`freePort`, `go srv.Start()`), waits on `/healthz` (`waitReady`, 10 s deadline polling every 50 ms), and registers a `cleanup` that shuts the server, **deletes the tenant's rows in FK-safe order** (`cleanupTenant`, lines 218-320, ~95 `DELETE … WHERE tenant_id=$1` statements), and closes Redis/pool.

This is **not** Testcontainers — it requires a developer-provisioned, pre-migrated DB. The teardown is per-test (each test calls `setupHarness`/`cleanup`), so tests are reasonably isolated by tenant. Crucially, the harness connects with the **owner/superuser** role (the same `DATABASE_URL`), **never** the `fuelgrid_app` RLS-bound role — so no Go test ever traverses the row-level-security path.

### 1.4 Fixture realism

A pervasive structural weakness: most preconditions are seeded by **raw SQL `INSERT`s** that bypass the domain layer. Examples: users/roles/tanks/nozzles in `seedTenant` (`phase2:178-203`); closed shifts and frozen close-lines in `seedClosedDayShift` (`phase4:650-673`); costed delivery movements (`phase6:35-41`); revenue-day rows (`phase9:95-100, 256-261`); `cash_reconciliations` rows for the entire Phase-10 risk suite (`phase10:27-32, 89-94, 130-135, 190-195`); AR charges (`phase8:300-305`). Raw seeding lets tests reach a downstream state quickly, but it means the **producing** code path (the close engine, the recognition engine, the reconciliation writer) is frequently *not* the thing under test — the assertions verify the *reader* given a hand-built state. Where the domain layer is exercised directly it is via the repo API (`inventory.New(h.pool).PostMovement`, `phase4:71-84`) rather than HTTP, again skipping the handler/authz layer for that step.

---

## 2. Coverage matrix — domain → unit / integration / none

~31 top-level `internal/*` packages plus 6 `internal/identity/*` subpackages and the `services/api/internal/server` handler layer (52 `*_handlers.go` files). Legend: **U** = pure Go unit test; **I** = DB-backed HTTP integration (skipped in CI); **CI-sh** = covered only by the bash `migrations` job; **none** = no automated test.

| Domain / package | Unit | Integration | Notes |
|---|---|---|---|
| `internal/calibration` | **U** (6 funcs) | I (phase2 upload/lookup/supersede) | Best-tested pure logic: interpolation, extrapolation refusal, CSV validation. |
| `internal/readings` (meter math) | **U** (2 funcs) | I (phase3) | `LitresDispensed`, `ValidateScale` rollback/precision. |
| `internal/identity/policy` | **U** (9 funcs) | CI-sh + I | Strong authZ matrix via `fakeLoader`. |
| `internal/identity/password` | **U** (5 funcs) | — | Hash/verify/pepper/rehash/malformed. |
| `internal/identity/totp` | **U** (4 funcs) | — | Enroll/verify/skew/reject. |
| `internal/identity` (service) | — | CI-sh (login/logout/revoke) + I | Login path hit by harness; service unit-untested. |
| `internal/identity/session`, `ratelimit`, `repo` | — | CI-sh / I (indirect) | No direct tests. |
| `internal/inventory` (ledger, sales, deliveries, costing, periods) | — | **I** (phase4/5/6/9) | Ledger ordering, reversal, idempotency, opening-balance, landed cost. Repo called directly. |
| `internal/reconciliation` | — | **I** (phase4) | Tolerance band, adjustment, seal, balance-forward. |
| `internal/operations` (shifts, close, exceptions, myshift) | — | **I** (phase3/4) | Day workflow, self-scope, post-close lock, zero-assignment. |
| `internal/procurement` (suppliers, PO, receipts, invoices) | — | **I** (phase5) | Single happy+discrepancy flow. |
| `internal/pricing` | — | **I** (phase6/9) | Station price set; central rollout. |
| `internal/revenue` (days) | — | **I** (phase6) | Recognition, COGS/margin, revenue day lock. |
| `internal/payments` | — | **I** (phase6) | Tender reconciliation, credit-limit refusal. |
| `internal/accounting` (journal, periods, reports, accounts, exports) | — | **I** (phase7/9) | Balanced-entry guard, period lock, trial balance, P&L, balance sheet, export checksum. |
| `internal/payables` (+ supplier payments) | — | **I** (phase7) | Import, partial pay, over-allocation, aging. |
| `internal/banking` (cash recon, deposits, statements) | — | **I** (phase7) | Cash recon variance, deposit, statement import. |
| `internal/receivables` (invoices, customer payments) | — | **I** (phase7) | Issue→AR, allocation, over-allocation. |
| `internal/expenses` (+ petty cash) | — | **I** (phase7) | Lifecycle, overdraw guard, reconcile. |
| `internal/fleet` (vehicles, drivers, credentials, authorizations, odometer, statements, credit) | — | **I** (phase8) | Credit position, authorization holds, credential validate, odometer monotonicity. |
| `internal/enterprise` (governance, projections, central_commercial) | — | **I** (phase9) | Groups, scope grants, approval engine, projections, transfers. |
| `internal/risk` (signals, scoring, investigations, governance) | — | **I** (phase10) | Detection idempotency, scoring, cases, suppression. |
| `internal/products`, `tanks`, `pumps`, `nozzles`, `stations`, `regions`, `companies`, `incidents` | — | I (phase2 partial) | Only products/tanks/pumps/nozzles touched; stations/regions/companies/incidents CRUD largely untested in Go. |
| `internal/events` (outbox, publisher, bus) | — | CI-sh (publish tick) + I (atomicity) | Publisher loop verified only in bash. |
| `internal/audit` | — | CI-sh + I (atomicity) | |
| `internal/database` (incl. RLS `WithTenant`) | — | CI-sh only | **RLS never tested from Go.** |
| `internal/cache`, `observability` | — | CI-sh (metrics/readyz) | |
| **`apps/web` (Next.js UI)** | **none** | **none** | No component/RTL/e2e tests at all. |
| **`packages/sdk` (TS client)** | **none** | **none** | No unit/contract tests; only `typecheck`. |
| **`packages/ui`, `packages/config`** | **none** | **none** | |

**Rough quantification:** of ~37 Go packages, **5 have true unit tests** (calibration, readings, policy, password, totp) and roughly **20 have HTTP integration coverage** that is *skipped in CI*. Of the 52 handler files, the happy path of each phase's headline flow is covered, but per-endpoint negative/error coverage is thin and **CI executes none of it**. The frontend and SDK have **0% test coverage**.

---

## 3. Test quality assessment

The integration suite is materially better than "happy-path smoke tests." It does assert real invariants and a respectable set of negative paths. Concretely:

**Negative / error paths that ARE tested** (status-code level): out-of-scope tank read → 403 (`phase2:410`); cross-station filter → 403 (`phase2:414`); calibration out-of-range → 422 (`phase2:471`); invalid CSV → 400 (`phase2:475`); soft-delete of in-use product/tank → 409 (`phase2:524,528`); revive decommissioned pump → 409 (`phase2:557`); attendant writing a non-assigned nozzle → 403 (`phase3:213,221`); post-close correction → 409 (`phase3:282`); zero-assignment close → 422 (`phase3:305`); cross-station reading → 400 (`phase3:335`); delivery before opening balance → 409/`ErrNoOpeningBalance` (`phase4:202,339`); re-set opening → 409 (`phase4:243`); second reversal → `ErrAlreadyReversed` (`phase4:153`); reconcile-before-approval → 409 (`phase4:480`); seal over tolerance → 409 (`phase4:509`); over-allocation of supplier/customer payments → 422 (`phase7:155,394`); unbalanced journal → 422 (`phase7:65`); post into locked period → 409 (`phase7:90`); re-reverse → 409 (`phase7:79`); over-limit credit charge → 422 (`phase6:89`); insufficient-credit / account-hold authorization denials with rule codes (`phase8:198,224`); double-fulfill → 409 (`phase8:214`); over-transfer → 422 (`phase9:212`); duplicate cash reconciliation → 409 (`phase7:240`); double deposit → 409 (`phase7:291`); re-import statement → 409 (`phase7:304`); re-lock revenue day → 409 (`phase6:115`).

**Real invariants that ARE asserted:**
- **Debits == credits enforcement** is tested *indirectly*: the unbalanced journal entry is rejected with 422 (`phase7:59-67`), and the **trial balance `balanced` flag is asserted true** after every multi-step finance flow (`phase7:185,319,410,493`). So a regression that let an unbalanced entry post, or that broke trial-balance summation, would likely be caught — *if the tests ran*.
- **Recognized revenue with COGS/margin**: `phase6:57-69` asserts gross 12,390,000 / COGS 10,080,000 / margin 2,310,000 on approval, and that a `sales_revenue`-crediting journal produces P&L revenue 5,000 and net profit 5,000 (`phase7:188-191`).
- **Credit-limit blocks**: a 6,000 charge against a 5,000 limit → 422 (`phase6:89`), and the fleet authorization engine denies with `insufficient_credit` after a hold consumes available credit (`phase8:195-200`).
- **Stock ledger conservation**: running balances asserted per-movement (`phase4:115-125`), reversal nets to a known balance (`phase4:161`), balance-forward across days (`phase4:555`).
- **RLS isolation & cross-tenant FK rejection**: asserted — but **only in the bash `migrations` job** (`ci.yml:357-434`), counting 2 vs 1 vs 0 stations across tenant contexts and the fail-closed (no context → 0 rows) case.
- **Idempotency**: re-posting a shift's sales is a no-op (`phase4:424`); projection rebuild does not double-count (`phase9:124-129`); risk detection is idempotent while an alert is open (`phase10:65`); export checksum is reproducible (`phase7:540`).

**What is conspicuously NOT tested:**
- **Balance sheet `A = L + E` is never asserted.** `phase7:192` only checks `assets == "5000.00"` (a single hand-built cash sale). No test posts a realistic mix and asserts assets equal liabilities + equity. The trial-balance `balanced` flag checks Σdebits = Σcredits, which is a *weaker* invariant than the accounting equation holding across the balance-sheet classification — the exact gap a sibling audit flags (audit theme c).
- **No concurrency / race test of any business invariant.** `grep` for `go func`, `sync.WaitGroup`, `errgroup` in tests finds only `go func(){ srv.Start() }` (`phase2:118`). No test fires two simultaneous credit charges, two reconciliation seals, or two sales postings to probe a credit-limit race, a reconciliation TOCTOU, or a double-spend. `go test -race` runs, but with no concurrent test bodies it can only catch races the HTTP server itself triggers under single-client load.
- **Money rounding / property tests:** none. No `testing/quick`, no fuzz, no table of rounding edge cases. Money in the accounting layer is decimal-as-string (`accounting/journal.go:44-55`, `money0(...)`), which is sound, **but litres and several derived figures are `float64`** (`inventory/repo.go:79-80` `Litres/BalanceAfter float64`; `inventory/deliveries.go:22-78`, with the literal comment at line 56 "carry currency through float64"). No test asserts float64 accumulation stays exact across many movements; the existing ledger tests use clean integers (20000/10000/4200) that never expose float drift.
- **Tenant isolation from Go:** the Go suite seeds one tenant per test and never logs in as a *second* tenant to attempt IDOR against the first via the HTTP API. Cross-tenant 404 is checked only in bash (`ci.yml:376-384`). The app-layer `WHERE tenant_id = ?` filter is therefore unguarded by any Go test.
- **Separation of duties / self-approval:** no test verifies that the *creator* of a journal entry, supplier invoice, expense, or approval request is *blocked from approving their own* item. Every finance/approval test uses the **same `admin` token** to both create and approve (`phase7` expenses `submit→approve→post` all as admin; `phase9` raises and decides an approval as the same actor at `phase9:64`). A sibling audit flags self-approval as present-and-unguarded (theme d); the suite would not catch it because it never even attempts the two-actor scenario as a failure case.

**Status-code discipline** is generally good — tests assert specific codes (403 vs 404 vs 409 vs 422), which is more rigorous than asserting "not 200." The harness decodes JSON and asserts on field values, not just shape.

---

## 4. Would the suite catch the known critical bugs?

For each theme from sibling audits, the verdict and evidence:

| # | Known critical bug (theme) | Would tests catch it? | Evidence |
|---|---|---|---|
| a | **RLS inert at runtime / cross-tenant isolation** | **No (Go); Partial (CI bash only)** | No Go test connects as `fuelgrid_app` or sets `app.current_tenant`; the harness uses the superuser pool (`phase2:78`), which *bypasses* RLS. The only isolation assertions live in `ci.yml:386-434`. If RLS were silently disabled in app runtime (e.g. `WithTenant` not called on a pool checkout), the bash test that sets the GUC manually would still pass while the *application* leaked — the bash test does not exercise the app's own connection-acquisition path. So a runtime-inert RLS bug would **escape**. |
| b | **float64 for money / litres** | **No** | Litres are `float64` (`inventory/repo.go:79`); delivery currency rides float64 (`deliveries.go:56`). All ledger tests use round integers that never trigger float representation error; no property/fuzz test exists. A 0.1+0.2-class drift across many movements is untested. |
| c | **Balance sheet doesn't balance (A≠L+E); revenue never recognized** | **Partial / No** | Revenue recognition *is* asserted (`phase6:57-69`, `phase7:188`) and trial-balance `balanced` is checked, so a "revenue never recognized" regression in the *tested* path would likely fail. But **A=L+E is never asserted** — `phase7:192` checks only `assets=="5000.00"`. And these tests are **skipped in CI**, so even the partial coverage provides no live protection. |
| d | **Separation of duties absent (self-approval)** | **No** | Every approval/finance test uses one admin actor as both maker and checker (e.g. `phase9:57-66`, `phase7:448-452`). No test asserts a self-approval is *rejected*. |
| e | **Credit-limit race / holds not enforced on the real sale path** | **No (race); Partial (single-threaded)** | Single-threaded credit-limit refusal is tested on `/payments` (`phase6:89`) and on `/fuel-authorizations` (`phase8:195`). But there is **no concurrent** double-charge test, and the credit check is exercised via the *tender/authorization* endpoints, not necessarily the same code path a real metered sale draws down. A TOCTOU where two requests each see headroom and both commit would **escape** entirely. |
| f | **Reconciliation TOCTOU (compute-before-tx)** | **No** | `phase4` reconciliation tests are strictly sequential; the preview-then-persist and seal flows are never run concurrently. No test interleaves a second mutation between compute and commit. |
| g | **SDK fetch "Illegal invocation" (no SDK/web tests)** | **No** | The fix already exists (`packages/sdk/src/client.ts:151` `fetch.bind(globalThis)` with an explanatory comment), but there is **no test** guarding it. A future refactor reintroducing `this.fetchImpl(...)` unbound would ship green — `pnpm test` is a no-op and there are zero SDK tests. |

**Net:** of the seven known critical bugs, the suite as configured in CI would catch **none of them at merge time** (all relevant coverage is either skipped, single-threaded, or bash-only). Even with the integration suite run locally, only theme (c)'s "revenue recognized" sub-case has a real chance of failing.

---

## 5. Test hygiene, flakiness & infrastructure gaps

**Cleanup / teardown — good.** Every integration test pairs `setupHarness`/`defer cleanup()`; `cleanupTenant` deletes in FK-safe order (`phase2:218-320`). Tenants and Redis keys are namespaced by `time.Now().UnixNano()` (`phase2:92,167`), so parallel or repeated runs against a shared DB don't collide. Sessions/ratelimit use a per-run Redis prefix (`phase2:92-94`).

**Test interdependence — low.** Each `Test*` builds its own tenant; no ordering dependency between functions. Within a function, steps are sequential by design.

**Shared state — contained but real.** All tests share one Postgres + Redis instance. System tables (`roles WHERE is_system`, `phase2:210`) and the default chart-of-accounts seeding are global assumptions; a migration that renamed a system role code would break many tests simultaneously. Acceptable for an integration suite.

**Flakiness risk — moderate.**
- `waitReady` polls `/healthz` with a 10 s deadline and 50 ms sleeps (`phase2:144-158`) — reasonable, not a hardcoded sleep.
- The bash `migrations` job, by contrast, leans on **`sleep 0.5` retry loops** and a publisher-tick race window (`ci.yml:161-167, 493-507`). The outbox-publish assertion waits up to 10×500 ms for `published_at` to flip — a slow CI runner could flake. This is the most timing-fragile part of the whole test apparatus.
- Date literals are hardcoded (`"2026-06-01"`, etc.). These are fixed business dates, not `now()`, so they won't rot — but `seedFirstDip` uses `time.Now().Format(...)` (`phase4:708`), mixing absolute and relative dates; a test run near a month boundary that combines both could in principle interact with period-window logic. Low probability, worth noting.

**No parallelism in the heavy suite.** Integration tests do **not** call `t.Parallel()` (only `server_test.go` does). Sequential execution is safe but slow; more importantly it means the suite *cannot* surface concurrency bugs even accidentally.

**Fixture realism — weak (see §1.4).** Raw-SQL seeding bypasses domain validation, so tests frequently assert reader correctness over a hand-built state rather than exercising the real write entry point.

**Missing infrastructure — extensive:**
- **No frontend tests** (RTL/Jest/Vitest/Playwright) — zero. The entire Next.js app (forms, optimistic mutations, auth/session UI, money formatting) is unverified by automation.
- **No SDK tests** — the typed client that every screen depends on has no unit or contract test; `pnpm test` is a no-op.
- **No contract test against `docs/openapi.yaml`** — the spec is linted but never diffed against the live API. Drift between SDK types, OpenAPI, and handler responses is undetected.
- **No coverage measurement or gate** — no `-coverprofile`, no minimum-coverage check; coverage trend is invisible.
- **No load / performance / soak tests** — no k6/vegeta/Locust; throughput, connection-pool exhaustion, and N+1 behavior are unmeasured.
- **Migration testing is round-trip but shallow** — the `migrations` job does `up → down-all → up → seed` (`ci.yml:132-147`), which proves the down files don't error and the schema re-applies; it does **not** assert per-migration reversibility, schema equivalence after a round-trip, or data-preserving migrations. All 63 migrations have `.down.sql` files present.
- **No property / fuzz tests for money math** despite the float64 exposure.
- **No mutation testing** to gauge assertion strength.

---

## 6. Findings register

| ID | Severity | File:Line | Issue | Fix |
|---|---|---|---|---|
| TEST-01 | **Critical** | `.github/workflows/ci.yml:62-93` | The 41 DB-backed integration tests (the bulk of business-logic verification) **never run in CI** — no `TEST_DATABASE_URL`/`TEST_REDIS_URL` is provided, so `go test -race ./...` skips them. CI green-lights merges while exercising only 28 of 69 test functions. | Add a Postgres+Redis service to the `go` job (or a new `go-integration` job), run migrations, then `TEST_DATABASE_URL=… TEST_REDIS_URL=… go test -race -count=1 ./...`. Fail the build if any phase test is skipped unexpectedly. |
| TEST-02 | **Critical** | root `package.json:18`; `apps/web/package.json:6-12`; `packages/sdk/package.json:11-13` | The `node` job's `pnpm test` step is a **no-op** — no workspace defines a `test` script. CI's "Test" gate asserts nothing for the entire frontend + SDK. | Add Vitest (SDK + UI utils) and Playwright/RTL (web) with real `test` scripts; the `-r --if-present` wiring will then execute them. |
| TEST-03 | **Critical** | `packages/sdk/` (whole), `apps/web/` (whole) | **Zero** frontend/SDK tests. Regressions like the `fetch` "Illegal invocation" bug (`client.ts:151`) — already fixed by hand — have no guard and would silently reship. | Add SDK unit tests (request building, token injection, error mapping, the `fetch.bind` invariant) and at least smoke e2e for login + one mutation flow. |
| TEST-04 | **High** | `phase2_integration_test.go:78`; `internal/database/tenant.go` | **RLS is never tested from Go.** The harness uses the superuser pool, bypassing RLS; isolation is asserted only by manual `SET LOCAL app.current_tenant` in bash (`ci.yml:386-434`), which does not exercise the app's own connection-acquisition path. A runtime-inert RLS bug escapes. | Add a Go test that acquires a connection through the production `WithTenant`/`fuelgrid_app` path and asserts cross-tenant rows are invisible and that no-context fails closed. |
| TEST-05 | **High** | `phase7_integration_test.go:192` | Balance-sheet integrity is checked only as `assets=="5000.00"` on a single hand-built sale; **A = L + E is never asserted**, and the trial-balance flag is a weaker proxy. The "balance sheet doesn't balance" bug would not be caught. | Add an assertion that `assets == liabilities + equity` after a realistic multi-entry scenario (sales recognition, payable, AR, expense). |
| TEST-06 | **High** | finance/approval tests, e.g. `phase9:57-66`, `phase7:448-452` | **No separation-of-duties test.** Maker and checker are always the same admin token; self-approval is never asserted to be rejected. | Add two-actor tests (creator A, approver B) and a negative case asserting A cannot approve their own journal/invoice/expense/approval-request. |
| TEST-07 | **High** | inventory/payments tests (no concurrency anywhere) | **No concurrency tests.** Credit-limit race (theme e), reconciliation TOCTOU (theme f), and double-sale/double-seal are untested; `-race` finds nothing because no test runs concurrent bodies. | Add tests firing N simultaneous credit charges / seals via goroutines and assert exactly one succeeds and invariants hold. |
| TEST-08 | **High** | `internal/inventory/repo.go:79-80`; `deliveries.go:56` | **Money/litres use `float64`** and no property/fuzz test guards accumulation error; ledger tests use clean integers that never expose drift. | Migrate money/litres to decimal (or integer minor units), and add a property test summing many fractional movements to assert exactness. |
| TEST-09 | **Medium** | `phase2:178-205`; `phase4:650-673`; `phase10:27-32` | **Raw-SQL fixtures bypass the domain layer**, so many tests verify readers over hand-built state instead of the real write path (close engine, recon writer, risk source facts). | Where feasible, build fixtures through the public repo/HTTP API so producing code is under test; reserve raw SQL for states the API genuinely cannot create. |
| TEST-10 | **Medium** | `phase2:78` (Go suite has no second-tenant HTTP IDOR test) | App-layer cross-tenant IDOR via the HTTP API is checked only in bash (`ci.yml:376-384`); no Go test logs in as tenant B and probes tenant A's resources for 404. | Add a Go integration test seeding two tenants and asserting tenant B receives 404 on tenant A's IDs across representative endpoints. |
| TEST-11 | **Medium** | (absent) `docs/openapi.yaml` vs handlers | **No contract test.** OpenAPI is linted but never diffed against live responses; SDK/spec/handler drift is undetected. | Add a contract test (e.g. schemathesis or a response-vs-schema validator) hitting the booted API against `openapi.yaml`. |
| TEST-12 | **Medium** | `.github/workflows/ci.yml` (no `-cover`) | **No coverage measurement or gate.** Coverage trend and dead spots are invisible. | Emit `-coverprofile`, upload, and set a non-regressing threshold for the business packages. |
| TEST-13 | **Medium** | `ci.yml:161-167, 493-507` | **Timing-fragile bash assertions** — outbox-publish and readiness rely on `sleep 0.5` retry loops and a publisher-tick window; slow runners can flake. | Replace polling with a deterministic signal (e.g. force a publisher tick endpoint, or assert on a synchronous publish in a Go test instead of bash). |
| TEST-14 | **Low** | `ci.yml:138-142` | **Migration testing is round-trip but shallow** — proves down files don't error and schema re-applies, but not per-migration reversibility or schema equivalence. | Add a check that diffs schema after `up`→`down N`→`up` for the latest migration, and consider per-migration reversibility assertions. |
| TEST-15 | **Low** | `phase4_integration_test.go:708` | Mixed absolute (`"2026-06-01"`) and relative (`time.Now().Format`) seed dates within the suite; low risk of month-boundary interaction with period logic. | Pin all seed dates to fixed literals for determinism. |
| TEST-16 | **Low** | integration suite (no `t.Parallel()`) | Heavy suite runs strictly sequentially — slow, and structurally unable to surface concurrency issues even by accident. | Where tenant-isolation guarantees hold, enable `t.Parallel()` to speed runs; pair with explicit concurrency tests (TEST-07). |
| TEST-17 | **Info** | whole repo | **No load/perf, soak, or mutation testing.** Throughput, pool exhaustion, and assertion strength are unmeasured. | Add a smoke load test (k6) for hot paths and consider mutation testing on `internal/accounting` + `internal/inventory`. |

---

## 7. Severity counts, top risks & grade

**Severity counts:** Critical **3** · High **5** · Medium **5** · Low **3** · Info **1** — **17 findings**.

**Top 5 risks (ranked):**

1. **TEST-01 — The integration suite is skipped in CI.** The single most damaging fact in this audit: the carefully written 41-function phase suite — the only thing that verifies revenue recognition, reconciliation, credit limits, accounting balance, and authZ scoping at the HTTP level — *does not execute on any push or PR*. The team has invested in good tests that provide **zero merge-time protection**. (`ci.yml:62-93`)
2. **TEST-02 / TEST-03 — Frontend & SDK are 100% untested and the `pnpm test` gate is a no-op.** Every UI screen and every typed client call is unverified; the CI "Test" step is decorative. The already-fixed SDK `fetch` bug proves regressions here ship silently. (`package.json:18`, `packages/sdk/`)
3. **TEST-04 / TEST-10 — Tenant isolation & RLS are verified only by bash that bypasses the app's own connection path.** For a multi-tenant SaaS this is the highest-consequence correctness property, and the Go suite — the layer that actually models production access — never touches it. A runtime-inert RLS or app-layer IDOR bug would escape. (`phase2:78`, `ci.yml:386-434`)
4. **TEST-07 — No concurrency tests.** Three of the named critical bugs (credit-limit race, reconciliation TOCTOU, double-spend) are inherently concurrent; the suite is wholly sequential and cannot catch them. `-race` provides false comfort.
5. **TEST-05 / TEST-08 — Financial-integrity gaps: A=L+E unasserted and money/litres on `float64`.** The accounting equation is never checked end-to-end, and the float64 money path has no rounding/property guard — both are silent-corruption risks in the system's most sensitive domain.

**Overall coverage grade: D+.**

Rationale: The *content* of the Go integration suite is genuinely good for an early-stage product — it asserts real invariants (trial-balance balance, ledger conservation, credit refusal, post-close locks, idempotency) and disciplined status codes, which on its own would merit a C+/B-. But a test suite is only worth what CI runs, and **CI runs almost none of it**: the integration tests skip, the frontend/SDK tests don't exist, and the `pnpm test` gate asserts nothing. Tenant isolation and RLS — the defining correctness property of the platform — are validated only by timing-fragile bash that sidesteps the application's real access path. There are no concurrency, contract, property, load, or coverage-gated tests. The effective, CI-enforced safety net is thin enough that **six of seven known critical bugs would pass to main unflagged.** The grade reflects executed-and-enforced coverage, not authored intent; wiring TEST-01 alone would lift this materially.

*End of Audit 18.*
