-- 0012_tank_calibration: dip-to-volume strapping charts (Phase 2, Stage 4).
--
-- A calibration chart maps dipstick millimetres to litres for one tank.
-- Charts are versioned: a re-strap supersedes the previous chart rather
-- than overwriting it, so history is preserved. Entries are sparse points;
-- the API linearly interpolates between the two surrounding rows.

-- ---------------------------------------------------------------------------
-- tank_calibration_charts
-- ---------------------------------------------------------------------------
CREATE TABLE tank_calibration_charts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    tank_id         uuid NOT NULL,
    name            text NOT NULL,
    effective_from  timestamptz NOT NULL DEFAULT now(),
    effective_until timestamptz,
    status          text NOT NULL DEFAULT 'active',
    source          text NOT NULL DEFAULT 'csv_upload',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_tcc_status CHECK (status IN ('active', 'superseded')),

    CONSTRAINT tcc_tank_fk
        FOREIGN KEY (tenant_id, tank_id) REFERENCES tanks(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_tcc_tenant_id ON tank_calibration_charts(tenant_id);
CREATE INDEX idx_tcc_tank_id   ON tank_calibration_charts(tank_id);

-- At most one active chart per tank at any time.
CREATE UNIQUE INDEX idx_tcc_one_active
    ON tank_calibration_charts(tank_id) WHERE status = 'active';

CREATE TRIGGER tcc_set_updated_at
    BEFORE UPDATE ON tank_calibration_charts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE tank_calibration_charts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tank_calibration_charts
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- tank_calibration_entries — sparse (dip_mm -> volume_litres) points.
--
-- Deliberately carries no tenant_id: like role_permissions in 0005, its
-- isolation comes from its parent. Every read goes through a chart_id that
-- has already been tenant-scoped, and the FK + cascade tie each entry to a
-- chart inside one tenant. So no RLS policy here.
-- ---------------------------------------------------------------------------
CREATE TABLE tank_calibration_entries (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    chart_id      uuid NOT NULL REFERENCES tank_calibration_charts(id) ON DELETE CASCADE,
    dip_mm        integer NOT NULL,
    volume_litres numeric(14, 3) NOT NULL,

    CONSTRAINT chk_tce_dip    CHECK (dip_mm >= 0),
    CONSTRAINT chk_tce_volume CHECK (volume_litres >= 0),
    CONSTRAINT uq_tce_chart_dip UNIQUE (chart_id, dip_mm)
);

CREATE INDEX idx_tce_chart_id ON tank_calibration_entries(chart_id);

-- ---------------------------------------------------------------------------
-- Permission: tanks.calibrate is station-scoped. Reads ride station.read.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('tanks.calibrate', 'Upload and replace tank calibration charts', 'station', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'tanks.calibrate'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager');
