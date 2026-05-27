# Phase 1 — Platform Foundation Roadmap

The base operating layer on which every other phase is built. When this phase is done, FuelGrid OS has a working web app shell + Go API + Postgres database, with multi-tenant auth, RBAC, and audit logs in place. No fuel logic yet — that begins in Phase 2.

## Stack decisions locked for this phase

These are picked to start fast. They can be revisited later, but treat them as fixed until Phase 1 ships.

| Concern | Choice | Reason |
|---|---|---|
| JS package manager | **pnpm** with workspaces | Best monorepo DX, fast, strict |
| TS build orchestration | pnpm scripts (add Turborepo later) | Avoid premature complexity |
| Lint / format | ESLint + Prettier | Standard |
| Go module path | `github.com/japharyroman/fuelgrid-os` | Matches GitHub owner |
| Go HTTP router | **chi** | Lightweight, idiomatic, middleware-friendly |
| Go logging | **slog** (stdlib) | No extra dep, structured by default |
| Go config | **envconfig** | Simple, struct-tag driven |
| DB driver | **pgx/v5** | De facto standard for Postgres in Go |
| Migrations | **golang-migrate** | Mature, simple file-based |
| Local infra | **docker compose** | Postgres + Redis dev stack |
| Auth tokens | **Opaque session tokens in Redis** | Revocable; JWT is wrong default for sessions |
| Password hashing | **argon2id** | Modern standard |
| MFA | **TOTP** (RFC 6238) | Universal authenticator app support |
| Web framework | **Next.js (App Router)** | Per architecture doc |
| Web data layer | **TanStack Query** + generated SDK | Per architecture doc |
| UI primitives | **shadcn/ui** + Radix + Tailwind | Per UI/UX doc |
| CI | **GitHub Actions** | Where the repo lives |

---

## Stage 1 — Repo & Tooling

**Goal:** Anyone can clone, `pnpm install`, and have lint/format/typecheck/CI all working before a single feature is written.

- [x] Initialize pnpm workspace (`pnpm-workspace.yaml` covering `apps/*`, `services/*`, `packages/*`)
- [x] Root `package.json` with shared scripts (`lint`, `typecheck`, `test`, `build`, `dev`)
- [x] `packages/config` — shared `tsconfig.base.json`, `eslint-config`, `prettier-config`, `tailwind-preset`
- [x] Husky + lint-staged for pre-commit lint/format
- [x] `.editorconfig` and `.nvmrc` (Node 22 LTS)
- [x] `.env.example` at repo root documenting all env vars used anywhere
- [x] CONTRIBUTING.md with branch / commit conventions (Conventional Commits)
- [x] GitHub Actions workflow: lint + typecheck on every PR

**Done when:** A fresh clone passes `pnpm install && pnpm lint && pnpm typecheck` with zero code. ✅ Verified — `format:check`, `lint`, `typecheck`, `test`, `build` all green.

---

## Stage 2 — Backend Service Skeleton

**Goal:** `services/api` runs, serves `/healthz`, logs structured JSON, shuts down gracefully.

- [x] `go mod init github.com/japharyroman/fuelgrid-os`
- [x] Layout: `services/api/cmd/api/main.go`, `services/api/internal/server`, shared `internal/` modules per architecture doc
- [x] HTTP server using chi with middleware stack: request ID, recoverer, logger, CORS, timeout
- [x] Config struct loaded via envconfig with sensible defaults
- [x] `slog` JSON handler wired to log writer
- [x] `/healthz` (liveness) and `/readyz` (deps reachable) endpoints
- [x] Graceful shutdown on SIGTERM/SIGINT with context cancellation
- [x] `Dockerfile` (multi-stage, distroless final image)
- [x] `services/api/Makefile` with `run`, `build`, `test`, `lint` targets
- [x] golangci-lint config + CI step

**Done when:** `docker build` + `docker run` boots the API, `/healthz` returns 200, logs are structured. ✅ Verified — `go run ./cmd/api` boots locally; `/healthz` and `/readyz` return 200 with JSON; `X-Request-Id` echoed in responses; CI `docker` job runs the image and smoke-tests both endpoints.

---

## Stage 3 — Database Foundation

**Goal:** Postgres + Redis run locally via compose; migrations apply cleanly; baseline tenant/org schema exists.

- [x] `docker-compose.yml` at repo root: Postgres 16, Redis 7, with named volumes
- [x] Add migration runner (`golang-migrate`) — `make migrate-up`, `make migrate-down`, `make migrate-new NAME=...`
- [x] Decide on schema conventions and document in `docs/db-conventions.md`:
  - UUIDv7 PKs (`uuid` column type, `gen_random_uuid()` default for now)
  - `tenant_id uuid NOT NULL` on every tenant-owned table
  - `created_at`, `updated_at` everywhere (created_by / updated_by added in Stage 4)
  - Soft-delete via `status='deleted'` (not `deleted_at`) — see db-conventions.md for rationale
  - Snake_case tables/columns
