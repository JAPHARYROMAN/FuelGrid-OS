# FuelGrid OS — Audit 01: Architecture, Build & Tooling

**Auditor scope:** Architecture (30,000-ft), build pipeline, and infrastructure/tooling. Domain
handlers and repos are deliberately out of scope (covered by other agents). This is a read-only
review; no source was modified.

**Files reviewed (representative):**

- Monorepo root: `go.mod`, `go.sum`, `package.json`, `pnpm-workspace.yaml`, `pnpm-lock.yaml`,
  `.gitignore`, `.dockerignore`, `.editorconfig`, `.gitattributes`, `.nvmrc`, `.env.example`,
  `eslint.config.mjs`, `prettier.config.mjs`, `.prettierignore`, `.golangci.yml`, `redocly.yaml`,
  `docker-compose.yml`, `.husky/pre-commit`, `CONTRIBUTING.md`.
- Workspace packages: `packages/config/*` (tsconfig base/node/next, eslint, prettier, tailwind),
  `packages/sdk/{package.json,src/*}`, `packages/types/`, `packages/ui/`, `apps/web/{package.json,
  tsconfig.json}`, `apps/web/src/app/**`, `apps/mobile/`.
- Go layering: `services/api/internal/server/server.go` (865 lines, the composition root),
  `services/api/internal/server/{handlers,middleware,auth_middleware,platform_handlers,
  metrics_handler}.go`, `internal/database/{postgres,tenant}.go`, `internal/audit/tx.go`,
  `internal/events/publisher.go`, `internal/observability/metrics.go`.
- Config + entrypoints: `services/api/internal/config/config.go`,
  `services/api/cmd/{api,migrate,seed}/main.go`.
- Build/deploy: `services/api/Dockerfile`, `services/api/Makefile`, `.github/workflows/ci.yml`.
- Migrations: all 63 `services/api/migrations/00NN_*.{up,down}.sql` (RLS coverage, role model).
- API contract: `docs/openapi.yaml` (2020 lines).
- Docs: `docs/{architecture,blueprint,genesis-manifesto,prd,deployment,multi-tenancy,
  db-conventions}.md` and the `roadmap-phase-*.md` set.

A live `go build ./...` was run and **passes cleanly (exit 0)** on the as-committed tree.

---

## 1. Monorepo Layout & Module Boundaries

The repo is a polyglot monorepo with a single Go module (`github.com/japharyroman/fuelgrid-os`,
`go.mod:1`) rooted at the top level, and a pnpm workspace (`pnpm-workspace.yaml`) covering
`apps/*`, `services/*`, and `packages/*`. This is a clean, conventional shape: Go code lives under
`internal/<domain>` (30 domain packages) and `services/api`, while TS code lives under `apps/web`,
`apps/mobile`, and `packages/{config,sdk,types,ui}`. The pnpm lockfile is present and committed
(`pnpm-lock.yaml`, 166 KB), Node is pinned to 22 via `.nvmrc`, and the package manager is pinned to
`pnpm@10.28.2` via the root `packageManager` field — all good for reproducibility.

**Shared config is well-factored.** `packages/config` exports base/node/next tsconfigs, base/next
ESLint flat configs, a Prettier config, and a Tailwind preset via a clean `exports` map
(`packages/config/package.json`). The root `eslint.config.mjs` and `prettier.config.mjs` are
one-line re-exports of the shared package — exactly the right pattern. `tsconfig.base.json` is
strict and modern: `strict`, `noUncheckedIndexedAccess`, `noImplicitOverride`,
`noUnusedLocals/Parameters`, `verbatimModuleSyntax`, `isolatedModules`. This is a high-quality TS
baseline.

**Two workspace members are empty placeholders.** `packages/types/` and `apps/mobile/` contain only
a `.gitkeep` — no `package.json`, no source. Because `pnpm-workspace.yaml` globs `apps/*` and
`packages/*`, pnpm scans these directories and finds nothing installable. This is mostly harmless
today but is a latent footgun: any workspace tooling that assumes every globbed directory is a real
package (and `@fuelgrid/types` is *not* referenced by any consumer, unlike `@fuelgrid/sdk`/`ui`/
`config`) will produce confusing "no package found" diagnostics. (Finding ARCH-09.)

