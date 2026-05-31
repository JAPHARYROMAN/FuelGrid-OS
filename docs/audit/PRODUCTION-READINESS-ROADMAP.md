# FuelGrid OS — Production-Readiness Execution Program

> Generated 2026-05-30 from a 14-agent parallel analysis of each readiness criterion against `main`, synthesized into a dependency- and conflict-aware wave plan. The authoritative execution program for taking every criterion to ≥90%.

Current weighted readiness sits at ~62% across 14 criteria; target is ≥90% on every criterion with full production readiness. The 14 plans contain **84 distinct work items** which dedupe to **~58 PRs** after collapsing cross-criterion duplicates (INV-001/single-opening, RLS-FORCE, money-string types, OpenAPI regen, web test harness all appear under multiple criteria). Roughly 20 PRs need a migration; the rest are app/test/CI/docs. **Blunt timeline note:** this is not a one-sprint effort. The money/decimal campaign (10 sequenced PRs), the OpenAPI regen (8 PRs), the frontend build-out (13 PRs), and the RLS-default + multi-tenancy hardening (6+ PRs) are each multi-week serialized chains because they hammer the same shared files (`inventory/repo.go`, `client.ts`/`types.ts`, `server.go`, `docs/openapi.yaml`, `ci.yml`). Realistic calendar: **8–12 weeks** with 2–3 parallel worktrees, gated by the conflict map below. Attempting to parallelize the conflict-heavy chains will cause continuous rebase churn.

## Scorecard & targets

| Criterion | Now | Achievable | # PRs |
|---|---|---|---|
| Architecture & code structure | 86 | 91 | 5 |
| Database schema & migrations | 88 | 93 | 4 |
| Compliance & audit trail | 82 | 92 | 4 |
| Multi-tenancy isolation | 70 | 92 | 6 |
| Security & access control | 72 | 91 | 7 |
| Financial / data correctness | 64 | 91 | 8 |
| Money/decimal discipline | 55 | 92 | 10 |
| Testing & QA | 55 | 90 | 7 |
| CI/CD | 70 | 92 | 4 |
| Observability | 70 | 92 | 7 |
| Reliability & availability | 55 | 90 | 6 |
| API contract & docs | 35 | 90 | 8 |
| Frontend / web app | 40 | 88 | 13 |
| Deployment & ops / secrets | 60 | 91 | 5 |

(Raw count = 94 plan items; deduped delivery ≈ 58 PRs — see Wave plan.)

## Conflict map

Items touching the SAME file must be **sequenced, not parallelized**. Grouped by shared surface:

