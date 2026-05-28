-- 0053_fleet_identity: the vehicles, drivers, and credentials that identify who
-- may fuel (Phase 8, Stages 4-6). Credentials and PINs store only salted hashes
-- and a display-safe mask; raw tokens are never persisted or logged.

CREATE TABLE customer_vehicles (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id        uuid NOT NULL,
    registration       text NOT NULL,
    fleet_number       text,
    vin                text,
    vehicle_type       text,
    default_product_id uuid,
    tank_capacity      numeric(14, 3),
    odometer_required  boolean NOT NULL DEFAULT false,
    status             text NOT NULL DEFAULT 'active',
    metadata           jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_vehicle_status CHECK (status IN ('active', 'on_hold', 'retired')),
    CONSTRAINT vehicle_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT vehicle_product_fk
        FOREIGN KEY (tenant_id, default_product_id) REFERENCES products(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_vehicles_tenant   ON customer_vehicles(tenant_id);
CREATE INDEX idx_vehicles_customer ON customer_vehicles(customer_id);
CREATE UNIQUE INDEX uq_vehicles_registration ON customer_vehicles(tenant_id, lower(registration)) WHERE status <> 'retired';
ALTER TABLE customer_vehicles ADD CONSTRAINT uq_vehicles_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER customer_vehicles_set_updated_at
    BEFORE UPDATE ON customer_vehicles FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE customer_vehicles ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_vehicles
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE customer_drivers (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id         uuid NOT NULL,
    name                text NOT NULL,
    phone               text,
    license_number      text,
    pin_hash            text,
    status              text NOT NULL DEFAULT 'active',
    allowed_product_ids uuid[] NOT NULL DEFAULT '{}',
    assignment_rule     text NOT NULL DEFAULT 'any',
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_driver_status CHECK (status IN ('active', 'on_hold', 'inactive')),
    CONSTRAINT chk_driver_assignment CHECK (assignment_rule IN ('any', 'assigned', 'primary')),
    CONSTRAINT driver_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_drivers_tenant   ON customer_drivers(tenant_id);
CREATE INDEX idx_drivers_customer ON customer_drivers(customer_id);
ALTER TABLE customer_drivers ADD CONSTRAINT uq_drivers_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER customer_drivers_set_updated_at
    BEFORE UPDATE ON customer_drivers FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE customer_drivers ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_drivers
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE fuel_credentials (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id     uuid NOT NULL,
    vehicle_id      uuid,
    driver_id       uuid,
    credential_type text NOT NULL,
    token_hash      text NOT NULL,
    masked_label    text NOT NULL,
    status          text NOT NULL DEFAULT 'active',
    issued_at       date NOT NULL DEFAULT CURRENT_DATE,
    expiry_date     date,
    last_used_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_credential_type CHECK (credential_type IN ('card', 'qr', 'rfid', 'manual_code')),
    CONSTRAINT chk_credential_status CHECK (status IN ('issued', 'active', 'suspended', 'expired', 'revoked')),
    CONSTRAINT credential_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT credential_vehicle_fk
        FOREIGN KEY (tenant_id, vehicle_id) REFERENCES customer_vehicles(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT credential_driver_fk
        FOREIGN KEY (tenant_id, driver_id) REFERENCES customer_drivers(tenant_id, id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX uq_credential_token ON fuel_credentials(tenant_id, token_hash);
CREATE INDEX idx_credentials_tenant   ON fuel_credentials(tenant_id);
CREATE INDEX idx_credentials_customer ON fuel_credentials(customer_id);
ALTER TABLE fuel_credentials ADD CONSTRAINT uq_credentials_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER fuel_credentials_set_updated_at
    BEFORE UPDATE ON fuel_credentials FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE fuel_credentials ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fuel_credentials
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions. Vehicles/drivers ride customer.manage; credentials get their
-- own issue/manage/revoke permissions.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('fuel_credential.manage', 'Manage fuel credentials',  'fleet', false),
    ('fuel_credential.issue',  'Issue fuel credentials',   'fleet', false),
    ('fuel_credential.revoke', 'Revoke fuel credentials',  'fleet', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('fuel_credential.manage', 'fuel_credential.issue', 'fuel_credential.revoke')
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager')
ON CONFLICT (role_id, permission_id) DO NOTHING;