**The SDK is hand-written, not generated.** `packages/sdk/package.json` points `main`/`types`
straight at `./src/index.ts` with no build step, and `src/client.ts` is 2,953 lines of
hand-maintained fetch calls plus `src/types.ts` (1,343 lines). `index.ts` re-exports types for
*every* phase (risk, enterprise, finance, fleet). This is workable at the current size but is a
maintenance tax that scales linearly with the API surface, and it is the de-facto contract — see
§4 for why that matters relative to the stale OpenAPI spec.

**`apps/web` lint command is stale for Next 15.** `apps/web/package.json` defines
`"lint": "next lint"`. `next lint` is deprecated as of Next 15 (slated for removal) and bypasses the
repo's shared flat ESLint config in favor of `eslint-config-next`. The web app has no local
`eslint.config.*`, so under the root `pnpm lint` (`pnpm -r ... lint`) the web package lints with a
*different* ruleset than every other package. (Finding ARCH-10.)

---

## 2. Go Layering & Dependency Direction

This is the strongest part of the codebase. The dependency direction is clean and the house
convention ("domain logic & repos in `internal/<domain>`; HTTP in `services/api/internal/server`")
is respected.

**`internal/<domain>` is free of HTTP concerns.** A repo-wide grep for `net/http`, `go-chi/chi`, or
imports of the server/config packages inside `internal/` returns **exactly one hit, and it is a doc
comment** (`internal/observability/metrics.go:5`). No domain package imports chi, `net/http`, or the
server. Repos take a `database.Querier` (`internal/database/postgres.go:32`) — an interface
satisfied by both `*pgxpool.Pool` and `pgx.Tx` — so the same method runs auto-committed or inside a
caller's transaction. This is the mechanism that lets one transaction wrap a business change + its
audit + outbox rows, and it is implemented exactly as the convention prescribes
(`internal/audit/tx.go:35` `WriteWithOutbox`).

**The composition root is `server.New` (`server.go:105`).** It is a manual DI assembly: `Deps`
(DB, Redis, Identity, Policy, Metrics) is built in `cmd/api/main.go` and passed in; `New` then
constructs ~26 domain repos (`server.go:117-145`) guarded by `if deps.DB != nil`, so a thin
DB-less boot (for `/healthz` smoke tests) still works. Route registration is gated layer by layer:
`if s.identity != nil` → auth routes; `if s.policy != nil` → permissioned routes; per-feature
`if s.companies != nil` etc. This degradation model is coherent and well-commented.

**`server.go` is a god-file of route wiring (865 lines, ~237 route registrations).** Every route in
the entire product — Phases 1 through 10, from `/auth/login` to `/risk/suppressions` — is declared
in a single `New` function. The file is readable thanks to disciplined section comments, but it is a
merge-conflict magnet and a single 700-line `func` that no reviewer can hold in their head. It also
couples the lifecycle (`Start`/`Shutdown`) to the route table. The natural refactor is per-domain
`registerXRoutes(r chi.Router)` methods in their respective `*_handlers.go` files, called from a
slim `New`. This is a maintainability finding, not a correctness one. (Finding ARCH-04.)

**Middleware stack ordering has a real defect.** `server.go:149-162` orders:
`RequestID → echoRequestID → logRequests → recordMetrics → Recoverer → Timeout → CORS`.
`Recoverer` is **fifth**, after `logRequests` and `recordMetrics`. chi middleware wraps
outside-in, so a panic that escapes a handler is caught by `Recoverer` *before* it can unwind
through `recordMetrics`/`logRequests` (those run earlier in the chain / outside Recoverer), which is
actually fine for those two because their work is deferred. **But** anything that panics *inside*
`logRequests` or `recordMetrics` themselves (or in `RequestID`/`echoRequestID`) is **not** recovered
and will crash the connection. Conventionally `Recoverer` is placed first (just after `RequestID`)
so the entire downstream chain is protected. The current ordering also means the panic 500 is
emitted by `Recoverer` but the metrics/log middleware *do* still observe it via their deferred
closures (they wrap Recoverer), so observability is preserved — the only exposed gap is panics in
the outer middleware themselves. Low severity, but it's an inverted-from-idiom stack worth fixing.
(Finding ARCH-05.)