- **`internal/inventory/repo.go`** (the hottest backend file) — FIN-1/MD-1 (float→string ledger, SAME work), FIN-6/INV-010/FIN-6 (single-opening index code side), REL-2 (LIMIT/OFFSET on List), QA-4 (property test), QA-5 (concurrency test). **Sequence:** MD-1/FIN-1 first (retypes signatures) → then INV-010 code guard → then REL-2 → then QA-4/QA-5 assert against the post-string API. Never run two of these in parallel.
- **`packages/sdk/src/client.ts` + `packages/sdk/src/types.ts`** — FE-5/MD-8/FIN-8 (money→string, SAME), FE-2/SEC-3 (401 onUnauthorized), FE-10 (zod validation), REL-6 (pagination params), OBS-1 (requestId from X-Request-Id), WI-3/4/5/6/7 (schema lift reference, read-only). **Sequence:** money-string retype (FE-5/MD-8) lands FIRST as the type contract, then 401-hook, then pagination, then zod. OpenAPI work only READS these — fine to parallelize reads.
- **`services/api/internal/server/server.go`** (route table + middleware stack) — ARCH-05 (reorder Recoverer), ARCH-04 (god-function split), OBS-3 (sentry recover mw), OBS-5 (otel mw), REL-4 (rate-limit mw), SEC-3/MT-2 (Deps wiring), WI-1 (Routes() accessor). **Sequence:** ARCH-05 (tiny) → ARCH-04 (big mechanical split) → then OBS-3/OBS-5/REL-4 middleware additions on the split-out stack → WI-1 accessor last. All serialize.
- **`docs/openapi.yaml`** (highest-traffic doc) — WI-2..WI-7 (regen, batched by phase, internally serial), CICD-1 (contract test + regen, SAME as WI-set), REL-6 (pagination schema), FE-5/MD-8 money-encoding consistency. **Sequence:** all OpenAPI path-writing PRs serialize on this single file; do money-string + pagination param shape FIRST so schemas are written once.
- **`.github/workflows/ci.yml`** — MT-3 (RLS smoke env), CICD-1 (contract test step), CICD-3 (coverage gate), CICD-4 (deploy/registry), QA-6 (e2e job), QA-7 (coverage floor), OPS-1 (RLS CI env), OPS-3 (MIGRATE_CONFIRM), OPS-4 (Trivy/SBOM). **Rule:** each appends a job/step; edit in place with surgical diffs, never restructure. Serialize merges to avoid trivial conflicts.
- **`services/api/cmd/api/main.go` + `internal/config/config.go`** — MT-2/SEC-6/OPS-1 (RLS-default wiring, SAME core work), MT-5/OPS-2 (secret validation/redaction), OBS-4 (OTLP fatal), REL-1/REL-4 (page+ratelimit config knobs), SEC-7 (secret contract). **Sequence:** RLS-default (OPS-1/MT-2/SEC-6 collapsed) first, then secret-redaction (OPS-2/MT-5), then config knobs additively.
- **`apps/web/src/app/providers.tsx`** — SEC-3/FE-2 (401 QueryCache onError), OBS-1 (Sentry capture onError), WEB-004. **Sequence:** one PR owns the QueryCache/MutationCache config (FE-2) and the next layers Sentry capture into it (OBS-1).
- **Dashboard `apps/web/src/app/(dashboard)/**/page.tsx`** — FE-7/MD-10 (money formatting), FE-8/SEC-5 (permission gates), FE-9 (mutation errors), OBS-2 (error.tsx). **Sequence:** money-format sweep first, then permission gates, then mutation-error sweep — these touch the same page files; batch by page cluster.
- **`packages/ui/src/components/{pump-card,tank-visual}.tsx`** — FE-6/MD-9/FIN-8 (string props, SAME), QA-3/CICD-2 (render tests). **Sequence:** retype props first, tests after.
- **Migration number `0070+`** — INV-010/FIN-6, AUDIT-IMMUT-1, OUTBOX-RETRY-2, OUTBOX-IMMUT-3, MT-1/SEC-6/OPS-1 (FORCE RLS), MT-4 (orphan tables), ENT-25-DB, FLEET-005-FK, MINOR-FKS, SEC-1 (session epoch). **Rule:** a single owner assigns sequential numbers at merge time; coordinate to avoid two PRs claiming `0070`.
- **`apps/web/package.json` + vitest configs** — QA-1/CICD-2/FE-11 (web test harness, SAME bring-up). Do ONCE in Wave 1.
- **`apps/web/next.config.ts`** — FE-1/SEC-4 (CSP headers, SAME), OBS-7 (Sentry sourcemaps), FE-12 (PWA). Sequence: CSP first.

## Wave plan

Deduped PR IDs below; where multiple criteria specify the same work, the canonical PR notes all criteria it advances.

### Wave 1 — Contained low-risk wins (fully parallel, no shared files)

