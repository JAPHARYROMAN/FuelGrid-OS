-- 0013_pump_cal_and_incidents: pump calibration events + the incidents
-- queue (Phase 2, Stage 5).
--
-- Calibrations and incident transitions are sensitive writes: each one
-- rides the audit + outbox pipeline in its handler, exactly like every
-- other Phase-1 mutation. The tables here are the durable record.

-- ---------------------------------------------------------------------------
-- pump_calibrations — one row per calibration event for a pump.
-- ---------------------------------------------------------------------------
CREATE TABLE pump_calibrations (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    pump_id           uuid NOT NULL,
    performed_at      timestamptz NOT NULL DEFAULT now(),
    performed_by      uuid NOT NULL,
    notes             text,
    tolerance_percent numeric(5, 2),
    status            text NOT NULL DEFAULT 'passed',
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_pumpcal_status CHECK (status IN ('passed', 'failed', 'adjusted')),

    CONSTRAINT pumpcal_pump_fk
        FOREIGN KEY (tenant_id, pump_id) REFERENCES pumps(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT pumpcal_performed_by_fk
        FOREIGN KEY (tenant_id, performed_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_pumpcal_tenant_id ON pump_calibrations(tenant_id);
CREATE INDEX idx_pumpcal_pump_id   ON pump_calibrations(pump_id);

CREATE TRIGGER pumpcal_set_updated_at
    BEFORE UPDATE ON pump_calibrations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE pump_calibrations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pump_calibrations
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- incidents — operational issues raised against a station or one of its
-- entities. related_entity_* is a loose (polymorphic) pointer, so no FK.
-- ---------------------------------------------------------------------------
CREATE TABLE incidents (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id          uuid NOT NULL,
    related_entity_type text,
    related_entity_id   uuid,
    type                text NOT NULL DEFAULT 'other',
    severity            text NOT NULL DEFAULT 'medium',
    description         text NOT NULL,
    status              text NOT NULL DEFAULT 'open',
    opened_at           timestamptz NOT NULL DEFAULT now(),
    opened_by           uuid NOT NULL,
    resolved_at         timestamptz,
    resolved_by         uuid,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_incidents_type CHECK (
        type IN ('equipment', 'leak', 'variance', 'safety', 'calibration', 'other')
    ),
    CONSTRAINT chk_incidents_severity CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    CONSTRAINT chk_incidents_status   CHECK (status IN ('open', 'investigating', 'resolved', 'closed')),

    CONSTRAINT incidents_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT incidents_opened_by_fk
        FOREIGN KEY (tenant_id, opened_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT incidents_resolved_by_fk
        FOREIGN KEY (tenant_id, resolved_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_incidents_tenant_id  ON incidents(tenant_id);
CREATE INDEX idx_incidents_station_id ON incidents(station_id);
CREATE INDEX idx_incidents_status     ON incidents(status);
CREATE INDEX idx_incidents_severity   ON incidents(severity);

CREATE TRIGGER incidents_set_updated_at
    BEFORE UPDATE ON incidents
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON incidents
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: both station-scoped. Reads ride station.read.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('pumps.calibrate',  'Record pump calibration events',        'station', true),
    ('incidents.manage', 'Open, transition, and resolve incidents', 'station', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND (
    (p.code = 'pumps.calibrate'  AND r.code IN ('system_admin', 'regional_manager', 'station_manager'))
    OR (p.code = 'incidents.manage' AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor'))
);
