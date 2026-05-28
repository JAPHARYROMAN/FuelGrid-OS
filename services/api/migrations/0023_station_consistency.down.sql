-- Revert 0023.
ALTER TABLE shift_nozzle_assignments DROP CONSTRAINT sna_nozzle_station_fk;
ALTER TABLE shift_nozzle_assignments DROP CONSTRAINT sna_shift_station_fk;
ALTER TABLE shift_nozzle_assignments DROP COLUMN station_id;

ALTER TABLE shifts DROP CONSTRAINT shifts_day_station_fk;

ALTER TABLE nozzles         DROP CONSTRAINT uq_nozzles_tenant_station_id;
ALTER TABLE shifts          DROP CONSTRAINT uq_shifts_tenant_station_id;
ALTER TABLE operating_days  DROP CONSTRAINT uq_operating_days_tenant_station_id;