| PR | Title | Criteria | Files | Migration | Test |
|---|---|---|---|---|---|
| W1-ARCH-05 | Reorder middleware so Recoverer wraps full chain | Architecture | `server.go` (mw block only) | No | server_test.go: panicking handler → 500, outer mw still ran |
| W1-DOCS | Root README + CONTRIBUTING Go 1.25 align | Architecture (ARCH-13/14) | `README.md`, `CONTRIBUTING.md` | No | Docs; spot-check commands |
| W1-WS-CLEAN | Remove dead workspace members | Architecture (ARCH-09) | `pnpm-workspace.yaml`, delete `apps/mobile/.gitkeep`, `packages/types/.gitkeep` | No | `pnpm install --frozen-lockfile` + `pnpm -r build` |
| W1-WEBTEST | Stand up web/UI/SDK Vitest toolchain | Testing(QA-1), CI/CD(CICD-2), Frontend(FE-11 partial) | `apps/web/package.json`, `packages/ui/package.json`, `vitest.config.ts`×N, `apps/web/test/setup.ts`, `packages/config/vitest.base.ts` | No | One smoke test per package so `pnpm -r test` is non-empty |
| W1-AUDIT-IMMUT | audit_logs append-only trigger | Compliance(AUDIT-IMMUT-1) | `migrations/0070_audit_logs_immutable.*`, `phase7_integration_test.go` | **Yes** | mustBlock UPDATE/DELETE; DELETE ok under GUC |
| W1-OUTBOX-RETRY | Outbox retry tracking + dead-letter | Compliance(OUTBOX-RETRY-2) | `migrations/0071_outbox_retry.*`, `internal/events/publisher.go`, `outbox.go` | **Yes** | First publisher_test.go: attempt_count climbs, failed_at after MaxAttempts |
| W1-SEC-TOTP | TOTP one-time-use + tightened skew | Security(SEC-2/AUTH-10) | `internal/identity/totp/totp.go`, `internal/identity/service.go` | No | Replay rejected; concurrent NX one-winner |
| W1-FE-CSP | Security headers + CSP | Frontend(FE-1), Security(SEC-4) | `apps/web/next.config.ts` | No | Header-present smoke; connect-src includes API origin |
| W1-OBS-OTLP | Implement OTLP exporter (fatal on broken) | Observability(OBS-4) | `tracing.go`, `main.go`, `config.go`, `go.mod`, `.env.example`, `docs/deployment.md` | No | bogus endpoint fails as configured; stdout no-op safe |
| W1-OBS-DASH | Dashboards/alerts/SLOs as code | Observability(OBS-6) | `deploy/observability/**`, `docs/runbooks/` | No | promtool check rules; dashboard JSON schema validate |
| W1-FLEET-FK | FK fuel_authorizations.consumed_by→sales | DB(FLEET-005-FK) | `migrations/0072_fuel_auth_consumed_fk.*` | **Yes** | bad sale id rejected; round-trip passes |
| W1-MINOR-FK | Backfill bare-uuid reference FKs | DB(MINOR-FKS) | `migrations/0073_misc_reference_fks.*` | **Yes** | round-trip + one cross-ref rejection |

### Wave 2 — Foundations that later waves depend on

| PR | Title | Criteria | Files | Migration | Test |
|---|---|---|---|---|---|
| W2-INV-OPENING | Single-opening DB unique index + ErrOpeningExists | DB(INV-010), Financial(FIN-6) | `migrations/0070→renumber_single_opening.*`, `internal/inventory/repo.go`, `internal/reconciliation/repo.go`, `phase4_integration_test.go` | **Yes** | two SetOpeningBalance → second ErrOpeningExists; exactly one opening row |
| W2-FORCE-RLS | FORCE RLS all tenant tables + non-owner app pool default + CI enforced path | Multi-tenancy(MT-1/MT-2/MT-3 core), Security(SEC-6), Ops(OPS-1) | `migrations/00NN_force_rls.*`, `main.go`, `config.go`, `server.go`, `auth_middleware.go`, `ci.yml`, `.env.example` | **Yes** | rls_integration_test owner-forced returns only tenant A; CI asserts enforced path + cross-tenant 404 |
| W2-PAGINATE-HELPER | parsePage + writePage envelope + config knobs | Reliability(REL-1) | `server/pagination.go`, `pagination_test.go`, `config.go` | No | clamp/default/garbage/has_more table test |
| W2-SEC-EPOCH | Authoritative session revocation via per-user epoch | Security(SEC-1/AUTH-04) | `session.go`, `redis_store.go`, `identity/service.go`, `repo/user.go`, `repo/session.go`, `migrations/00NN_user_session_epoch.*`, `auth_middleware.go` | **Yes** | global revoke → Resolve ErrSessionRevoked even with Redis stub no-op |
| W2-AUDIT-EXPORT | Audit export generation | Compliance(AUDIT-EXPORT-4) | `export_handlers.go` | No | audit_logs has action='export.generated' |
| W2-OBS-BE-SENTRY | Backend Sentry panic + 5xx capture | Observability(OBS-3) | `middleware.go`, `server.go`, `internal/observability/sentry.go` | No | panic→500 with fake sentry event tagged request_id; 4xx not captured |
| W2-WI-ROUTES | chi.Walk route dump + route-vs-spec contract test (skeleton, red) | API contract(WI-1/CICD-1) | `server.go` (Routes accessor), `routes_contract_test.go`, `cmd/routes/main.go` | No | walk yields 237 routes; diff oracle |