**No import cycles, no leaky abstractions found.** The `Pool` type alias (`database.Pool =
pgxpool.Pool`, `postgres.go:25`) and the `Querier` interface keep pgx from leaking domain-ward in an
uncontrolled way. `internal/audit` depends on `internal/events` (`tx.go:10`) which is a sensible
direction (audit composes the outbox write).

---

## 3. Config Surface, Entrypoints, Startup & Shutdown

**Config (`config/config.go`) is clean and centralized.** All runtime parameters are
`envconfig`-tagged with dev-friendly defaults, including DB pool tuning, auth TTLs/rate limits,
outbox tuning, and observability. `Load()` returns a single typed struct; `Addr()` composes
host:port. Secrets (`AUTH_PASSWORD_PEPPER`, `PLATFORM_ADMIN_TOKEN`, `SENTRY_DSN`, SMTP creds) have
**no defaults**, which is correct — they must be supplied explicitly.

**Startup ordering is thoughtful (`cmd/api/main.go:43`).** Observability (Sentry, OTel) boots first
and is intentionally non-fatal (`main.go:69-91`) — a telemetry failure logs a warning and continues.
`wireDeps` (`main.go:129`) connects Postgres then Redis then identity/policy, registering reverse-
order cleanups, and **connection failures during boot are fatal** (`main.go:154-156`) — explicitly
"degraded mode is not the right default." Good call.

**Graceful shutdown is correct.** `main.go:106-123` traps SIGINT/SIGTERM, calls `srv.Shutdown` with
`cfg.ShutdownTimeout` (15s default), waits on the listener's error channel, and the deferred
`cleanup()` releases pool/redis/publisher in reverse. The outbox publisher's `Stop` drains within a
5s deadline (`main.go:267-273`). `http.Server` has sane `ReadHeaderTimeout`/`ReadTimeout`/
`WriteTimeout`/`IdleTimeout` set (`server.go:843-846`).

**Production safety guard is a warning, not a fail-stop.** `main.go:181-183`: when
`AUTH_PASSWORD_PEPPER` is empty and `NODE_ENV != development`, the app logs a warning and **starts
anyway** with an empty pepper. An empty pepper materially weakens password hashing (the HMAC key is
empty). For a security-sensitive multi-tenant SaaS, a non-development boot with no pepper should be
a hard failure. (Finding ARCH-02.)

**`migrate` and `seed` are solid CLIs.** `cmd/migrate/main.go` wraps golang-migrate with
`up/down/down-all/goto/force/version`, resolves the migrations dir from `MIGRATIONS_PATH` → repo
default → `./migrations` (`main.go:130`), and treats `ErrNoChange` as success. `cmd/seed/main.go`
is idempotent on the demo slug, hashes the demo/admin passwords with the configured pepper, and the
dev-only default passwords are flagged with `//nolint:gosec // G101` and documented as override-in-
prod (`seed/main.go:41,46`). No secrets are hardcoded outside these documented dev defaults.

**A latent panic-vector worth flagging:** the outbox metrics observer (`main.go:219-241`) and the
publisher (`main.go:247-274`) launch goroutines whose cancellation is registered in `cleanups`
*before* the goroutine starts, which is the correct ordering and avoids the leak-on-early-return
race. Well done.

---

## 4. API Contract vs Reality (OpenAPI)

**This is the single largest documentation/contract gap in the repo.** `docs/openapi.yaml` is a
hand-maintained spec (`redocly.yaml` lints it in CI). It declares **`version: 0.1.0`** and its tag
list (`openapi.yaml:16-56`) stops at **Operations** — i.e. Phase 3. The last path defined is
`/api/v1/stations/{stationID}/operations-overview`; the spec contains **70 paths / 95 operations**.

