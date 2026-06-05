-- 0099_station_scoped_setup_steps: make operational setup reviews station-scoped.
--
-- Global setup steps (company, regions, products, suppliers, users) still have
-- station_id NULL. Station-specific setup steps (tanks, pumps, nozzles,
-- opening_stock, employees, teams, rotation_anchor) are reviewed per station so
-- one site can go live without being blocked by every other site in the tenant.

ALTER TABLE setup_steps
    ADD COLUMN station_id uuid;

ALTER TABLE setup_steps
    DROP CONSTRAINT setup_steps_pkey;

ALTER TABLE setup_steps
    ADD CONSTRAINT setup_steps_station_fk
    FOREIGN KEY (tenant_id, station_id)
    REFERENCES stations(tenant_id, id)
    ON DELETE CASCADE;

CREATE UNIQUE INDEX uq_setup_steps_global
    ON setup_steps (tenant_id, code)
    WHERE station_id IS NULL;

CREATE UNIQUE INDEX uq_setup_steps_station
    ON setup_steps (tenant_id, station_id, code)
    WHERE station_id IS NOT NULL;

CREATE INDEX idx_setup_steps_station_id
    ON setup_steps (tenant_id, station_id)
    WHERE station_id IS NOT NULL;