### Wave 3 — Money/decimal backend core + RLS follow-ons (serialized on inventory/repo.go & main.go)

| PR | Title | Criteria | Files | Migration | Test |
|---|---|---|---|---|---|
| W3-INV-STRING | Inventory ledger litres/balance → decimal strings | Financial(FIN-1), Money(MD-1) | `inventory/repo.go`, `deliveries.go`, `sales.go`, `periods.go`, `central_commercial.go`, `inventory_handlers.go`, `transfer_handlers.go` | No | phase4: post 0.001×30 → '0.030' exact |
| W3-INV-DTO | stockMovementDTO + handlers emit strings | Money(MD-2) | `inventory_handlers.go` | No | JSON litres serialize as quoted decimals |
| W3-SECRETS | Config validate + Secret redaction + secrets seam | Multi-tenancy(MT-5), Ops(OPS-2), Security(SEC-7) | `config.go`, `main.go`, `docs/deployment.md`, `docs/security/secrets.md`, `.env.example` | No | non-dev empty secret fails; Secret.LogValue redacted |
| W3-RLS-ORPHAN | tank_calibration_entries policy + drift detector | Multi-tenancy(MT-4) | `migrations/00NN_rls_orphan_tables.*`, `0012_tank_calibration.up.sql` | **Yes** | drift test: every tenant_id table has policy+FORCE |
| W3-OUTBOX-IMMUT | outbox_events column-scoped mutation trigger | Compliance(OUTBOX-IMMUT-3) | `migrations/00NN_outbox_events_guard.*` | **Yes** | payload UPDATE rejected; published_at allowed |
| W3-ENT25-DB | DB transfer product-alignment FK | DB(ENT-25-DB) | `migrations/00NN_stock_transfer_product_alignment.*` | **Yes** | cross-product transfer rejected at insert |
| W3-MIGRATE-GUARD | Guard down-all/force + migration runbook | Ops(OPS-3) | `cmd/migrate/main.go`, `docs/runbook-migrations.md`, `ci.yml` | No | down-all errors without MIGRATE_CONFIRM=1 |

### Wave 4 — Money/decimal master-data + readings + procurement (depends on W3-INV-STRING)