The implementation in `server.go` registers **~237 routes spanning Phases 1–10**: procurement
(suppliers, purchase-orders, goods-receipts, supplier-invoices), pricing, recognized sales, tender,
customers/AR, the entire Phase 7 finance/accounting stack (accounts, journals, periods, payables,
banking, customer-invoices, expenses, petty-cash, reports/exports), Phase 8 fleet/credit, Phase 9
enterprise command, and Phase 10 risk/fraud/investigations. A grep of the spec for
`procurement|suppliers|purchase-orders|risk/|investigations|finance/|accounts|journal|customers|
fleet|enterprise` returns **zero matches**.

So the OpenAPI spec documents roughly **40% of the surface (the Phase 1–3 routes)** and is silent on
the remaining ~140+ routes. The spec's own preamble admits it is "kept in lockstep with the Go
handlers by review until Stage 11+ wires automatic generation" — but that review has not happened
since Phase 3. The CI job `openapi: lint docs/openapi.yaml` (`ci.yml:51-60`) only validates that the
*existing* YAML is well-formed; it cannot detect missing paths, so the spec rotted silently while
green. The real, current contract is the hand-written SDK (`packages/sdk/src/index.ts` exports
risk/enterprise/finance types), which is kept up to date by hand — meaning the team maintains the
contract **twice** and only one copy is current. (Finding ARCH-01, High.)

---

## 5. Multi-Tenancy & RLS — Posture at Runtime

The schema honors the house convention well: a spot-check confirms RLS `ENABLE ROW LEVEL SECURITY` +
`tenant_isolation` policies are applied not just to the Phase 1–2 tables in `0005_rls.up.sql`
(companies, regions, stations, users, devices, sessions, user_roles, user_station_access, roles) but
to **51 of the 63 migrations'** tenant-scoped tables (every one carries an `ENABLE ROW LEVEL
SECURITY` + `current_setting('app.current_tenant')` policy — verified `0024_stock_movements`,
`0039_journals`, etc.). The composite tenant-FK convention (`uq_<table>_tenant_id`) and the
`current_setting(..., true)` fail-closed pattern are present. The CI `migrations` job
(`ci.yml:386-434`) proves RLS works under the restricted `fuelgrid_app` role: demo tenant sees 2
stations, other tenant 1, no-context 0, nonexistent-tenant 0.

**But RLS is inert at runtime.** Two facts together neuter the entire defense-in-depth layer:

1. **The application never sets the tenant GUC.** `database.WithTenant` (`internal/database/
   tenant.go:29`) is the *only* code that issues `SET LOCAL app.current_tenant`, and a repo-wide
   grep shows it is **never called** anywhere in `services/` or `internal/` — it is dead code. So in
   the running API, `app.current_tenant` is always unset.

2. **The app connects as the table owner / superuser.** `0005_rls.up.sql:13-16` documents that RLS
   is "ENABLED but NOT FORCED" and "Table owners (including the superuser the API currently connects
   as) bypass RLS." `.env.example:30` confirms the default `DATABASE_URL` is
   `postgres://fuelgrid:fuelgrid@...` (the owner), **not** `fuelgrid_app`. There is no later
   migration or config that switches the app onto `fuelgrid_app` or runs `ALTER TABLE ... FORCE ROW
   LEVEL SECURITY`.

Net effect: tenant isolation in production rests **entirely** on the app-layer `WHERE tenant_id = ?`
filters in the repos. RLS provides **zero** runtime protection — it only fires in the CI harness that
deliberately logs in as `fuelgrid_app`. The migration comments are candid that this is a deferred
"future stage," but the architecture/blueprint docs sell RLS as an active control, and an operator
reading "RLS as safety net" in the conventions would reasonably assume it is live. Either the app
must move onto `fuelgrid_app` + `SET LOCAL` per request, or the docs must stop implying the net is
deployed. This is the highest-impact security-posture finding in scope. (Finding ARCH-03, High.)

---

## 6. Build, Run & Deploy Readiness

**Dockerfile is genuinely production-grade** (`services/api/Dockerfile`): multi-stage,
`golang:1.25-alpine` builder with BuildKit cache mounts for module + build cache, `-trimpath`,
`-ldflags="-s -w -X main.version -X main.commit"`, `CGO_ENABLED=0`, distroless `static-debian12:
nonroot` runtime, non-root uid 65532, `EXPOSE 8080`. This is exactly right.

