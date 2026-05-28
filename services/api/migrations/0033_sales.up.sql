-- 0033_sales: recognized priced sales (Phase 6, Stages 3-4).
--
-- When a shift is approved, each nozzle's metered litres-sold (frozen in
-- shift_close_lines) are valued at the resolved selling price into one sale
-- row, split into net + tax (the selling price is treated as tax-inclusive),
-- and valued at the tank's moving-average landed cost for COGS and margin.
-- Price, tax rate, and unit cost are SNAPSHOTTED so a later change never
-- rewrites recognized revenue. One sale per (shift, nozzle) — idempotent on
-- re-approval, like the Phase-4 sales movement.

CREATE TABLE sales (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    shift_id         uuid NOT NULL,
    station_id       uuid NOT NULL,
    operating_day_id uuid NOT NULL,
    nozzle_id        uuid NOT NULL,
    product_id       uuid NOT NULL,
    tank_id          uuid NOT NULL,
    litres           numeric(14, 3) NOT NULL,
    unit_price       numeric(14, 4) NOT NULL,
    gross_amount     numeric(14, 2) NOT NULL,
    tax_rate         numeric(5, 2)  NOT NULL,
    tax_amount       numeric(14, 2) NOT NULL,
    net_amount       numeric(14, 2) NOT NULL,
    unit_cost        numeric(14, 4),
    cogs_amount      numeric(14, 2),
    margin_amount    numeric(14, 2),
    recorded_by      uuid NOT NULL,
    recorded_at      timestamptz NOT NULL DEFAULT now(),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT sales_shift_fk
        FOREIGN KEY (tenant_id, shift_id) REFERENCES shifts(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sales_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sales_day_fk
        FOREIGN KEY (tenant_id, operating_day_id) REFERENCES operating_days(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sales_nozzle_fk
        FOREIGN KEY (tenant_id, nozzle_id) REFERENCES nozzles(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sales_product_fk
        FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sales_tank_fk
        FOREIGN KEY (tenant_id, tank_id) REFERENCES tanks(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sales_recorded_by_fk
        FOREIGN KEY (tenant_id, recorded_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,

    -- One recognized sale per nozzle per shift — the idempotency key.
    CONSTRAINT uq_sales_shift_nozzle UNIQUE (shift_id, nozzle_id)
);

CREATE INDEX idx_sales_tenant_id ON sales(tenant_id);
CREATE INDEX idx_sales_shift_id  ON sales(shift_id);
CREATE INDEX idx_sales_station_day ON sales(station_id, operating_day_id);
CREATE INDEX idx_sales_product   ON sales(product_id);

ALTER TABLE sales ADD CONSTRAINT uq_sales_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER sales_set_updated_at
    BEFORE UPDATE ON sales
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE sales ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sales
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: revenue.read (station-scoped). Margin/cost reads ride the
-- existing margin.view (0004).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('revenue.read', 'View recognized sales and revenue', 'finance', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'revenue.read'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor',
                 'finance_officer', 'executive', 'auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