| PR | Title | Criteria | Files | Migration | Test |
|---|---|---|---|---|---|
| W4-MD-MASTER | products/tanks/nozzles price/litre/density → strings | Money(MD-3), Financial(FIN-2 partial) | `products/repo.go`, `tanks/repo.go`, `nozzles/repo.go` + 4 handlers | No | create product price "2.345" round-trips; reject non-decimal 400 |
| W4-MD-READINGS | meter+dip readings → strings, SQL precision math | Money(MD-4) | `readings/repo.go`, `meter.go`, `meter_test.go`, `dips.go`, `interpolate.go`, 2 handlers | No | decimal-string meter cases incl. precision rejection |
| W4-MD-PROC | PO ordered/received litres → strings | Money(MD-6), Financial(PROC-06) | `procurement/purchase_orders.go`, `overview.go`, 2 handlers | No | partial receipts accumulate exact; status at ordered==received |
| W4-FIN-INVDISC | PROC-19 per-invoice receipt scoping | Financial(FIN-5) | `procurement/invoices.go` | No | two partial invoices each flag against own receipt |
| W4-FIN-COSTING | REV-02 perpetual avg-cost (or rename+doc) | Financial(FIN-4) | `inventory/costing.go`, `revenue/repo.go`, `docs/costing-policy.md` | No | COGS reflects perpetual moving average |
| W4-FIN-TAX | REV-03 non-zero tax split integration test | Financial(FIN-3) | `phase6_integration_test.go`, `phase2_integration_test.go` | No | 18% gross→net+tax exact, net+tax==gross |
| W4-QA-PROP | Decimal/litre accumulation property+fuzz test | Testing(QA-4), Money(MD-7), Financial(FIN-7) | `internal/reconciliation/decimal_property_test.go`, `internal/inventory/repo_property_test.go` | No | quick.Check sum == big.Rat reference, zero drift |
| W4-QA-CONC | Inventory + transfer advisory-lock race test | Testing(QA-5) | `inventory_concurrency_test.go` | No | N concurrent posts: balance==sum, gapless seq, under -race |

### Wave 5 — Shift-close + reliability (depends on W4 master-data/readings strings)

| PR | Title | Criteria | Files | Migration | Test |
|---|---|---|---|---|---|
| W5-MD-SHIFT | Shift-close lines + cash submission decimal, expected_value in SQL | Money(MD-5), Financial(FIN-2 core/OPS-001) | `operations/close.go`, `shift_close_handlers.go`, `cash_submissions_handlers.go`, `revenue/repo.go` | No | fractional shift expected_value/cash exact; variance on exact boundary |
| W5-REL-REPO | LIMIT/OFFSET into all repo List() (3–4 sub-PRs by domain) | Reliability(REL-2) | ~27 `internal/*/repo.go` | No | per-domain page1/page2 disjoint+ordered |
| W5-REL-RATELIMIT | Per-tenant quota + global throttle | Reliability(REL-4) | `server/ratelimit_middleware.go`, `server.go`, `config.go`, `identity/ratelimit/redis.go` | No | N+1 → 429 Retry-After; tenants isolated; inflight cap 503 |

### Wave 6 — Handler pagination + SDK money/401 (depends on W5-REL-REPO, W4 string types)

| PR | Title | Criteria | Files | Migration | Test |
|---|---|---|---|---|---|
| W6-REL-HANDLERS | parsePage into all handleList* + envelope (3 sub-PRs) | Reliability(REL-3) | ~42 `server/*_handlers.go` | No | ?limit=2→has_more; over-max clamp; limit=abc→400 |
| W6-REL-DEGRADE | Readiness-aware shedding + Redis-down fail-open | Reliability(REL-5) | `handlers.go`, `server.go`, `main.go` | No | Redis down → limiter fail-open; degraded readyz body |
| W6-SDK-MONEY | SDK types + request params money/litre → string | Money(MD-8), Financial(FIN-8), Frontend(FE-5) | `packages/sdk/src/types.ts`, `client.ts`, `client.test.ts` | No | decimal-string fields parse without Number coercion; tsc passes |
| W6-OBS-CORR | Trace_id↔log correlation + otelhttp spans | Observability(OBS-5) | `middleware.go`, `tracing.go`, `server.go` | No | span per request with http.route; log correlation_id==trace_id |

### Wave 7 — SDK transport hardening + UI components (depends on W6-SDK-MONEY)