- [x] Migration `0001_init.sql`: `tenants`, `companies`, `regions`, `stations` with FK chain and indexes
- [x] Migration `0002_users.sql`: `users` table (no auth fields yet — those come next stage)
- [x] Connection pool (pgx) wired into API server with health probe in `/readyz`
- [x] Seed script for one demo tenant + company + region + station

**Done when:** `make migrate-up` from a clean DB produces the schema; API can query `tenants` table. ✅ Verified in CI (`migrations` job spins up Postgres 16 + Redis 7, applies migrations, exercises down-all + re-up, seeds demo data, boots the API and asserts `/readyz` reports postgres + redis ok). Local Docker-based verification deferred — daemon was down at commit time.

---

## Stage 4 — Identity & Auth

**Goal:** Real users can sign in, sessions are revocable, password reset works, MFA is wired (even if optional).

- [x] Migration: add auth fields to `users` (password_hash, mfa_secret, mfa_enabled, last_login_at, failed_login_count, locked_until, password_changed_at)
- [x] Migration: `sessions` (token_hash, user_id, tenant_id, device_id, ip, user_agent, issued_at, last_seen_at, expires_at, revoked_at, revoke_reason)
- [x] Migration: `devices` (id, user_id, tenant_id, label, fingerprint, last_seen_at)
- [x] Password hashing: argon2id with sane params; pepper from env (HMAC-SHA256 pre-mix)
- [x] Endpoints:
  - `POST /api/v1/auth/login` (email + password → session token)
  - `POST /api/v1/auth/logout` (revoke current session)
  - `POST /api/v1/auth/refresh` (extend session)
  - `POST /api/v1/auth/password-reset/request`
  - `POST /api/v1/auth/password-reset/confirm`
  - `POST /api/v1/auth/mfa/enroll` + `POST /api/v1/auth/mfa/verify`
  - `GET  /api/v1/me` (protected smoke endpoint)
- [x] Session store: Redis with TTL = session expiry; opaque token = random 32-byte b64url (sha256 in Postgres for audit)
- [x] Auth middleware: extract token → load session → inject `actor` into request context
- [x] Rate limiting on `/auth/login` (Redis fixed window — default 5 attempts / 15 min / IP)
- [x] Audit hook: log `UserLoggedIn`, `UserLoggedOut`, `UserLoginFailed`, `UserMfaFailed`, `UserMfaEnrolled`, `UserMfaActivated`, `UserPasswordChanged`, `UserPasswordResetRequested`, `UserPasswordReset` via slog. Stage 7 swaps these for outbox writes.

**Done when:** A seeded user can log in via curl, hit a protected endpoint, log out, and the session is gone from Redis. ✅ Verified in CI (`migrations` job: login → token → /me → 204 logout → post-logout /me returns 401). Local Docker-based verification skipped (daemon was down).

---

## Stage 5 — RBAC & Permissions

**Goal:** Every protected action checks `actor + action + resource → allow/deny` via a single policy evaluator.

- [x] Migration: `roles`, `permissions`, `user_roles`, `role_permissions`
- [x] Seed default roles per PRD §5: Attendant, Supervisor, Station Manager, Regional Manager, Finance Officer, Procurement Officer, Auditor, Executive, System Administrator
- [x] Seed default permissions (18 codes spanning station, shift, inventory, pricing, finance, reports, audit, admin)
- [x] Policy evaluator package: `internal/identity/policy` with `Service.Can(ctx, actor, perm, resource) error` (and pure `PermissionSet.Can`)
- [x] HTTP middleware factory: `requirePermission("station.read", stationFromURLParam("stationID"))`
- [x] Migration: `user_station_access` (user_id, station_id, tenant_id, granted_by, granted_at) — for station-scoped permissions
- [x] Policy considers tenant → station scoping (region/company scoping deferred until those tables grow real semantics)
- [x] Endpoint: `GET /api/v1/me/permissions` returns `{permissions:[{code, station_scoped}], station_ids:[...], tenant_wide}`
- [x] Unit tests covering role × action × scope: attendant denied, station_manager allowed at assigned station and denied at others, tenant-wide actor allowed everywhere, tenant-wide permissions don't require a station

**Done when:** Calling a protected endpoint as an attendant returns 403; as a station manager for an assigned station returns 200; as a station manager for an unassigned station returns 403. ✅ Verified in CI: the seeded `station_manager` user gets 200 for `GET /api/v1/stations/{MIK-01_id}` and 403 for `GET /api/v1/stations/{MSA-01_id}` — same role, different scope.

