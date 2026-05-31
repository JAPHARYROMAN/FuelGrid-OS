# Multi-Tenancy

How FuelGrid OS guarantees that one customer's data never reaches another customer.

## The threat model

A FuelGrid OS deployment serves many tenants from one database. The data of every tenant is in the same tables; the only thing keeping them apart is the `tenant_id` column on every tenant-owned row. A single missing `WHERE tenant_id = ?` in any handler, repo, report query, AI prompt, or background job is enough to leak data.

We treat this as a serious, ongoing risk and apply **four independent layers** of defense. A bug in any single layer must not result in cross-tenant access.

## The four layers

### 1. Tenant resolution (where does `tenant_id` come from?)

Tenants are resolved from a small, deliberate set of sources:

| Source | When |
|---|---|
| Authenticated session | Default. Every authenticated request has an `identity.Actor` on the context with `TenantID` set. |
| Body field on login | Pre-auth flow only. `POST /api/v1/auth/login` accepts `tenant_slug`. |
| API key scope | When introduced (Stage 13). Each key is tenant-bound at issuance. |

**Never:** subdomains alone, query strings, untrusted headers, or anything else a client controls outside of the authenticated session.

The `Actor.TenantID` field is the only value any handler or repo should read for scoping decisions.

### 2. Application scoping (the primary defense)

**Every tenant-owned table is queried with `tenant_id` in the WHERE.** No exceptions.

Repository convention:

```go
func (r *Repo) GetStation(ctx context.Context, tenantID, stationID uuid.UUID) (*Station, error) {
    // WHERE tenant_id = $1 AND id = $2
}
```

Tenant IDs always come from `Actor.TenantID`, never from URL paths or request bodies. A handler that wants to operate on a row by its primary key still scopes by tenant:

```go
actor, _ := identity.Require(r.Context())
row, err := repo.GetStation(ctx, actor.TenantID, stationID)
```

A station from another tenant doesn't error — it returns `pgx.ErrNoRows`, which the handler maps to 404. Indistinguishable from "no such station".

### 3. Authorization scoping (orthogonal to tenant scoping)

The policy evaluator ([internal/identity/policy](../internal/identity/policy/)) layers role-based and station-based access checks on top of tenant scoping. Even with the right tenant ID, an attendant cannot read another attendant's shift. This layer is documented in [the Stage 5 work](../services/api/migrations/0004_rbac.up.sql) and the policy package's own doc comment.

### 4. Row-level security (the safety net)

Every tenant-owned table has a Postgres RLS policy that enforces `tenant_id = current_setting('app.current_tenant')`. If application code ever forgets a WHERE clause, RLS still refuses to return cross-tenant rows.

#### Current posture

- RLS is **enabled** on **every** tenant-owned table. Migration `0005` covered the Phase-1 tables; later phase migrations added it inline; migration `0074` closes any remaining gaps by enabling the standard `tenant_isolation` policy on every table with a `NOT NULL tenant_id` that lacked one (INFRA-01/AUTH-25/MT-4).
- **The API runs as the non-owner `fuelgrid_app` role in production, by default.** Request-scoped queries acquire a `fuelgrid_app` connection with `app.current_tenant` set per request (see `database.AcquireTenant`), so RLS is the **operative** DB-layer defense — not just app-layer scoping. `config.validate()` **fail-stops** outside development if `DATABASE_APP_URL` is unset (or equals `DATABASE_URL`), so production can never boot RLS-bypassed. In development the owner-pool fallback remains for convenience.
- Pre-auth paths (login, session resolve, tenant lookup by slug), platform provisioning, the seed, and owner-pool background jobs / the outbox publisher run as the **owner** and bypass RLS — they legitimately operate before or across tenant context.
- RLS is **enabled, not FORCED**. FORCE (subjecting the owner too) is deferred: the seed and every owner-run background job would first need to set a per-tenant `app.current_tenant` GUC. ENABLE already makes the `fuelgrid_app` request path fail-closed, which is the operative protection.
- `fuelgrid_app` isolation is proven in CI by `rls_integration_test.go` through the real request connection path.

