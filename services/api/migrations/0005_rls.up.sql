-- 0005_rls: defense-in-depth row-level security.
--
-- See docs/multi-tenancy.md for the full design rationale. Short version:
--
--   1. Application code already filters every tenant-scoped query by
--      tenant_id. That is the primary defense.
--
--   2. This migration adds Postgres RLS policies as a secondary defense:
--      if app code ever forgets a WHERE clause, RLS still refuses to
--      return cross-tenant rows.
--
--   3. RLS is ENABLED but NOT FORCED. Table owners (including the
--      superuser the API currently connects as) bypass RLS. A new
--      `fuelgrid_app` role does NOT bypass — that role is what CI tests
--      use to prove DB-layer isolation, and is the connection a future
--      stage will migrate the API onto.
--
-- ---------------------------------------------------------------------------

-- ---------------------------------------------------------------------------
-- Application role.
-- Idempotent: re-applying the migration on a database where the role
-- already exists (e.g. created by IaC) is a no-op. Production deployments
-- should rotate the password via secret store and not rely on the default.
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fuelgrid_app') THEN
        CREATE ROLE fuelgrid_app LOGIN PASSWORD 'fuelgrid_app';
    END IF;
END$$;

-- Schema usage + DML on every existing table. ALTER DEFAULT PRIVILEGES
-- below extends this to tables created by future migrations.
GRANT USAGE ON SCHEMA public TO fuelgrid_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO fuelgrid_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fuelgrid_app;

ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO fuelgrid_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO fuelgrid_app;

-- ---------------------------------------------------------------------------
-- The tenant GUC.
-- Application code sets `app.current_tenant` per transaction:
--     SET LOCAL app.current_tenant = '<uuid>';
-- Reads use current_setting('app.current_tenant', true). The trailing
-- `true` means "missing setting" returns NULL, which makes the policy
-- expression NULL and the comparison fails closed.
-- ---------------------------------------------------------------------------

-- ---------------------------------------------------------------------------
-- Policies on tenant-owned tables.
-- Each table gets the same shape: ENABLE RLS, then a single
-- `tenant_isolation` policy with USING (reads) and WITH CHECK (writes).
-- ---------------------------------------------------------------------------

ALTER TABLE companies ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON companies
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE regions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON regions
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE stations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON stations
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON users
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE devices ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON devices
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sessions
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE user_roles ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON user_roles
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE user_station_access ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON user_station_access
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- roles is special: system roles have tenant_id IS NULL and must be
-- visible to every tenant (they're the platform's role catalogue).
ALTER TABLE roles ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_or_system ON roles
    USING      (tenant_id IS NULL OR tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id IS NULL OR tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Intentionally NOT RLS-controlled:
--   • tenants            — top of the hierarchy; login looks up by slug
--                          before any actor / tenant context exists.
--   • permissions        — platform-wide vocabulary, no tenant scope.
--   • role_permissions   — joined to roles; protection comes via roles RLS.
-- ---------------------------------------------------------------------------