---

## Stage 6 — Multi-Tenancy Enforcement

**Goal:** No query can return another tenant's data. Tested and verified.

- [x] Tenant resolver middleware: `requireAuth` injects `identity.Actor{TenantID, …}` onto the request context; `identity.Require(ctx)` is the single read path
- [x] Repository layer convention: every existing query takes `tenantID` as first scoping parameter; documented as the contract in [docs/multi-tenancy.md](docs/multi-tenancy.md)
- [x] Postgres row-level security policies on every tenant-owned table (companies, regions, stations, users, devices, sessions, user_roles, user_station_access, roles)
- [x] `database.WithTenant(ctx, pool, tenantID, fn)` helper wraps queries in a transaction with `SET LOCAL app.current_tenant`; RLS policies reference `current_setting('app.current_tenant', true)` and fail closed when unset
- [x] Tenant-isolation integration tests in CI: create a second tenant via psql, attempt cross-tenant GET via the API → 404; query as `fuelgrid_app` with no tenant context → 0 rows; with each tenant's context → only their rows
- [x] Document tenant safety rules in `docs/multi-tenancy.md`

**Done when:** Integration tests prove cross-tenant data access is impossible at API and DB layers. ✅ Verified in CI — the new "Tenant isolation" step asserts: (a) cross-tenant `GET /api/v1/stations/{id}` returns 404 (app-layer scoping), (b) `fuelgrid_app` SELECT against `stations` with each tenant's `app.current_tenant` returns only that tenant's rows, (c) no tenant context returns zero rows (fail-closed RLS).

**Posture note:** RLS is ENABLED but not FORCED. The API still connects as the table owner today, so its operative defense is application-layer `WHERE tenant_id = ?` scoping. The `fuelgrid_app` role exists and is subject to RLS — a future stage will migrate the API onto it, at which point FORCE can be added.

---

## Stage 7 — Audit & Event Foundation

**Goal:** Every sensitive action emits a domain event and writes an immutable audit log. Outbox pattern wired end-to-end.

- [x] Migration: `audit_logs` (id, tenant_id, actor_id, action, entity_type, entity_id, previous_value jsonb, new_value jsonb, reason, ip, user_agent, request_id, occurred_at)
- [x] Migration: `outbox_events` (id, tenant_id, event_type, event_version, aggregate_type, aggregate_id, actor_id, payload jsonb, metadata jsonb, occurred_at, published_at, correlation_id, causation_id)
- [x] Domain event envelope type matches architecture §13.2 fields exactly (`internal/events/event.go`)
- [x] Repository convention: every sensitive write now lands in the same DB transaction as its `audit.Write` + `events.WriteOutbox` — demonstrated by the grant-role endpoint
- [x] Background outbox publisher goroutine: polls unpublished events with `FOR UPDATE SKIP LOCKED`, dispatches to the in-process bus, marks `published_at`. Kafka/NATS deferred.
- [x] Audit writer (`internal/audit`) — handlers wrap the action and its audit row in one tx so a crash between business change and audit row is impossible
- [x] Sensitive actions wired: `user.role.granted` lands today; the audit + outbox pattern is the template every later sensitive write follows (price change, period lock, record deletion, etc.)
- [x] `GET /api/v1/audit-logs` with filters (action, entity_type, entity_id, actor_id, since, until, limit) — requires `audit.read`, scoped to actor's tenant

**Done when:** A user permission change writes both an `audit_logs` row and an `outbox_events` row in the same transaction, and the publisher dispatches the event. ✅ Verified in CI: the new "Audit + outbox" step logs in as the seeded `admin@fuelgrid.local`, POSTs `{role_code:"attendant"}` to `/api/v1/admin/users/{demo_user_id}/roles`, snapshots row counts before/after, then waits for the publisher to set `published_at`. All three assertions hold: +1 `audit_logs`, +1 `outbox_events`, and the new outbox row reaches `published_at IS NOT NULL` within the polling window. The same admin token can then read the entry back via `GET /audit-logs?action=user.role.granted`.

---

## Stage 8 — Frontend App Shell

**Goal:** A logged-in user lands on an empty Command Center with the full app chrome — sidebar, topbar, right panel, theme — even though most modules are stubs.

