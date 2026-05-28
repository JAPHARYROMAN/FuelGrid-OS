-- Reverse of 0008_tenant_integrity: restore single-column FKs.

ALTER TABLE user_station_access DROP CONSTRAINT usa_station_fk;
ALTER TABLE user_station_access ADD CONSTRAINT user_station_access_station_id_fkey
    FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE;

ALTER TABLE user_station_access DROP CONSTRAINT usa_user_fk;
ALTER TABLE user_station_access ADD CONSTRAINT user_station_access_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE user_roles DROP CONSTRAINT user_roles_user_fk;
ALTER TABLE user_roles ADD CONSTRAINT user_roles_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE stations DROP CONSTRAINT stations_region_fk;
ALTER TABLE stations ADD CONSTRAINT stations_region_id_fkey
    FOREIGN KEY (region_id) REFERENCES regions(id) ON DELETE SET NULL;

ALTER TABLE stations DROP CONSTRAINT stations_company_fk;
ALTER TABLE stations ADD CONSTRAINT stations_company_id_fkey
    FOREIGN KEY (company_id) REFERENCES companies(id) ON DELETE RESTRICT;

ALTER TABLE regions DROP CONSTRAINT regions_company_fk;
ALTER TABLE regions ADD CONSTRAINT regions_company_id_fkey
    FOREIGN KEY (company_id) REFERENCES companies(id) ON DELETE RESTRICT;

ALTER TABLE users     DROP CONSTRAINT uq_users_tenant_id;
ALTER TABLE stations  DROP CONSTRAINT uq_stations_tenant_id;
ALTER TABLE regions   DROP CONSTRAINT uq_regions_tenant_id;
ALTER TABLE companies DROP CONSTRAINT uq_companies_tenant_id;
