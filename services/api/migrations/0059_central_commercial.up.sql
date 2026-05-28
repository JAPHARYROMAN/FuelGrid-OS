-- 0059_central_commercial: central pricing, central procurement planning, and
-- inter-station stock transfers (Phase 9, Stages 7-9). Central pricing produces
-- station-effective Phase-6 price_changes on activation; transfers post paired
-- Phase-4 'transfer' stock movements; procurement plans coordinate
-- station-scoped replenishment.

CREATE TABLE central_price_rollouts (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    product_id     uuid NOT NULL,
    scope_type     text NOT NULL DEFAULT 'tenant',
    scope_id       uuid,
    unit_price     numeric(14, 4) NOT NULL,
    effective_from date NOT NULL DEFAULT CURRENT_DATE,
    status         text NOT NULL DEFAULT 'draft',
    stations_applied integer NOT NULL DEFAULT 0,
    created_by     uuid NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_cpr_scope CHECK (scope_type IN ('tenant', 'region', 'station')),
    CONSTRAINT chk_cpr_status CHECK (status IN ('draft', 'pending_approval', 'approved', 'scheduled', 'active', 'superseded', 'cancelled')),
    CONSTRAINT cpr_product_fk FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT cpr_created_by_fk FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_cpr_tenant ON central_price_rollouts(tenant_id);
ALTER TABLE central_price_rollouts ADD CONSTRAINT uq_cpr_tenant_id UNIQUE (tenant_id, id);
CREATE TRIGGER central_price_rollouts_set_updated_at BEFORE UPDATE ON central_price_rollouts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE central_price_rollouts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON central_price_rollouts
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE central_procurement_plans (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    name       text NOT NULL,
    status     text NOT NULL DEFAULT 'draft',
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_cpp_status CHECK (status IN ('draft', 'reviewed', 'approved', 'released', 'closed')),
    CONSTRAINT cpp_created_by_fk FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_cpp_tenant ON central_procurement_plans(tenant_id);
ALTER TABLE central_procurement_plans ADD CONSTRAINT uq_cpp_tenant_id UNIQUE (tenant_id, id);
CREATE TRIGGER central_procurement_plans_set_updated_at BEFORE UPDATE ON central_procurement_plans FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE central_procurement_plans ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON central_procurement_plans
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE central_procurement_plan_lines (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    plan_id       uuid NOT NULL,
    station_id    uuid NOT NULL,
    product_id    uuid NOT NULL,
    target_litres numeric(14, 3) NOT NULL,
    released      boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT cppl_plan_fk FOREIGN KEY (tenant_id, plan_id) REFERENCES central_procurement_plans(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT cppl_station_fk FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT cppl_product_fk FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_cppl_tenant ON central_procurement_plan_lines(tenant_id);
CREATE INDEX idx_cppl_plan   ON central_procurement_plan_lines(plan_id);
ALTER TABLE central_procurement_plan_lines ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON central_procurement_plan_lines
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE stock_transfer_orders (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    from_tank_id    uuid NOT NULL,
    to_tank_id      uuid NOT NULL,
    product_id      uuid NOT NULL,
    litres          numeric(14, 3) NOT NULL,
    status          text NOT NULL DEFAULT 'draft',
    out_movement_id uuid,
    in_movement_id  uuid,
    created_by      uuid NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_sto_litres CHECK (litres > 0),
    CONSTRAINT chk_sto_status CHECK (status IN ('draft', 'approved', 'dispatched', 'received', 'cancelled')),
    CONSTRAINT chk_sto_distinct CHECK (from_tank_id <> to_tank_id),
    CONSTRAINT sto_from_tank_fk FOREIGN KEY (tenant_id, from_tank_id) REFERENCES tanks(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sto_to_tank_fk   FOREIGN KEY (tenant_id, to_tank_id) REFERENCES tanks(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sto_product_fk   FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sto_created_by_fk FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_sto_tenant ON stock_transfer_orders(tenant_id);
ALTER TABLE stock_transfer_orders ADD CONSTRAINT uq_sto_tenant_id UNIQUE (tenant_id, id);
CREATE TRIGGER stock_transfer_orders_set_updated_at BEFORE UPDATE ON stock_transfer_orders FOR EACH ROW EXECUTE FUNCTION set_updated_at();
ALTER TABLE stock_transfer_orders ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON stock_transfer_orders
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('central_pricing.manage',     'Manage central pricing rollouts',   'enterprise', false),
    ('central_pricing.approve',    'Approve central pricing rollouts',  'enterprise', false),
    ('central_pricing.publish',    'Publish central pricing to stations', 'enterprise', false),
    ('central_procurement.manage', 'Manage central procurement plans',  'enterprise', false),
    ('central_procurement.release','Release central procurement plans',  'enterprise', false),
    ('stock_transfer.manage',      'Manage inter-station stock transfers', 'enterprise', false),
    ('stock_transfer.approve',     'Approve stock transfers',           'enterprise', false),
    ('stock_transfer.receive',     'Receive stock transfers',           'enterprise', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('central_pricing.manage', 'central_pricing.approve', 'central_pricing.publish',
                                 'central_procurement.manage', 'central_procurement.release',
                                 'stock_transfer.manage', 'stock_transfer.approve', 'stock_transfer.receive')
  AND r.code IN ('system_admin', 'regional_manager', 'executive')
ON CONFLICT (role_id, permission_id) DO NOTHING;