- [ ] Scaffold `apps/web` with Next.js 15 (App Router), TS, Tailwind, shadcn/ui
- [ ] Design tokens in `packages/ui` per UI/UX doc §5–7: colors, typography (incl. tabular numerals for money/liters), spacing, radius, shadows
- [ ] Theme: dark mode default for executive views, light mode toggle
- [ ] App layout component: left sidebar (collapsible, role-aware), top command bar, main workspace, right insight panel
- [ ] Auth pages: `/login`, `/forgot-password`, `/reset-password`, `/mfa`
- [ ] Protected route wrapper that redirects to `/login` and preserves `?next=`
- [ ] `packages/sdk` — typed API client (generated from OpenAPI spec emitted by the Go server, OR hand-written initially)
- [ ] TanStack Query provider, default options (staleTime, retry policy)
- [ ] Permission hook: `usePermission("station.read")` driven by `/me/permissions` response
- [ ] Zustand stores: `useAuth`, `useTenantContext` (active company/region/station)
- [ ] Empty/loading/error state components in `packages/ui` (referenced as a quality gate per UI/UX §18.4)
- [ ] Command palette skeleton (cmdk) bound to ⌘K/Ctrl+K — wired to global search later

**Done when:** Logging in lands on `/command-center` showing an empty premium-looking shell; logging out returns to `/login`; refreshing the page preserves the session.

---

## Stage 9 — Admin Console (Users & Org Hierarchy)

**Goal:** A System Administrator can manage the entities created in Stages 3–6 entirely through the UI.

- [ ] Companies CRUD page (`/settings/companies`)
- [ ] Regions CRUD page (`/settings/regions`)
- [ ] Stations CRUD page (`/settings/stations`)
- [ ] Users page: invite, edit, deactivate, reset password, manage MFA, assign roles, assign station access
- [ ] Roles page: list default roles, view permission matrix (custom-role creation deferred)
- [ ] Audit log viewer (`/audit`) with filter UI matching the API filters
- [ ] Tenant/company/region/station switcher in the top command bar, persisted in `useTenantContext`
- [ ] Profile page: change password, manage MFA, view active sessions, revoke sessions

**Done when:** A fresh tenant can be configured from zero to "ready for Phase 2 (Fuel Infrastructure)" without anyone touching the database.

---

## Stage 10 — CI/CD & Observability Baseline

**Goal:** Every push runs the full suite; the API exports the observability signals we'll need from Phase 2 onward.

- [ ] GitHub Actions: `lint`, `typecheck`, `test` (web + api), `build`, `docker-build-api` on push to any branch
- [ ] Branch protection on `main`: require checks to pass
- [ ] OpenAPI spec generation from Go handlers (use chi-openapi or hand-maintained spec) — published as an artifact
- [ ] SDK regeneration step in CI when OpenAPI changes
- [ ] Structured logging fields standardized: `request_id`, `correlation_id`, `tenant_id`, `user_id`, `service`, `operation`, `latency_ms`, `status`
- [ ] `/metrics` endpoint exporting Prometheus format (request count, latency histogram, DB pool stats, outbox lag)
- [ ] OpenTelemetry tracing stub: spans emitted but exporter configurable (stdout for now, OTLP later)
- [ ] Error tracking: Sentry SDK in both web and api, gated behind env var (off by default)
- [ ] Deployment target decision documented in `docs/deployment.md` (Fly.io / Railway / Render / self-hosted — defer the actual deploy until Phase 1 is feature-complete, but pick the target now)

**Done when:** A PR shows green CI with all checks, the api Docker image is built and tagged on merge to main, and `/metrics` returns Prometheus-formatted output.

---

## Phase 1 acceptance criteria

Phase 1 is complete when **all** of the following are true:

1. A new tenant can be created via API, configured fully via the admin UI, and have users invited.
2. Users can log in with MFA, sessions are revocable, and password reset works end-to-end.
3. Every API endpoint enforces tenant isolation and permission scoping; tests prove it.
4. Every write produces an outbox event in the same transaction; sensitive writes also produce an audit log entry.
5. The web app shell is visually consistent with the UI/UX doc's design tokens — premium feel, dark + light modes, sidebar + topbar + right panel + command palette all functional.
6. CI is green on `main`; api Docker images build automatically; observability signals are in place.
7. A demo Phase 2 developer can clone the repo, run `pnpm install && docker compose up && pnpm dev`, and start building fuel infrastructure features against a working platform.

---

## Out of scope for Phase 1 (intentionally)

These belong to later phases — do not let scope creep pull them in:

- Any fuel domain logic: products, tanks, pumps, nozzles, shifts, deliveries, sales, payments, inventory, finance
- Risk engine, AI assistant, forecasting, hardware integrations
- Mobile app (Phase 14 — but the web shell must be responsive enough that mobile work can layer on)
- Offline sync engine (Phase 14)
- ClickHouse analytics warehouse (Phase 11 needs it; defer)
- Kafka/NATS (in-process bus + outbox pattern is enough for now)
- Webhooks and public API keys (Phase 13)
