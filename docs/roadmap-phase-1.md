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

- [ ] Initialize pnpm workspace (`pnpm-workspace.yaml` covering `apps/*`, `services/*`, `packages/*`)
- [ ] Root `package.json` with shared scripts (`lint`, `typecheck`, `test`, `build`, `dev`)
- [ ] `packages/config` — shared `tsconfig.base.json`, `eslint-config`, `prettier-config`, `tailwind-preset`
- [ ] Husky + lint-staged for pre-commit lint/format
- [ ] `.editorconfig` and `.nvmrc` (Node 20 LTS)
- [ ] `.env.example` at repo root documenting all env vars used anywhere
- [ ] CONTRIBUTING.md with branch / commit conventions (Conventional Commits)
- [ ] GitHub Actions workflow: lint + typecheck on every PR

**Done when:** A fresh clone passes `pnpm install && pnpm lint && pnpm typecheck` with zero code.

---

## Stage 2 — Backend Service Skeleton

**Goal:** `services/api` runs, serves `/healthz`, logs structured JSON, shuts down gracefully.

- [ ] `go mod init github.com/japharyroman/fuelgrid-os`
- [ ] Layout: `services/api/cmd/api/main.go`, `services/api/internal/server`, shared `internal/` modules per architecture doc
- [ ] HTTP server using chi with middleware stack: request ID, recoverer, logger, CORS, timeout
- [ ] Config struct loaded via envconfig with sensible defaults
- [ ] `slog` JSON handler wired to log writer
- [ ] `/healthz` (liveness) and `/readyz` (deps reachable) endpoints
- [ ] Graceful shutdown on SIGTERM/SIGINT with context cancellation
- [ ] `Dockerfile` (multi-stage, distroless final image)
- [ ] `services/api/Makefile` with `run`, `build`, `test`, `lint` targets
- [ ] golangci-lint config + CI step

**Done when:** `docker build` + `docker run` boots the API, `/healthz` returns 200, logs are structured.

---

## Stage 3 — Database Foundation

**Goal:** Postgres + Redis run locally via compose; migrations apply cleanly; baseline tenant/org schema exists.

- [ ] `docker-compose.yml` at repo root: Postgres 16, Redis 7, with named volumes
- [ ] Add migration runner (`golang-migrate`) — `make migrate-up`, `make migrate-down`, `make migrate-new NAME=...`
- [ ] Decide on schema conventions and document in `docs/db-conventions.md`:
  - UUIDv7 PKs (`uuid` column type, `gen_random_uuid()` default for now)
  - `tenant_id uuid NOT NULL` on every tenant-owned table
  - `created_at`, `updated_at`, `created_by`, `updated_by` everywhere
  - Soft-delete via `deleted_at` only where business requires it
  - Snake_case tables/columns
- [ ] Migration `0001_init.sql`: `tenants`, `companies`, `regions`, `stations` with FK chain and indexes
- [ ] Migration `0002_users.sql`: `users` table (no auth fields yet — those come next stage)
- [ ] Connection pool (pgx) wired into API server with health probe in `/readyz`
- [ ] Seed script for one demo tenant + company + region + station

**Done when:** `make migrate-up` from a clean DB produces the schema; API can query `tenants` table.

---

## Stage 4 — Identity & Auth

**Goal:** Real users can sign in, sessions are revocable, password reset works, MFA is wired (even if optional).

- [ ] Migration: add auth fields to `users` (email, password_hash, mfa_secret, mfa_enabled, last_login_at, locked_until)
- [ ] Migration: `sessions` (id, user_id, tenant_id, device_id, ip, user_agent, issued_at, expires_at, revoked_at)
- [ ] Migration: `devices` (id, user_id, label, fingerprint, last_seen_at)
- [ ] Password hashing: argon2id with sane params; pepper from env
- [ ] Endpoints:
  - `POST /api/v1/auth/login` (email + password → session token)
  - `POST /api/v1/auth/logout` (revoke current session)
  - `POST /api/v1/auth/refresh` (extend session)
  - `POST /api/v1/auth/password-reset/request`
  - `POST /api/v1/auth/password-reset/confirm`
  - `POST /api/v1/auth/mfa/enroll` + `POST /api/v1/auth/mfa/verify`
