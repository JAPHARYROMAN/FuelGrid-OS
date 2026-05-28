-- 0027_tank_reconciliations: book-vs-physical reconciliation per tank per
-- operating day (Phase 4, Stages 5-6).
--
-- One reconciliation per (tank, operating_day). It freezes the book figure
-- (computed from the ledger), the physical figure (from the closing dip), and
-- the variance between them, classified against the product's loss tolerance.
--
-- Lifecycle (status):
--   draft     — computed/persisted, variance within tolerance, sealable
--   exception — variance over tolerance, blocks sealing until adjusted down
--   sealed    — frozen and signed off; its closing_physical carries forward as
--               the next period's opening book (the trust anchor)
--
-- through_seq is the stock-ledger watermark this reconciliation covers: the
-- next period sums movements with seq > through_seq. At seal a variance
-- write-off adjustment reconciles the ledger to the physical figure, so the
-- ledger never drifts from the sealed anchor.

CREATE TABLE tank_reconciliations (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    tank_id           uuid NOT NULL,
    operating_day_id  uuid NOT NULL,
    opening_book      numeric(14, 3) NOT NULL,
    deliveries_total  numeric(14, 3) NOT NULL,
    sales_total       numeric(14, 3) NOT NULL,
    adjustments_total numeric(14, 3) NOT NULL,
    closing_book      numeric(14, 3) NOT NULL,
    closing_physical  numeric(14, 3) NOT NULL,
    variance_litres   numeric(14, 3) NOT NULL,
    variance_percent  numeric(10, 4) NOT NULL,
    tolerance_percent numeric(5, 2)  NOT NULL,
    through_seq       bigint NOT NULL DEFAULT 0,
    status            text NOT NULL DEFAULT 'draft',
    sealed_by         uuid,
    sealed_at         timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_tank_recon_status CHECK (status IN ('draft', 'exception', 'sealed')),

    CONSTRAINT tank_recon_tank_fk
        FOREIGN KEY (tenant_id, tank_id) REFERENCES tanks(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT tank_recon_day_fk
        FOREIGN KEY (tenant_id, operating_day_id) REFERENCES operating_days(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT tank_recon_sealed_by_fk
        FOREIGN KEY (tenant_id, sealed_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,

    -- One reconciliation per tank per operating day.
    CONSTRAINT uq_tank_recon_tank_day UNIQUE (tank_id, operating_day_id)
);

CREATE INDEX idx_tank_recon_tenant_id ON tank_reconciliations(tenant_id);
CREATE INDEX idx_tank_recon_tank_id   ON tank_reconciliations(tank_id);
CREATE INDEX idx_tank_recon_day_id    ON tank_reconciliations(operating_day_id);
-- Find a tank's most recent sealed reconciliation (the balance-forward anchor).
CREATE INDEX idx_tank_recon_sealed
    ON tank_reconciliations(tank_id, through_seq DESC) WHERE status = 'sealed';

ALTER TABLE tank_reconciliations ADD CONSTRAINT uq_tank_recon_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER tank_reconciliations_set_updated_at
    BEFORE UPDATE ON tank_reconciliations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE tank_reconciliations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tank_reconciliations
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: reconciliation.read (view) and reconciliation.manage
-- (run/adjust/seal), both station-scoped.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('reconciliation.read',   'View tank reconciliations and variances', 'inventory', true),
    ('reconciliation.manage', 'Run, adjust, and seal reconciliations',   'inventory', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'reconciliation.read'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor', 'executive', 'auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'reconciliation.manage'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