| PR | Title | Criteria | Files | Migration | Test |
|---|---|---|---|---|---|
| W7-SDK-401 | 401 onUnauthorized + QueryCache forced logout | Security(SEC-3/WEB-003), Frontend(FE-2) | `client.ts`, `providers.tsx`, `lib/api.ts`, `auth-store.ts` | No | 401 triggers clearSession+redirect |
| W7-FE-MW | Server route guard middleware.ts + presence cookie | Frontend(FE-4/WEB-002) | `apps/web/src/middleware.ts`, `protected-route.tsx`, `auth-store.ts` | No | no cookie → 307 /login?next= |
| W7-UI-COMP | TankVisual/PumpCard accept decimal strings | Money(MD-9), Financial(FIN-8), Frontend(FE-6) | `packages/ui/src/components/{tank-visual,pump-card}.tsx` | No | decimal-string price renders, no NaN |
| W7-SDK-ZOD | Runtime zod validation in SDK responses | Frontend(FE-10/SDK-01) | `client.ts`, `schemas.ts`, `package.json` | No | malformed payload rejected with SdkError |
| W7-OBS-FE | Frontend Sentry query/mutation capture | Observability(OBS-1/WEB-004) | `providers.tsx`, `lib/sentry.ts`, `lib/observability.ts`, `client.ts` | No | onError captures 500 not 401; setUser on login; SdkError.requestId |

### Wave 8 — Frontend pages + boundaries + error boundaries (depends on W6/W7)

| PR | Title | Criteria | Files | Migration | Test |
|---|---|---|---|---|---|
| W8-OBS-BOUNDARY | global-error.tsx + segment error.tsx | Observability(OBS-2), Frontend(FE-3) | `app/global-error.tsx`, `(dashboard)/error.tsx`, `(auth)/error.tsx`, `lib/observability.ts` | No | thrown child → boundary + captureException + reset |
| W8-FE-MONEY | Page money formatting + decimal-safe sum (2 sub-PRs) | Money(MD-10), Financial(FIN-8), Frontend(FE-7) | `lib/format.ts`/`money.ts` + ~11 dashboard pages | No | format.ts fractional sum no drift; products payload strings |
| W8-FE-PERM | Permission-gated controls (2 sub-PRs) | Security(SEC-5/PAGE-013), Frontend(FE-8) | `use-permissions.ts`, `permission-gate.tsx` + high-priv pages | No | button disabled+tooltip when permission absent |
| W8-FE-MUTERR | Surface mutation errors everywhere | Frontend(FE-9/PAGE-008) | users/risk/approvals/profile pages, `toast.tsx` | No | failing grantRole → error toast; row-scoped pending |
| W8-FE-PWA | Resolve PWA claim (manifest or correct docs) | Frontend(FE-12/WEB-005) | `public/manifest.webmanifest`, `next.config.ts`, `layout.tsx` | No | manifest served + linked |

### Wave 9 — Test coverage, OpenAPI regen, CI gates, CD, e2e (depends on stable contracts)

| PR | Title | Criteria | Files | Migration | Test |
|---|---|---|---|---|---|
| W9-QA-WEBUNIT | safeRedirect/auth-store/format unit tests | Testing(QA-2) | extract `lib/safe-redirect.ts` + `__tests__/*` | No | open-redirect rejection; store reducers |
| W9-QA-RTL | RTL: ProtectedRoute, login-form, providers, pump-card | Testing(QA-3) | `__tests__/*.tsx` | No | redirect/401-clear/decimal-render |
| W9-OPENAPI | Components + Phase 4–10 path regen (WI-2..WI-7, serial on openapi.yaml) | API contract | `docs/openapi.yaml` (6 sub-PRs) | No | contract test passes per phase; redocly green |
| W9-CONTRACT-CI | Wire contract diff into CI + version bump + optional gen | API contract(WI-8/CICD-1) | `ci.yml`, `openapi.yaml`, `packages/sdk/package.json` | No | undocumented route fails CI |
| W9-REL-SDKPAGE | Pagination to SDK + web list pages + OpenAPI | Reliability(REL-6) | `client.ts`, `types.ts`, list pages, `openapi.yaml` | No | list appends ?limit&offset, parses has_more |
| W9-COV-GATE | Coverage thresholds Go + TS | Testing(QA-7), CI/CD(CICD-3) | `ci.yml`, vitest configs, `scripts/check-go-coverage.sh` | No | build fails on coverage drop |
| W9-E2E | Playwright smoke (login→dashboard→401) | Testing(QA-6) | `playwright.config.ts`, `e2e/*.spec.ts`, `ci.yml` | No | login lands dashboard; revoked token logs out |
| W9-CD | CD pipeline + migrate-on-deploy + GHCR push | CI/CD(CICD-4), Ops(OPS-5) | `deploy.yml`, `Dockerfile`, `fly.toml`×2, `docs/ops/deploy.md`, `docs/runbook-backups.md` | No | dry-run: idempotent migrate, post-deploy /readyz gate |
| W9-SCAN | Trivy + SBOM + read-only FS docs | Ops(OPS-4) | `ci.yml`, `Dockerfile`, `docs/deployment.md` | No | Trivy fails on HIGH/CRITICAL; SBOM artifact |
| W9-OBS-BIZ | Business/queue metrics + Sentry sourcemaps | Observability(OBS-7) | `metrics.go`, `main.go`, `next.config.ts`, `package.json` | No | new counters increment; sourcemap upload gated on token |
| W9-MT-BLAST | Cross-tenant blast property test | Multi-tenancy(MT-6) | `rls_integration_test.go`, `phase2_integration_test.go` | No | tenant-A conn sees 0 of B; WITH CHECK rejects B insert |

