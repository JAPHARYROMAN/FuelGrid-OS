-- 0008_tenant_integrity: enforce tenant-bound foreign keys.
--
-- Audit findings #2/#3: hierarchy and user-scope writes accepted parent
-- ids (company_id, region_id, station_id, user_id) without proving they
-- belong to the actor's tenant, and the FKs pointed only at `id`. That
-- let a tenant-A row link to a tenant-B entity.
--
-- Fix: composite (tenant_id, id) unique keys on the parents, and
-- composite FKs on the children so Postgres itself rejects any
-- cross-tenant link. App-layer guards (separate change) turn the
-- resulting constraint errors into clean 404s, but the DB is now the
-- backstop.

-- ---------------------------------------------------------------------------
-- Parent composite unique keys (FK targets).
-- ---------------------------------------------------------------------------
ALTER TABLE companies ADD CONSTRAINT uq_companies_tenant_id UNIQUE (tenant_id, id);
ALTER TABLE regions   ADD CONSTRAINT uq_regions_tenant_id   UNIQUE (tenant_id, id);
ALTER TABLE stations  ADD CONSTRAINT uq_stations_tenant_id  UNIQUE (tenant_id, id);
ALTER TABLE users     ADD CONSTRAINT uq_users_tenant_id     UNIQUE (tenant_id, id);

-- ---------------------------------------------------------------------------
-- regions.company_id must belong to the same tenant.
-- ---------------------------------------------------------------------------
ALTER TABLE regions DROP CONSTRAINT regions_company_id_fkey;
ALTER TABLE regions ADD CONSTRAINT regions_company_fk
    FOREIGN KEY (tenant_id, company_id) REFERENCES companies(tenant_id, id) ON DELETE RESTRICT;

-- ---------------------------------------------------------------------------
-- stations.company_id and stations.region_id must belong to the tenant.
-- ---------------------------------------------------------------------------
ALTER TABLE stations DROP CONSTRAINT stations_company_id_fkey;
ALTER TABLE stations ADD CONSTRAINT stations_company_fk
    FOREIGN KEY (tenant_id, company_id) REFERENCES companies(tenant_id, id) ON DELETE RESTRICT;

ALTER TABLE stations DROP CONSTRAINT stations_region_id_fkey;
ALTER TABLE stations ADD CONSTRAINT stations_region_fk
    FOREIGN KEY (tenant_id, region_id) REFERENCES regions(tenant_id, id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- user_roles.user_id must belong to the tenant. role_id stays a plain FK:
-- system roles have tenant_id IS NULL and can't take part in a composite
-- tenant key.
-- ---------------------------------------------------------------------------
ALTER TABLE user_roles DROP CONSTRAINT user_roles_user_id_fkey;
ALTER TABLE user_roles ADD CONSTRAINT user_roles_user_fk
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE;

-- ---------------------------------------------------------------------------
-- user_station_access: both the user and the station must belong to the
-- tenant.
-- ---------------------------------------------------------------------------
ALTER TABLE user_station_access DROP CONSTRAINT user_station_access_user_id_fkey;
ALTER TABLE user_station_access ADD CONSTRAINT usa_user_fk
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE user_station_access DROP CONSTRAINT user_station_access_station_id_fkey;
ALTER TABLE user_station_access ADD CONSTRAINT usa_station_fk
    FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE CASCADE;
