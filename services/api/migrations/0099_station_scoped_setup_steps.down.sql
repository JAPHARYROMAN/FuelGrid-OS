-- Reverse 0099_station_scoped_setup_steps.

DELETE FROM setup_steps WHERE station_id IS NOT NULL;

DROP INDEX IF EXISTS idx_setup_steps_station_id;
DROP INDEX IF EXISTS uq_setup_steps_station;
DROP INDEX IF EXISTS uq_setup_steps_global;

ALTER TABLE setup_steps
    DROP CONSTRAINT IF EXISTS setup_steps_station_fk;

ALTER TABLE setup_steps
    DROP COLUMN IF EXISTS station_id;

ALTER TABLE setup_steps
    ADD PRIMARY KEY (tenant_id, code);
