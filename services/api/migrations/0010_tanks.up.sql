-- 0010_tanks: physical tank inventory bound to stations and products
-- (Phase 2, Stage 2).
--
-- A tank lives at one station and stores one product. Capacity limits feed
-- the loss/overfill engine in later phases. Composite (tenant_id, *) FKs
-- mirror 0008: a tank can only ever point at a station and a product that
-- belong to its own tenant — Postgres rejects any cross-tenant link.

CREATE TABLE tanks (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id         uuid NOT NULL,
    product_id         uuid NOT NULL,
    name               text NOT NULL,
    code               text NOT NULL,
    capacity_litres    numeric(14, 3) NOT NULL,
    safe_min_litres    numeric(14, 3) NOT NULL DEFAULT 0,
    safe_max_litres    numeric(14, 3) NOT NULL,
    dead_stock_litres  numeric(14, 3) NOT NULL DEFAULT 0,
    has_water_sensor   boolean NOT NULL DEFAULT false,
    has_temp_sensor    boolean NOT NULL DEFAULT false,
    status             text NOT NULL DEFAULT 'active',
    installation_date  date,
    decommission_date  date,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_tanks_status CHECK (
        status IN ('active', 'inactive', 'maintenance', 'decommissioned', 'deleted')
    ),
    CONSTRAINT chk_tanks_capacity CHECK (capacity_litres > 0),
    CONSTRAINT chk_tanks_dead_stock CHECK (dead_stock_litres >= 0),
    -- safe_min <= safe_max <= capacity.
    CONSTRAINT chk_tanks_safe_band CHECK (
        safe_min_litres <= safe_max_litres AND safe_max_litres <= capacity_litres
    ),

    -- Tenant-bound parents (FK targets are the composite uniques from 0008/0009).
    CONSTRAINT tanks_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT tanks_product_fk
        FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_tanks_tenant_id  ON tanks(tenant_id);
CREATE INDEX idx_tanks_station_id ON tanks(station_id);
CREATE INDEX idx_tanks_product_id ON tanks(product_id);
CREATE UNIQUE INDEX idx_tanks_station_code
    ON tanks(station_id, lower(code)) WHERE status <> 'deleted';

-- Composite tenant key + station/product-bearing key: FK targets for the
-- Stage 3 nozzles, which must prove nozzle.tank matches its station/product.
ALTER TABLE tanks ADD CONSTRAINT uq_tanks_tenant_id UNIQUE (tenant_id, id);
ALTER TABLE tanks ADD CONSTRAINT uq_tanks_tenant_station_product
    UNIQUE (tenant_id, id, station_id, product_id);

CREATE TRIGGER tanks_set_updated_at
    BEFORE UPDATE ON tanks
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Defense-in-depth tenant isolation, matching 0005_rls.
ALTER TABLE tanks ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tanks
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: tanks.manage is station-scoped. Reads ride station.read.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('tanks.manage', 'Create, edit, decommission tanks', 'station', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'tanks.manage'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager');
