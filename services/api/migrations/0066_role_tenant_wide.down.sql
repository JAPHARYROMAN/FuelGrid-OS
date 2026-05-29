-- Revert 0066_role_tenant_wide: drop the explicit tenant-wide flag. The loader
-- falls back to its prior behaviour (no user_station_access rows ⇒ tenant-wide).
ALTER TABLE roles DROP COLUMN tenant_wide;
