-- 0025_deliveries: minimal fuel delivery intake (Phase 4, Stage 3).
--
-- A delivery records that fuel arrived into a tank and how much. Receiving
-- one posts a +volume 'delivery' movement to the stock ledger (0024) in the
-- same transaction, so book stock reflects every replenishment. The optional
-- dip-before/dip-after let the operator cross-check the declared volume
-- against the measured level change; dip_variance_litres snapshots
-- declared − (after − before) at receive time.
--
-- This is intake ONLY — supplier master data, purchase orders, GRN matching
-- and delivery pricing are the later supply-chain phase. supplier_ref is
-- free text for now.

CREATE TABLE deliveries (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    tank_id             uuid NOT NULL,
    supplier_ref        text,
    volume_litres       numeric(14, 3) NOT NULL,
    dip_before_litres   numeric(14, 3),
    dip_after_litres    numeric(14, 3),
    dip_variance_litres numeric(14, 3),
    received_by         uuid NOT NULL,
    received_at         timestamptz NOT NULL DEFAULT now(),
    notes               text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_deliveries_volume      CHECK (volume_litres > 0),
    CONSTRAINT chk_deliveries_dip_before  CHECK (dip_before_litres IS NULL OR dip_before_litres >= 0),
    CONSTRAINT chk_deliveries_dip_after   CHECK (dip_after_litres  IS NULL OR dip_after_litres  >= 0),

    CONSTRAINT deliveries_tank_fk
        FOREIGN KEY (tenant_id, tank_id) REFERENCES tanks(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT deliveries_received_by_fk
        FOREIGN KEY (tenant_id, received_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_deliveries_tenant_id ON deliveries(tenant_id);
CREATE INDEX idx_deliveries_tank_time ON deliveries(tank_id, received_at);

ALTER TABLE deliveries ADD CONSTRAINT uq_deliveries_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER deliveries_set_updated_at
    BEFORE UPDATE ON deliveries
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE deliveries ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON deliveries
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: delivery.receive is station-scoped. Reads ride inventory.read
-- (0024).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('delivery.receive', 'Receive fuel deliveries into a tank', 'inventory', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'delivery.receive'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