### Wave 10 — Large mechanical refactor + long-term (lands last; high-churn)

| PR | Title | Criteria | Files | Migration | Test |
|---|---|---|---|---|---|
| W10-ARCH-SPLIT | Break server.New into per-domain registerXRoutes (2 PRs) | Architecture(ARCH-04) | `server.go` + all `*_handlers.go` | No | chi.Walk route-set snapshot byte-equivalent |
| W10-FE-COOKIE | httpOnly-cookie session migration (long-term) | Frontend(FE-13/WEB-001) | `auth-store.ts`, `lib/api.ts`, `client.ts`, `auth_handlers.go` | No | login sets httpOnly cookie; CORS reconcile |

## Big campaigns

**Money/decimal discipline (10 PRs, mostly serial on `inventory/repo.go` → SDK → UI → pages):** W3-INV-STRING → W3-INV-DTO → W4-MD-MASTER → W4-MD-READINGS → W4-MD-PROC → W5-MD-SHIFT → W6-SDK-MONEY → W7-UI-COMP → W8-FE-MONEY → W4-QA-PROP (property test, can land once W3 done). The DB columns are already `numeric(14,3)/(14,2)` so **no migration** — pure app-layer. The wire contract flips number→string in lockstep: backend handlers (W3–W5) must merge before SDK retype (W6), which must merge before UI (W7) and pages (W8) or tsc breaks. This single chain unblocks Financial-correctness and Money-discipline simultaneously.

**RLS-default / multi-tenancy (6 PRs):** W2-FORCE-RLS (collapses MT-1+MT-2+MT-3+SEC-6+OPS-1 — FORCE migration + non-owner pool default + CI enforced path) → W3-RLS-ORPHAN (MT-4 tank_calibration_entries + drift detector) → W3-SECRETS (MT-5/OPS-2 rotate fuelgrid_app password) → W9-MT-BLAST (MT-6 behavioural fuzz). The FORCE migration and pool-default flip MUST land together or CI/seed (owner-connected) breaks. Pre-auth identity reads stay on owner pool.

**OpenAPI regeneration (8 PRs, fully serial on `docs/openapi.yaml`):** W2-WI-ROUTES (red contract test) → W9-OPENAPI sub-PRs WI-2 (components/error/security) → WI-3 (Phase 4–5) → WI-4 (Phase 6 money) → WI-5 (Phase 8 fleet/credit) → WI-6 (Phase 9–10 enterprise/risk, largest) → WI-7 (Phase 7 accounting/banking) → W9-CONTRACT-CI (wire CI gate + version bump). Money fields must be documented as string-decimals — sequence AFTER W6-SDK-MONEY so the encoding is settled. Spec coverage reaches 237/237.

