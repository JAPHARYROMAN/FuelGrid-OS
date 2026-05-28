-- 0011_pumps_nozzles: the dispensing layer (Phase 2, Stage 3).
--
-- A pump sits at one station. A nozzle belongs to one pump, pulls from one
-- tank, and dispenses that tank's product. Two invariants must hold and are
-- enforced *at the database layer*, not just in app code:
--
--   1. nozzle.product_id = tank.product_id
--   2. nozzle.station_id = pump.station_id = tank.station_id
--
-- Rather than triggers, this rides composite foreign keys (same technique as
-- 0008). Stage 2 already exposed tanks(tenant_id, id, station_id, product_id)
-- as a unique key; here we expose pumps(tenant_id, id, station_id) too, then
-- point the nozzle's FKs at both. Because nozzle.station_id appears in both
-- FKs, the pump and tank are forced to share it; because product_id rides the
-- tank FK, the nozzle's product is forced to match the tank's.

-- ---------------------------------------------------------------------------
-- pumps
-- ---------------------------------------------------------------------------
CREATE TABLE pumps (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id        uuid NOT NULL,
    number            integer NOT NULL,
    name              text,
    manufacturer      text,
    model             text,
    serial_number     text,
    status            text NOT NULL DEFAULT 'active',
    installation_date date,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_pumps_status CHECK (
        status IN ('active', 'inactive', 'maintenance', 'decommissioned', 'deleted')
    ),
    CONSTRAINT chk_pumps_number CHECK (number > 0),

    CONSTRAINT pumps_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_pumps_tenant_id  ON pumps(tenant_id);
CREATE INDEX idx_pumps_station_id ON pumps(station_id);
CREATE UNIQUE INDEX idx_pumps_station_number
    ON pumps(station_id, number) WHERE status <> 'deleted';

-- FK targets for nozzles: the tenant key, and the station-bearing key that
-- forces a nozzle to share its pump's station.
ALTER TABLE pumps ADD CONSTRAINT uq_pumps_tenant_id UNIQUE (tenant_id, id);
ALTER TABLE pumps ADD CONSTRAINT uq_pumps_tenant_station UNIQUE (tenant_id, id, station_id);

CREATE TRIGGER pumps_set_updated_at
    BEFORE UPDATE ON pumps
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE pumps ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pumps
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- nozzles
-- ---------------------------------------------------------------------------
CREATE TABLE nozzles (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id           uuid NOT NULL,
    pump_id              uuid NOT NULL,
    tank_id              uuid NOT NULL,
    product_id           uuid NOT NULL,
    number               integer NOT NULL,
    default_price        numeric(14, 2) NOT NULL DEFAULT 0,
    meter_decimal_places integer NOT NULL DEFAULT 2,
    status               text NOT NULL DEFAULT 'active',
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_nozzles_status CHECK (
        status IN ('active', 'inactive', 'maintenance', 'decommissioned', 'deleted')
    ),
    CONSTRAINT chk_nozzles_number CHECK (number > 0),
    CONSTRAINT chk_nozzles_meter_dp CHECK (meter_decimal_places BETWEEN 0 AND 4),

    -- Same station as the pump.
    CONSTRAINT nozzles_pump_fk
        FOREIGN KEY (tenant_id, pump_id, station_id)
        REFERENCES pumps(tenant_id, id, station_id) ON DELETE RESTRICT,

    -- Same station AND same product as the tank.
    CONSTRAINT nozzles_tank_fk
        FOREIGN KEY (tenant_id, tank_id, station_id, product_id)
        REFERENCES tanks(tenant_id, id, station_id, product_id) ON DELETE RESTRICT
);

CREATE INDEX idx_nozzles_tenant_id  ON nozzles(tenant_id);
CREATE INDEX idx_nozzles_station_id ON nozzles(station_id);
CREATE INDEX idx_nozzles_pump_id    ON nozzles(pump_id);
CREATE INDEX idx_nozzles_tank_id    ON nozzles(tank_id);
CREATE INDEX idx_nozzles_product_id ON nozzles(product_id);
CREATE UNIQUE INDEX idx_nozzles_pump_number
    ON nozzles(pump_id, number) WHERE status <> 'deleted';

CREATE TRIGGER nozzles_set_updated_at
    BEFORE UPDATE ON nozzles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE nozzles ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nozzles
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: pumps.manage is station-scoped. Nozzle mutations fold into it
-- for now (no separate nozzles.manage). Reads ride station.read.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('pumps.manage', 'Create, edit, decommission pumps and nozzles', 'station', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'pumps.manage'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager');
