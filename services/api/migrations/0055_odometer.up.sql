-- 0055_odometer: fleet odometer capture + validation (Phase 8, Stages 10-11).
-- Readings are validated for monotonic increase; a non-increasing reading is
-- flagged 'warning' unless an explicit override is recorded. Consumption
-- reporting reads these alongside fulfilled authorizations.

CREATE TABLE vehicle_odometer_readings (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    vehicle_id        uuid NOT NULL,
    authorization_id  uuid,
    station_id        uuid,
    reading           numeric(14, 1) NOT NULL,
    distance_since    numeric(14, 1),
    validation_status text NOT NULL DEFAULT 'valid',
    note              text,
    recorded_by       uuid NOT NULL,
    captured_at       timestamptz NOT NULL DEFAULT now(),
    created_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_odometer_status CHECK (validation_status IN ('valid', 'warning', 'override')),
    CONSTRAINT odometer_vehicle_fk
        FOREIGN KEY (tenant_id, vehicle_id) REFERENCES customer_vehicles(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT odometer_recorded_by_fk
        FOREIGN KEY (tenant_id, recorded_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_odometer_tenant  ON vehicle_odometer_readings(tenant_id);
CREATE INDEX idx_odometer_vehicle ON vehicle_odometer_readings(vehicle_id, captured_at);

ALTER TABLE vehicle_odometer_readings ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON vehicle_odometer_readings
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: fleet_report.read (consumption reports; customer-scoped views).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('fleet_report.read', 'View fleet consumption reports', 'fleet', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'fleet_report.read'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'finance_officer', 'executive', 'auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