**Frontend build-out (13 PRs):** transport/auth first (W1-FE-CSP, W7-SDK-401, W7-FE-MW, W7-SDK-ZOD), then observability (W7-OBS-FE, W8-OBS-BOUNDARY), then money/perms/errors page sweeps (W8-FE-MONEY, W8-FE-PERM, W8-FE-MUTERR), then PWA (W8-FE-PWA) and tests (W9-QA-WEBUNIT, W9-QA-RTL), with httpOnly-cookie (W10-FE-COOKIE) as the deferred long-term. Web test harness (W1-WEBTEST) is the Wave-1 prerequisite for all `*.test.tsx`. Achievable ceiling is 88% (cookie migration spans beyond this milestone).

**server.go god-function split (2 PRs, last):** W10-ARCH-SPLIT runs AFTER all middleware additions (OBS-3/OBS-5/REL-4) and the WI-1 accessor land, so the mechanical extraction doesn't fight in-flight stack edits. Locked by a chi.Walk route-set snapshot.

## Definition of done per criterion

- **Architecture & code structure** — `server.New` < ~120 lines via per-domain `registerXRoutes`, Recoverer wraps the full middleware chain, dead workspace members gone, root README + Go-1.25-correct CONTRIBUTING; chi.Walk route snapshot locks the table.
- **Database schema & migrations** — single-opening partial unique index live, transfer/consumed-by/misc composite FKs added, all up files have down files and CI up→down-all→up round-trips green.
- **Compliance & audit trail** — `audit_logs` and `outbox_events` are DB-immutable (triggers), outbox has attempt-count + dead-letter quarantine, export generation is audited.
- **Multi-tenancy isolation** — FORCE RLS on every tenant table, non-owner app pool is the production default (fail-stop otherwise), no orphan tenant table, drift detector + cross-tenant blast test green in CI's enforced path.
- **Security & access control** — session revocation authoritative via per-user epoch on the hot path, TOTP one-time-use, CSP/security headers shipped, 401 forces logout, RLS enforced by default.
- **Financial / data correctness** — no float64 on any money/litre path (ledger, shift-close, receipt, master-data), avg-cost policy correct+documented, tax split tested at 18%, invoice tolerance scoped per receipt, single-opening invariant enforced.
- **Money/decimal discipline** — every money/litre/rate field is a decimal string through Go→SDK→UI→pages, all arithmetic in SQL `::numeric`, property/fuzz test proves zero accumulation drift.
- **Testing & QA** — web/UI/SDK Vitest+RTL suites real and gated, decimal property test + advisory-lock concurrency test exist, Playwright smoke e2e runs, coverage floors enforced in CI.
- **CI/CD** — route-vs-OpenAPI contract test gates the `go` job, web/UI test jobs non-empty, coverage gates on Go+TS, CD pipeline with migrate-on-deploy + GHCR push + post-deploy `/readyz` gate.
- **Observability** — both Sentry SDKs fed (backend panic/5xx + frontend query/mutation/render), OTLP implemented (fatal if broken), trace↔log correlation, dashboards/alerts/SLOs committed as code.
- **Reliability & availability** — every list endpoint paginated (repo LIMIT/OFFSET + parsePage + has_more envelope), per-tenant quota + global throttle, readiness-aware degradation with Redis-down fail-open.
- **API contract & docs** — all 237 routes documented with schemas + uniform error/pagination/security envelope, version bumped off 0.1.0, CI contract diff fails on any undocumented route.
- **Frontend / web app** — hardened transport (401 logout, zod), CSP + server route guard, decimal-string money end-to-end, permission-gated controls, universal mutation-error surfacing, baseline test suite, honest PWA story.
- **Deployment & ops / secrets** — FORCE-RLS + non-owner pool by default, config Validate() hard-fails on missing prod secrets with redaction wrapper, destructive migrate subcommands guarded + runbook, image scan/SBOM + committed fly.toml with probes + backup/PITR restore-drill runbook.