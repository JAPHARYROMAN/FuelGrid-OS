-- 0023_station_consistency: pin operational records to a single station at
-- the DB level (Phase 3 audit P2).
--
-- 0014/0015 FK'd children to parents independently, so the schema did not
-- guarantee a shift's station matched its operating day, or that a nozzle
-- assignment's nozzle lived at the shift's station. Handlers checked this,
-- but accounting data deserves the DB as the final guard. We add
-- station-bearing composite uniques on the parents and composite FKs on the
-- children. (A reading's "nozzle is assigned to the shift" invariant stays
-- handler-enforced for all roles — a pure FK there tangles with the
-- assignment-cascade and reading-supersede paths.)

-- FK targets: parents made unique on (tenant_id, station_id, id).
ALTER TABLE operating_days ADD CONSTRAINT uq_operating_days_tenant_station_id UNIQUE (tenant_id, station_id, id);
ALTER TABLE shifts          ADD CONSTRAINT uq_shifts_tenant_station_id          UNIQUE (tenant_id, station_id, id);
ALTER TABLE nozzles         ADD CONSTRAINT uq_nozzles_tenant_station_id         UNIQUE (tenant_id, station_id, id);

-- A shift's station must equal its operating day's station.
ALTER TABLE shifts
    ADD CONSTRAINT shifts_day_station_fk
    FOREIGN KEY (tenant_id, station_id, operating_day_id)
    REFERENCES operating_days (tenant_id, station_id, id) ON DELETE RESTRICT;

-- Carry the station on each nozzle assignment and pin it to both the shift
-- and the nozzle, so all three stations are provably equal.
ALTER TABLE shift_nozzle_assignments ADD COLUMN station_id uuid;
UPDATE shift_nozzle_assignments sna
    SET station_id = s.station_id
    FROM shifts s
    WHERE s.id = sna.shift_id;
ALTER TABLE shift_nozzle_assignments ALTER COLUMN station_id SET NOT NULL;

ALTER TABLE shift_nozzle_assignments
    ADD CONSTRAINT sna_shift_station_fk
    FOREIGN KEY (tenant_id, station_id, shift_id)
    REFERENCES shifts (tenant_id, station_id, id) ON DELETE CASCADE;
ALTER TABLE shift_nozzle_assignments
    ADD CONSTRAINT sna_nozzle_station_fk
    FOREIGN KEY (tenant_id, station_id, nozzle_id)
    REFERENCES nozzles (tenant_id, station_id, id) ON DELETE RESTRICT;
