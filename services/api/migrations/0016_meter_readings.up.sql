-- 0016_meter_readings: pump meter readings per shift (Phase 3, Stage 3).
--
-- Each nozzle gets an opening and a closing meter reading per shift. Readings
-- are append-only: a correction supersedes the prior reading (supersedes_id)
-- rather than overwriting it, so history stays auditable. Litres dispensed
-- per nozzle is closing - opening, computed in the API.

CREATE TABLE meter_readings (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    shift_id      uuid NOT NULL,
    nozzle_id     uuid NOT NULL,
    reading_type  text NOT NULL,
    reading       numeric(14, 3) NOT NULL,
    recorded_by   uuid NOT NULL,
    recorded_at   timestamptz NOT NULL DEFAULT now(),
    supersedes_id uuid REFERENCES meter_readings(id) ON DELETE SET NULL,
    status        text NOT NULL DEFAULT 'active',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_meter_readings_type    CHECK (reading_type IN ('opening', 'closing')),
    CONSTRAINT chk_meter_readings_value   CHECK (reading >= 0),
    CONSTRAINT chk_meter_readings_status  CHECK (status IN ('active', 'superseded')),

    CONSTRAINT meter_readings_shift_fk
        FOREIGN KEY (tenant_id, shift_id) REFERENCES shifts(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT meter_readings_nozzle_fk
        FOREIGN KEY (tenant_id, nozzle_id) REFERENCES nozzles(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT meter_readings_recorded_by_fk
        FOREIGN KEY (tenant_id, recorded_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_meter_readings_tenant_id ON meter_readings(tenant_id);
CREATE INDEX idx_meter_readings_shift_id  ON meter_readings(shift_id);
CREATE INDEX idx_meter_readings_nozzle_id ON meter_readings(nozzle_id);

-- At most one active opening and one active closing per (shift, nozzle).
CREATE UNIQUE INDEX idx_meter_readings_active
    ON meter_readings(shift_id, nozzle_id, reading_type) WHERE status = 'active';

-- FK target for later-phase references.
ALTER TABLE meter_readings ADD CONSTRAINT uq_meter_readings_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER meter_readings_set_updated_at
    BEFORE UPDATE ON meter_readings
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE meter_readings ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON meter_readings
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Capture + correction reuse the existing reading.edit permission (0004).
