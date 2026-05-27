-- Reverse of 0005_rls.

DROP POLICY IF EXISTS tenant_or_system   ON roles;
DROP POLICY IF EXISTS tenant_isolation   ON user_station_access;
DROP POLICY IF EXISTS tenant_isolation   ON user_roles;
DROP POLICY IF EXISTS tenant_isolation   ON sessions;
DROP POLICY IF EXISTS tenant_isolation   ON devices;
DROP POLICY IF EXISTS tenant_isolation   ON users;
DROP POLICY IF EXISTS tenant_isolation   ON stations;
DROP POLICY IF EXISTS tenant_isolation   ON regions;
DROP POLICY IF EXISTS tenant_isolation   ON companies;

ALTER TABLE roles                ENABLE ROW LEVEL SECURITY; -- keep current state
ALTER TABLE roles                DISABLE ROW LEVEL SECURITY;
ALTER TABLE user_station_access  DISABLE ROW LEVEL SECURITY;
ALTER TABLE user_roles           DISABLE ROW LEVEL SECURITY;
ALTER TABLE sessions             DISABLE ROW LEVEL SECURITY;
ALTER TABLE devices              DISABLE ROW LEVEL SECURITY;
ALTER TABLE users                DISABLE ROW LEVEL SECURITY;
ALTER TABLE stations             DISABLE ROW LEVEL SECURITY;
ALTER TABLE regions              DISABLE ROW LEVEL SECURITY;
ALTER TABLE companies            DISABLE ROW LEVEL SECURITY;

-- Revoke the privileges we granted in up. The role itself is left in
-- place — dropping a role with extant grants on other databases is
-- noisy, and Postgres roles are cluster-global.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM fuelgrid_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    REVOKE USAGE, SELECT ON SEQUENCES FROM fuelgrid_app;

REVOKE USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public FROM fuelgrid_app;
REVOKE SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public FROM fuelgrid_app;
REVOKE USAGE ON SCHEMA public FROM fuelgrid_app;
