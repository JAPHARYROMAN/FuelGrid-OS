-- 0097_nozzle_initial_meter: nozzle-specific starting meter readings.
--
-- A physical dispenser meter has a counter value before the first live shift,
-- and it may be reset/recalibrated during equipment maintenance. This value is
-- owned by one nozzle, not by a pump or tank, and is separate from per-shift
-- opening/closing meter_readings.

ALTER TABLE nozzles
    ADD COLUMN initial_meter_reading numeric(14, 4),
    ADD COLUMN initial_meter_recorded_at timestamptz,
    ADD COLUMN initial_meter_recorded_by uuid,
    ADD COLUMN initial_meter_note text;

ALTER TABLE nozzles
    ADD CONSTRAINT chk_nozzles_initial_meter_value
        CHECK (initial_meter_reading IS NULL OR initial_meter_reading >= 0),
    ADD CONSTRAINT chk_nozzles_initial_meter_metadata
        CHECK (
            initial_meter_reading IS NULL
            OR (initial_meter_recorded_at IS NOT NULL AND initial_meter_recorded_by IS NOT NULL)
        ),
    ADD CONSTRAINT nozzles_initial_meter_recorded_by_fk
        FOREIGN KEY (tenant_id, initial_meter_recorded_by)
        REFERENCES users(tenant_id, id) ON DELETE RESTRICT;