#### Tables under RLS

| Table | Policy | Notes |
|---|---|---|
| `companies` | `tenant_isolation` | Standard `tenant_id` match. |
| `regions` | `tenant_isolation` | Standard. |
| `stations` | `tenant_isolation` | Standard. |
| `users` | `tenant_isolation` | Standard. |
| `devices` | `tenant_isolation` | Standard. |
| `sessions` | `tenant_isolation` | Standard. |
| `user_roles` | `tenant_isolation` | Standard. |
| `user_station_access` | `tenant_isolation` | Standard. |
| `roles` | `tenant_or_system` | Visible if `tenant_id IS NULL` (system role) **or** matches the GUC. |

#### Tables intentionally not under RLS

| Table | Why |
|---|---|
| `tenants` | Top of the hierarchy. Login looks up by slug before any tenant context exists. |
| `permissions` | Platform-wide vocabulary. Same for every tenant. |
| `role_permissions` | Joined to `roles`; protection rides on the `roles` policy. |

## Connecting to the database

| Role | Purpose | RLS behavior |
|---|---|---|
| `fuelgrid` (superuser) | Migrations, seeds, and the API runtime today | Bypasses RLS — owns the tables |
| `fuelgrid_app` | CI tests; future API runtime | Subject to RLS, fail-closed when `app.current_tenant` is unset |

In dev, both roles use the password `fuelgrid` / `fuelgrid_app`. **Production must rotate these via secret store.** Migration 0005 creates `fuelgrid_app` only if it doesn't already exist, so IaC can pre-create it with a real password.

## The contract for new code

When you write a query against a tenant-owned table, you have two options.

### Option A — explicit `WHERE tenant_id = ?` (default)

```go
row, err := pool.QueryRow(ctx,
    `SELECT ... FROM stations WHERE id = $1 AND tenant_id = $2`,
    stationID, actor.TenantID,
).Scan(...)
```

This works against the superuser connection the API uses today. It's the pattern every existing repo follows.

### Option B — `database.WithTenant(...)` (when RLS-enforced isolation matters)

```go
err := database.WithTenant(ctx, pool, actor.TenantID, func(tx pgx.Tx) error {
    // Inside this transaction, app.current_tenant is set. RLS policies
    // apply automatically — even queries without a WHERE clause only
    // see rows belonging to this tenant.
    return tx.Exec(ctx, "INSERT INTO companies (name) VALUES ($1) RETURNING id, ...").Scan(...)
})
```

Use this when:

- Multiple tenant-scoped tables are touched in one transaction and you want RLS' `WITH CHECK` to fail closed if any row's `tenant_id` slips.
- The query is complex enough that you'd rather rely on the DB to enforce the boundary than carefully audit every JOIN.
- Migrating the call site onto `fuelgrid_app` is on the near-term horizon.

## Test patterns

### Application-layer isolation test

Create two tenants. Log in as a user of tenant A. Try to access a resource belonging to tenant B by its primary key. Expect 404 (the row exists, but the `WHERE tenant_id` filter excludes it).

### DB-layer isolation test

Connect as `fuelgrid_app`. Run `SET LOCAL app.current_tenant = '<tenant-A-uuid>'` then `SELECT * FROM stations`. Expect to see only tenant A's rows. Repeat for tenant B; expect only tenant B's rows. Without `SET LOCAL`, the same query returns zero rows.

Both kinds of tests run on every CI build under the `migrations` job — see [.github/workflows/ci.yml](../.github/workflows/ci.yml).

## Migration checklist for new tenant-owned tables

When a future migration adds a new tenant-owned table:

1. Include `tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT`.
2. Add an index on `tenant_id`.
3. Add `ENABLE ROW LEVEL SECURITY` + a `tenant_isolation` policy mirroring the templates in [0005_rls.up.sql](../services/api/migrations/0005_rls.up.sql).
4. Grants to `fuelgrid_app` happen automatically via `ALTER DEFAULT PRIVILEGES` set in 0005.

Reviewers should reject migrations that add tenant-owned tables without RLS.
