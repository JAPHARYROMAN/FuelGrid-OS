-- 0017_tank_dip_readings: tank dip readings per shift (Phase 3, Stage 4).
--
-- A dip is captured in millimetres and resolved to litres via the tank's
-- active calibration chart at capture time; the resolved volume_litres and
-- the chart_id that produced it are snapshotted on the row so a later
-- re-strap can't rewrite history. Append-only with supersede for
-- corrections, mirroring meter_readings.

CREATE TABLE tank_dip_readings (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    shift_id      uuid NOT NULL,
    tank_id       uuid NOT NULL,
    reading_type  text NOT NULL,
    dip_mm        numeric(14, 3) NOT NULL,
    volume_litres numeric(14, 3) NOT NULL,
    water_mm      numeric(14, 3),
    temperature_c numeric(6, 2),
    chart_id      uuid NOT NULL REFERENCES tank_calibration_charts(id) ON DELETE RESTRICT,
    recorded_by   uuid NOT NULL,
    recorded_at   timestamptz NOT NULL DEFAULT now(),
    supersedes_id uuid REFERENCES tank_dip_readings(id) ON DELETE SET NULL,
    status        text NOT NULL DEFAULT 'active',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_tank_dip_type    CHECK (reading_type IN ('opening', 'closing')),
    CONSTRAINT chk_tank_dip_dip     CHECK (dip_mm >= 0),
    CONSTRAINT chk_tank_dip_volume  CHECK (volume_litres >= 0),
    CONSTRAINT chk_tank_dip_water   CHECK (water_mm IS NULL OR water_mm >= 0),
    CONSTRAINT chk_tank_dip_status  CHECK (status IN ('active', 'superseded')),

    CONSTRAINT tank_dip_shift_fk
        FOREIGN KEY (tenant_id, shift_id) REFERENCES shifts(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT tank_dip_tank_fk
        FOREIGN KEY (tenant_id, tank_id) REFERENCES tanks(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT tank_dip_recorded_by_fk
        FOREIGN KEY (tenant_id, recorded_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_tank_dip_tenant_id ON tank_dip_readings(tenant_id);
CREATE INDEX idx_tank_dip_shift_id  ON tank_dip_readings(shift_id);
CREATE INDEX idx_tank_dip_tank_id   ON tank_dip_readings(tank_id);

-- At most one active opening and one active closing per (shift, tank).
CREATE UNIQUE INDEX idx_tank_dip_active
    ON tank_dip_readings(shift_id, tank_id, reading_type) WHERE status = 'active';

ALTER TABLE tank_dip_readings ADD CONSTRAINT uq_tank_dip_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER tank_dip_set_updated_at
    BEFORE UPDATE ON tank_dip_readings
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE tank_dip_readings ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tank_dip_readings
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Capture + correction reuse the existing reading.edit permission (0004).