**`docker-compose.yml`** runs Postgres 16 + Redis 7 with healthchecks and named volumes — local
infra only (no API/web service), which matches the "run the binary with `go run`" dev loop in the
Makefile. The `Makefile` (`services/api/Makefile`) is comprehensive: build/run/test/test-race/vet/
lint/tidy, docker-build, db-up/down/psql/redis, and the full migrate/seed surface. Good DX.

**CI is the standout strength** (`.github/workflows/ci.yml`, 654 lines). Five jobs:
`node` (format/lint/typecheck/test/build with `--frozen-lockfile`), `openapi` (redocly lint),
`go` (vet, golangci-lint v2.1, `go test -race -count=1`, build, **and a `go mod tidy` + `git diff
--exit-code` tidiness gate** — excellent), `migrations` (apply→version→down-all→re-apply→seed, boot
the binary, probe `/healthz` + `/readyz` asserting `postgres:ok`/`redis:ok`, scrape `/metrics` for
named collectors, then a deep auth + tenant-isolation + audit/outbox + platform-provisioning
behavioral smoke including the RLS-under-`fuelgrid_app` assertions), and `docker` (buildx build,
SHA-tag, run the image, smoke `/healthz`/`/readyz`/`/metrics`). This is far above the bar for a
project at this stage. Husky + lint-staged enforce prettier/eslint pre-commit.

**Gaps in deploy readiness (acknowledged in `deployment.md`, but still gaps):**

- **No `web` Dockerfile and no `mobile`/`web` deploy at all.** Only `services/api/Dockerfile` exists.
  `apps/web` has no container build despite being a deployable Next.js app. (Finding ARCH-06.)
- **`deployment.md` references artifacts that do not exist.** It claims a "separate small image" for
  migrations invoked as `release_command = "/api-migrate up"` (`deployment.md:100-108`) — there is no
  `Dockerfile.migrate` and no `/api-migrate` binary target; today migrations only run via `go run`.
  It also references `fly.toml` files and `.github/workflows/deploy.yml` that are on the "defer
  list" (`deployment.md:129-136`). The doc is honest about deferral, but a reader skimming the
  topology diagram could mistake the Fly deployment for live infrastructure. (Finding ARCH-07.)
- **No registry push.** The `docker` CI job builds and SHA-tags but `push: false` (`ci.yml:612`),
  so there is no durable image artifact between CI runs. Acceptable pre-launch; flagged for
  completeness.
- **Branch protection is documented as a manual one-time `gh api` call** (`deployment.md:110-127`),
  not codified. Cannot verify it is actually applied to `main`.

---

## 7. Observability & Operational Endpoints

`/healthz` (liveness, no deps), `/readyz` (pings every configured dep, 503 with the failing dep
named, 2s per-dep timeout), and `/metrics` (Prometheus exposition) are all present and correct
(`handlers.go:14-53`, `metrics_handler.go`). The outbox publisher uses `FOR UPDATE SKIP LOCKED`
(`publisher.go:130`) so multiple replicas partition work safely; failed dispatch leaves the row
unpublished for retry (the outbox is durable, the bus best-effort) — a textbook transactional outbox.
A background observer refreshes outbox-lag gauges on a timer (`main.go:218-242`).

