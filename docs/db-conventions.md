# Database Conventions

Conventions every table, column, and migration in FuelGrid OS must follow. Established in Stage 3; revisit only with deliberate intent.

## Engine and version

Postgres 16. Lower versions are not supported. Stay one major behind upstream so we benefit from patch releases without chasing bleeding-edge changes.

## Extensions

Required on every database:

- `pgcrypto` — for `gen_random_uuid()` until we move to UUIDv7

Stage 5+ will add `uuid-ossp` and possibly `pg_trgm` (for fuzzy customer/station search). Add them via migration when first needed, never as a manual setup step.

## Primary keys

- **Type:** `uuid` (native column type, not text)
- **Default:** `gen_random_uuid()` (UUIDv4)
- **Future:** Switch to UUIDv7 once a generator is in place. The column type doesn't change, only the default. Existing rows are unaffected.

Never use serial / bigserial / integer PKs for tenant-owned tables. Cross-tenant ID collisions are real and they leak information.

## Tenant scoping

**Every tenant-owned table MUST include:**

```sql
tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT
```

`ON DELETE RESTRICT`, not `CASCADE`. Tenants are not deleted by `DELETE` — they are soft-deleted via the `status` column. A cascade delete would be a catastrophic accident waiting to happen.

The only tables exempt from `tenant_id`:

- `tenants` itself
- Truly global lookup tables (none yet; document in this file when introduced)
- Operational tables for the platform itself (e.g., system audit of inter-tenant operations — none yet)

## Standard columns

Every table includes:

```sql
created_at  timestamptz NOT NULL DEFAULT now(),
updated_at  timestamptz NOT NULL DEFAULT now()
```

And a trigger to bump `updated_at` automatically:

```sql
CREATE TRIGGER <table>_set_updated_at
    BEFORE UPDATE ON <table>
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
```

`set_updated_at()` is a reusable plpgsql function created in migration 0001.

`created_by uuid REFERENCES users(id)` and `updated_by uuid REFERENCES users(id)` are added in Stage 4 when the auth context exists. Until then, they are nullable; once Stage 4 lands they become NOT NULL on tables created after that point.

## Status columns

Lifecycle is expressed via a `status` column with a CHECK constraint, not a separate boolean per state. Standard values:

| State | Meaning |
|---|---|
| `active` | Normal operation |
| `invited` | Created but not yet onboarded (users) |
| `suspended` | Temporarily disabled, retains data |
| `closed` | Permanently retired but kept for history (stations) |
| `deleted` | Soft-deleted; excluded from default queries |

Add tenant-specific states (e.g., `pending_delivery`, `overdue`) on the table that needs them. Don't bloat the global lexicon.

## Soft delete

Soft-delete via `status = 'deleted'`, not via `deleted_at`. Reasons:

1. One column expresses the full lifecycle.
2. Indexes can `WHERE status <> 'deleted'` to enforce uniqueness on active rows only.
3. Date of deletion is recoverable from `updated_at` plus the audit log.

If a future table genuinely needs a separate `deleted_at` (e.g., GDPR-driven hard-delete-after-N-days), document the exception inline in the migration.

## Append-only tables

These tables never UPDATE existing rows. Corrections create new adjustment rows:

- `stock_movements` (Stage 4)
- `outbox_events` (Stage 7)
- `audit_logs` (Stage 7)

They include `created_at` but not `updated_at`. The `set_updated_at` trigger is intentionally not attached.

## Naming

| Element | Rule | Example |
|---|---|---|
| Tables | plural, snake_case | `stations`, `stock_movements` |
| Columns | snake_case | `tenant_id`, `liters_dispensed` |
| Primary key | `id` | not `tenant_id`, not `tenants_pkey` |
| Foreign keys | `<referenced_singular>_id` | `tenant_id`, `company_id` |
| Indexes | `idx_<table>_<columns>` | `idx_stations_tenant_id` |
| Unique indexes | `idx_<table>_<columns>` (suffix optional) | `idx_companies_tenant_name` |
| Check constraints | `chk_<table>_<column>` | `chk_stations_status` |
| Triggers | `<table>_<purpose>` | `stations_set_updated_at` |

## Indexes

Every `tenant_id` gets an index. Every foreign key column gets an index — Postgres does not auto-create them.

Compose multi-column indexes by query pattern, not by intuition. The first column should be the one with the highest selectivity for the queries the index serves. `(tenant_id, status, created_at)` is a common shape.

Unique indexes on user-facing slugs / codes filter out soft-deleted rows so re-use is possible after deletion:

```sql
CREATE UNIQUE INDEX idx_stations_tenant_code
    ON stations(tenant_id, code) WHERE status <> 'deleted';
```

## Migrations

File pattern: `NNNN_short_slug.up.sql` and `NNNN_short_slug.down.sql`, zero-padded sequential.

Rules:

1. **One concern per migration.** Don't bundle a schema change with a backfill in the same file.
2. **Both `up` and `down` must work during development.** Production migrations may eventually be irreversible by design, but require explicit sign-off in the PR.
3. **No data manipulation in schema migrations.** Use a separate seed or data migration for that.
4. **Migrations are transactional by default.** golang-migrate wraps each in a transaction; don't break this with `CREATE INDEX CONCURRENTLY` unless you understand the implications.
5. **Never edit a migration that has been merged to `main`.** Write a forward migration to fix mistakes.

## Row-Level Security

Postgres RLS is enabled in Stage 6 as a defense-in-depth layer behind application-level tenant scoping. Every tenant-owned table gets a policy that checks `tenant_id = current_setting('app.current_tenant')::uuid`. Application code sets this per-transaction.

Migrations after Stage 6 must include the RLS policy in the same file as the `CREATE TABLE`. Stage 6's migration retrofits RLS to tables 0001-0002 already added.

## Querying conventions

All repository-layer queries pass `tenantID` as the first scoping argument. Repositories that don't are bugs.

```go
func (r *Repo) GetStation(ctx context.Context, tenantID, stationID uuid.UUID) (*Station, error) {
    // WHERE tenant_id = $1 AND id = $2
}
```

Application code never builds SQL by string concatenation with tenant values. Use parameterized queries.