- [ ] Session store: Redis with TTL = session expiry; opaque token = random 32-byte b64url
- [ ] Auth middleware: extract token → load session → inject `actor` into request context
- [ ] Rate limiting on `/auth/*` (Redis sliding window — e.g., 5 attempts / 15 min / IP)
- [ ] Audit hook: emit `UserLoggedIn`, `UserLoggedOut`, `PasswordChanged`, `MfaEnrolled` events (event infrastructure comes in Stage 7 — stub for now)

**Done when:** A seeded user can log in via curl, hit a protected endpoint, log out, and the session is gone from Redis.

---

## Stage 5 — RBAC & Permissions

**Goal:** Every protected action checks `actor + action + resource → allow/deny` via a single policy evaluator.

- [ ] Migration: `roles`, `permissions`, `user_roles`, `role_permissions`
- [ ] Seed default roles per PRD §5: Attendant, Supervisor, Station Manager, Regional Manager, Finance Officer, Procurement Officer, Auditor, Executive, System Administrator
- [ ] Seed default permissions (start with the list in PRD §8.1 — ~16 permissions covering shifts, stock, price, audit, exports, integrations)
- [ ] Policy evaluator package: `internal/identity/policy` with `Can(ctx, action, resource) error`
- [ ] HTTP middleware factory: `requirePermission("station.read")`
- [ ] Migration: `user_station_access` (user_id, station_id, granted_by, granted_at) — for station-scoped permissions
- [ ] Policy must consider: tenant → company → region → station scoping
- [ ] Endpoint: `GET /api/v1/me/permissions` (returns the actor's permission set + scopes for frontend)
- [ ] Unit tests covering each role × action × scope combination from the seed data

**Done when:** Calling a protected endpoint as an attendant returns 403; as a station manager for an assigned station returns 200; as a station manager for an unassigned station returns 403.

---

## Stage 6 — Multi-Tenancy Enforcement

**Goal:** No query can return another tenant's data. Tested and verified.

- [ ] Tenant resolver middleware: extract `tenant_id` from session, inject into context
- [ ] Repository layer convention: every query takes `tenantID` as first scoping parameter; reject queries that don't
- [ ] Postgres row-level security (RLS) policies on every tenant-owned table as defense-in-depth
- [ ] `SET app.current_tenant` per transaction; RLS policies reference it
- [ ] Tenant-isolation integration tests: create two tenants, attempt cross-tenant reads/writes via API → must all 404 or 403
- [ ] Document tenant safety rules in `docs/multi-tenancy.md`

**Done when:** Integration tests prove cross-tenant data access is impossible at API and DB layers.

---

## Stage 7 — Audit & Event Foundation

**Goal:** Every sensitive action emits a domain event and writes an immutable audit log. Outbox pattern wired end-to-end.

- [ ] Migration: `audit_logs` (id, tenant_id, actor_id, action, entity_type, entity_id, previous_value jsonb, new_value jsonb, reason, ip, user_agent, occurred_at)
- [ ] Migration: `outbox_events` (id, tenant_id, event_type, aggregate_type, aggregate_id, payload jsonb, metadata jsonb, occurred_at, published_at)
- [ ] Domain event envelope type matches architecture §13.2 fields
- [ ] Repository convention: every write in a transaction also writes to `outbox_events` in the same transaction
- [ ] Background outbox publisher goroutine: polls unpublished events → publishes to in-process bus (Kafka/NATS deferred to later phase) → marks published
- [ ] Audit interceptor: wraps sensitive handlers, captures before/after, writes to `audit_logs`
- [ ] Sensitive actions list per PRD §15.2 wired up (price change, permission change, record deletion, etc.) — most are placeholders for now since features don't exist yet
- [ ] `GET /api/v1/audit-logs` with filters (entity, actor, date range) — auditor-only

**Done when:** A user permission change writes both an `audit_logs` row and an `outbox_events` row in the same transaction, and the publisher dispatches the event.

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