**`/metrics` is unauthenticated** (`metrics_handler.go` comment: "intentionally open in dev — gate
it via network policy / ingress allowlist in production"). The reliance on an unwritten ingress rule
is a deploy-time risk: the endpoint leaks per-route latency/volume and outbox internals to anyone
who can reach the pod. Same applies to the `deployment.md:96` note. Documented, not enforced.
(Finding ARCH-08, Low.)

**The event bus has no real consumer.** The only subscriber is a catch-all that logs every event
(`main.go:251-260`). The blueprint/architecture docs describe an "event-driven platform" with
"event-driven alerts and integrations"; today events are durably written to `outbox_events` and then
dispatched to a log line. This matches the migration/code comments ("a Kafka/NATS replacement plugs
in here later") and is reasonable for the stage, but the docs overstate it as a present capability.

---

## 8. Security of Config & Secrets

- **No secrets committed.** `git ls-files` shows no `.env`, `.pem`, `.key`, or credential files
  tracked; `.gitignore` correctly ignores `.env`/`.env.*` (except `.env.example`), `*.pem`, `*.key`,
  and the usual build/IDE noise. `.dockerignore` keeps `.env`, `apps`, `docs`, and node_modules out
  of the build context. `.gitattributes` forces LF normalization (important for the Windows dev box +
  Husky shell hooks).
- **`.env.example` is documentation-grade** — every variable is annotated with the stage that
  introduces it and its security implications (e.g. pepper rotation invalidates hashes). Secrets are
  blank placeholders.
- **Platform-admin token check is hardened.** `requirePlatformAdmin` (`platform_handlers.go:26`)
  uses `crypto/subtle.ConstantTimeCompare`, **404s when the token is unset** (so the route isn't
  advertised), and slug input is validated against a regex (`platform_handlers.go:17`). Good.
- **`golangci.yml` enables `gosec`** (plus errorlint, bodyclose, gocritic, etc.), so secret-leak and
  common-vuln linting runs in CI.

**Request body size is unbounded for JSON handlers.** `decodeJSON` (`handlers.go:63`) uses
`DisallowUnknownFields` (good) but applies **no** `http.MaxBytesReader` cap. The only handler that
caps body size is the calibration CSV upload (`calibration_handlers.go`, the lone
`MaxBytesReader` hit). Every JSON `POST`/`PATCH` will read an arbitrarily large body into memory —
a trivial memory-exhaustion DoS vector. The 30s `Timeout` middleware bounds time, not bytes. A
global body limit (e.g. 1 MB default via middleware) is the standard mitigation. (Finding ARCH-11,
Medium.)

---

## 9. Docs vs Reality

The `docs/` set is voluminous and ambitious. Cross-checking claims against the code:

- **"Offline-first station operations" / PWA** (blueprint §2, architecture §2.1; the audit scope
  itself calls the web app a "PWA"): **not implemented.** No service worker, no web manifest, no
  IndexedDB/workbox/next-pwa in `apps/web/src` (grep returns nothing). The web app is a conventional
  online Next.js SPA. (Finding ARCH-12, Medium — docs/positioning overstate a headline capability.)
- **"AI assistant with permission-aware data access" / "AI intelligence"** (architecture §2.1,
  blueprint): no AI/LLM code in the Go or TS tree. Aspirational.
- **"Event-driven platform with alerts and integrations":** partially true — durable outbox exists,
  but the only consumer is a logger (§7).
- **RLS as an active control:** overstated at runtime (§5).
- **`CONTRIBUTING.md` toolchain table says "Go 1.23+"** (`CONTRIBUTING.md:13`) but `go.mod` requires
  `go 1.25.0` and the Dockerfile uses `golang:1.25`. A contributor on 1.23/1.24 cannot build.
  (Finding ARCH-13, Low.)
- **`go.mod` go directive is `go 1.25.0`** (three-part). This is valid in modern Go (1.21+ accept a
  patch-level toolchain version) and CI's `setup-go` reads `go-version-file: go.mod`, so it resolves
  — but it pins a *minimum patch* unnecessarily tightly; `go 1.25` is the more conventional form.
  Informational.
- The `phase-2-audit-report.md` / `phase-3-audit-report.md` and the roadmap-phase docs are internal
  planning artifacts; they are consistent with the migration/route reality for the phases they
  cover. No misrepresentation found there beyond the OpenAPI staleness already captured.

The architecture and blueprint docs are best read as **target-state vision documents**, and they say
so in places. The risk is that several headline capabilities (offline, AI, live RLS, event-driven
integrations) read as present-tense in the prose while being future work in the code. There is also
**no root `README.md`**, so a newcomer's first stop is the marketing-flavored blueprint rather than a
build/run quickstart. (Finding ARCH-14, Low.)

---

## 10. Dependency Freshness, Versioning & Reproducibility

- **Go deps are current and lean** (`go.mod`): chi v5.3.0, pgx v5.9.2, go-redis v9.19.0, golang-
  migrate v4.19.1, otel v1.44.0, sentry-go v0.46.2, golang.org/x/crypto v0.45.0. No abandoned or
  obviously stale modules. The `go mod tidy` CI gate guarantees `go.sum` integrity and no orphan
  requires.
- **Node deps are modern** (Next 15.1, React 19, TanStack Query 5, Tailwind 4, Zod 3). `@sentry/
  browser ^8.55` while the Go side is on a different Sentry SDK line — expected (different SDKs).
- **Reproducibility is strong:** pinned package manager, frozen lockfile in CI, `.nvmrc`, `go-
  version-file` in CI, BuildKit cache mounts. The main reproducibility gap is the hand-written SDK
  and OpenAPI both being manual (no codegen pin).

---

## Findings

| ID | Severity | File:Line | Issue | Fix |
|----|----------|-----------|-------|-----|
| ARCH-01 | High | `docs/openapi.yaml:4` (and whole file) vs `server.go:168-834` | OpenAPI spec is `version 0.1.0`, stops at Phase 3 (70 paths/95 ops); ~140+ implemented routes across Phases 4–10 (procurement, finance, fleet, enterprise, risk) are undocumented. CI only lints YAML validity, so it rotted while green. Contract is maintained twice (spec + hand SDK); only the SDK is current. | Regenerate the spec from chi route metadata (the deferred "Stage 11" plan) or, short-term, hand-add the missing paths and bump version; add a CI check that diffs declared routes vs registered handlers. |
| ARCH-02 | High | `cmd/api/main.go:181-183` | Non-development boot with empty `AUTH_PASSWORD_PEPPER` logs a warning and starts anyway, materially weakening password hashing in prod. | Fail-stop (return error from `wireDeps`) when `Env != development` and pepper is empty. |
| ARCH-03 | High | `internal/database/tenant.go:29`; `migrations/0005_rls.up.sql:13-16`; `.env.example:30` | RLS is comprehensively defined across 51 migrations but inert at runtime: `WithTenant` (the only `SET LOCAL app.current_tenant` issuer) is never called, and the app connects as the owner role which bypasses RLS. Tenant isolation rests solely on app-layer WHERE clauses; the documented "safety net" is not deployed. | Move the API onto the `fuelgrid_app` role and set `app.current_tenant` per request/transaction; or `FORCE ROW LEVEL SECURITY` + wire `WithTenant` into the request path. Until then, correct the docs/conventions to say RLS is CI-only. |
| ARCH-04 | Medium | `services/api/internal/server/server.go:105-838` | `server.New` is an 865-line god-file declaring ~237 routes for all 10 phases in one function; high merge-conflict risk and unreviewable in one pass. | Extract per-domain `registerXRoutes(r chi.Router)` methods into the existing `*_handlers.go` files; reduce `New` to middleware + sub-router mounting. |
| ARCH-05 | Low | `services/api/internal/server/server.go:149-153` | `Recoverer` is 5th in the chain, after `logRequests`/`recordMetrics`/`RequestID`; panics inside the outer middleware are not recovered (inverted from idiom). | Move `chimiddleware.Recoverer` to immediately after `RequestID` so the full downstream chain is protected. |
| ARCH-06 | Medium | `apps/web/` (no Dockerfile) | No container build for the deployable Next.js web app; only the API has a Dockerfile. Blocks `apps/web` deployment and the documented topology. | Add `apps/web/Dockerfile` (Next.js standalone output) and a web CI image-build job. |
| ARCH-07 | Low | `docs/deployment.md:100-136` | Deployment doc references a separate `/api-migrate` image, `fly.toml` files, and `deploy.yml` that do not exist; could be mistaken for live infra. | Mark these unambiguously as "not yet implemented" or build them; add a status banner. |
| ARCH-08 | Low | `services/api/internal/server/metrics_handler.go` | `/metrics` is unauthenticated and relies on an unwritten production ingress rule; leaks route-level latency/volume + outbox internals. | Gate `/metrics` behind an internal listener/bind, a bearer, or document+enforce the ingress allowlist as part of deploy. |
| ARCH-09 | Low | `packages/types/.gitkeep`, `apps/mobile/.gitkeep` | Two empty workspace members (no `package.json`) are matched by `pnpm-workspace.yaml` globs; `@fuelgrid/types` is referenced by nobody. Latent tooling confusion. | Either scaffold real packages or remove them from the workspace globs until needed. |
| ARCH-10 | Low | `apps/web/package.json` (`"lint": "next lint"`) | Web app uses deprecated `next lint` + `eslint-config-next`, bypassing the shared flat ESLint config; lints with a different ruleset than the rest of the monorepo. | Add `apps/web/eslint.config.mjs` extending `@fuelgrid/config/eslint/next` and switch to `eslint .`. |
| ARCH-11 | Medium | `services/api/internal/server/handlers.go:63-70` | `decodeJSON` applies no `http.MaxBytesReader`; every JSON write endpoint reads unbounded request bodies into memory (DoS vector). Only the CSV upload caps size. | Add a global body-size middleware (e.g. 1 MB default) or wrap `r.Body` in `decodeJSON`. |
| ARCH-12 | Medium | `apps/web/src/**` (no SW/manifest/IndexedDB) | "Offline-first" / PWA is a headline product claim but unimplemented; the web app is an online-only SPA. | Implement service worker + manifest + offline cache, or downgrade the claim in blueprint/architecture/scope docs. |
| ARCH-13 | Low | `CONTRIBUTING.md:13` vs `go.mod:3` | Contributing guide says "Go 1.23+"; `go.mod` requires `go 1.25.0` and Dockerfile uses 1.25 — contributors on 1.23/1.24 can't build. | Update CONTRIBUTING to "Go 1.25+". |
| ARCH-14 | Low | repo root (no `README.md`) | No top-level README; first contact is the marketing blueprint, not a build/run quickstart. | Add a concise root README (stack, prerequisites, `make db-up && migrate up && seed && run`). |

### Severity counts

- **Critical:** 0
- **High:** 3 (ARCH-01, ARCH-02, ARCH-03)
- **Medium:** 4 (ARCH-04, ARCH-06, ARCH-11, ARCH-12)
- **Low:** 6 (ARCH-05, ARCH-07, ARCH-08, ARCH-09, ARCH-10, ARCH-13) + ARCH-14
- **Info:** the `go 1.25.0` three-part directive (valid, but unconventional).

### Top 5 risks

1. **RLS is inert in production (ARCH-03).** The entire defense-in-depth tenant-isolation layer
   exists in the schema and CI but never activates at runtime — the app runs as the owner role and
   never sets `app.current_tenant`. A single missing `WHERE tenant_id = ?` in any of ~30 domains
   becomes a cross-tenant data leak with no backstop.
2. **OpenAPI spec is ~60% stale (ARCH-01).** The published contract documents only Phases 1–3 while
   the product ships Phases 1–10; external/SDK consumers and future codegen are working from a spec
   that is wrong by omission, and CI cannot catch it.
3. **Empty password pepper boots in prod (ARCH-02).** A misconfigured deploy silently weakens every
   password hash instead of refusing to start.
4. **Unbounded request bodies (ARCH-11).** Trivial memory-exhaustion DoS on any JSON write endpoint.
5. **Web app is undeployable as containerized + claimed offline-first is absent (ARCH-06 + ARCH-12).**
   The headline "PWA / offline-first" capability does not exist, and the web tier has no container
   build, so the documented two-service Fly topology can't be realized today.

**Overall:** the Go backend's layering, transactional outbox, config/startup/shutdown, secret
hygiene, and CI are genuinely excellent — well above the norm for this maturity. The serious issues
are concentrated in (a) the gap between the documented security/feature posture and the runtime
reality (RLS, pepper, offline-first), and (b) contract/deploy artifacts that have drifted from or
lag the implementation (OpenAPI, web Dockerfile, deployment.md). None are architecturally
load-bearing to fix; most are "finish what's already designed."
