-- 0032_price_changes: effective-dated selling price book (Phase 6, Stages 1-2).
--
-- The authoritative per-station, per-product SELLING price (Phase 5 priced the
-- inflow; this prices the outflow). Append-only and effective-dated, mirroring
-- the Phase-2 calibration-chart versioning: the active price for (station,
-- product) is the latest row with effective_from <= now. A future
-- effective_from schedules a change that activates automatically when its time
-- arrives — resolution is time-based, no cron.
--
-- unit_price is per-litre numeric(14, 4); a sale snapshots it so a later change
-- never rewrites recognized revenue (Phase 6 Stage 3).

CREATE TABLE price_changes (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id     uuid NOT NULL,
    product_id     uuid NOT NULL,
    unit_price     numeric(14, 4) NOT NULL,
    effective_from timestamptz NOT NULL DEFAULT now(),
    previous_price numeric(14, 4),
    reason         text,
    set_by         uuid NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_price_changes_price CHECK (unit_price >= 0),

    CONSTRAINT price_changes_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT price_changes_product_fk
        FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT price_changes_set_by_fk
        FOREIGN KEY (tenant_id, set_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_price_changes_tenant_id ON price_changes(tenant_id);
-- Active-price resolution: latest effective_from per (station, product).
CREATE INDEX idx_price_changes_resolve
    ON price_changes(station_id, product_id, effective_from DESC);

ALTER TABLE price_changes ADD CONSTRAINT uq_price_changes_tenant_id UNIQUE (tenant_id, id);

ALTER TABLE price_changes ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON price_changes
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: pricing.read (view the price board / history) is station-scoped.
-- Writes ride the existing price.change (0004); grant it to station managers
-- too (0004 gave it to regional_manager + system_admin only).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('pricing.read', 'View selling prices and price history', 'pricing', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'pricing.read'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor',
                 'attendant', 'finance_officer', 'executive', 'auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'price.change' AND r.code = 'station_manager'
ON CONFLICT (role_id, permission_id) DO NOTHING;
