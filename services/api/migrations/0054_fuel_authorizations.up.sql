-- 0054_fuel_authorizations: the controlled path from credential to billable
-- fuel (Phase 8, Stages 7-8). A fuel authorization is a permission to fuel, not
-- a sale; it is consumed (fulfilled) at most once by a Phase-6 credit sale.
-- Limits cap amount/litres per scope (the strictest applicable wins); denials
-- record exactly which rule blocked a request.

CREATE TABLE fuel_limits (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id uuid,
    vehicle_id  uuid,
    product_id  uuid,
    scope       text NOT NULL DEFAULT 'transaction',
    period      text NOT NULL DEFAULT 'transaction',
    max_amount  numeric(14, 2),
    max_litres  numeric(14, 3),
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_fuel_limit_period CHECK (period IN ('transaction', 'day', 'week', 'month')),
    CONSTRAINT fuel_limit_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT fuel_limit_vehicle_fk
        FOREIGN KEY (tenant_id, vehicle_id) REFERENCES customer_vehicles(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_fuel_limits_tenant   ON fuel_limits(tenant_id);
CREATE INDEX idx_fuel_limits_customer ON fuel_limits(customer_id);

ALTER TABLE fuel_limits ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fuel_limits
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE fuel_authorizations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id     uuid NOT NULL,
    vehicle_id      uuid,
    driver_id       uuid,
    credential_id   uuid,
    station_id      uuid NOT NULL,
    product_id      uuid,
    requested_amount numeric(14, 2) NOT NULL DEFAULT 0,
    approved_amount  numeric(14, 2) NOT NULL DEFAULT 0,
    odometer        numeric(14, 1),
    status          text NOT NULL DEFAULT 'approved',
    expiry_at       timestamptz,
    source          text NOT NULL DEFAULT 'forecourt',
    consumed_by     uuid,
    created_by      uuid NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_fuel_auth_status
        CHECK (status IN ('requested', 'approved', 'fulfilled', 'expired', 'cancelled', 'voided')),
    CONSTRAINT fuel_auth_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT fuel_auth_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT fuel_auth_vehicle_fk
        FOREIGN KEY (tenant_id, vehicle_id) REFERENCES customer_vehicles(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT fuel_auth_driver_fk
        FOREIGN KEY (tenant_id, driver_id) REFERENCES customer_drivers(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT fuel_auth_credential_fk
        FOREIGN KEY (tenant_id, credential_id) REFERENCES fuel_credentials(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT fuel_auth_created_by_fk
        FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

-- One authorization can be consumed by at most one sale.
CREATE UNIQUE INDEX uq_fuel_auth_consumed ON fuel_authorizations(tenant_id, consumed_by) WHERE consumed_by IS NOT NULL;
CREATE INDEX idx_fuel_auth_tenant   ON fuel_authorizations(tenant_id);
CREATE INDEX idx_fuel_auth_customer ON fuel_authorizations(customer_id);
CREATE INDEX idx_fuel_auth_status   ON fuel_authorizations(tenant_id, status);
ALTER TABLE fuel_authorizations ADD CONSTRAINT uq_fuel_auth_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER fuel_authorizations_set_updated_at
    BEFORE UPDATE ON fuel_authorizations FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE fuel_authorizations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fuel_authorizations
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE fuel_authorization_denials (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id      uuid,
    station_id       uuid,
    rule_code        text NOT NULL,
    detail           text,
    requested_amount numeric(14, 2),
    override_attempted boolean NOT NULL DEFAULT false,
    actor_id         uuid,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_fuel_denials_tenant ON fuel_authorization_denials(tenant_id);

ALTER TABLE fuel_authorization_denials ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fuel_authorization_denials
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: fuel_authorization.create / .cancel / .override.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('fuel_authorization.create',   'Request and approve fuel authorizations', 'fleet', false),
    ('fuel_authorization.cancel',   'Cancel or void fuel authorizations',      'fleet', false),
    ('fuel_authorization.override', 'Override a denied authorization',          'fleet', false),
    ('fuel_limit.manage',           'Manage fuel limits',                       'fleet', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('fuel_authorization.create', 'fuel_authorization.cancel', 'fuel_authorization.override', 'fuel_limit.manage')
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
