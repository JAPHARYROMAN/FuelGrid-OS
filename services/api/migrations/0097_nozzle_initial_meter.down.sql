-- Reverse of 0097_nozzle_initial_meter.

ALTER TABLE nozzles
    DROP CONSTRAINT IF EXISTS nozzles_initial_meter_recorded_by_fk,
    DROP CONSTRAINT IF EXISTS chk_nozzles_initial_meter_metadata,
    DROP CONSTRAINT IF EXISTS chk_nozzles_initial_meter_value;

ALTER TABLE nozzles
    DROP COLUMN IF EXISTS initial_meter_note,
    DROP COLUMN IF EXISTS initial_meter_recorded_by,
    DROP COLUMN IF EXISTS initial_meter_recorded_at,
    DROP COLUMN IF EXISTS initial_meter_reading;
