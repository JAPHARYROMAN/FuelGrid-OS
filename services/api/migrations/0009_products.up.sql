-- 0009_products: the per-tenant product catalogue (Phase 2, Stage 1).
--
-- Products are the vocabulary the rest of the fuel OS references by id:
-- tanks bind to a product, nozzles dispense one, the stock ledger meters
-- one. Stage 2's tanks FK onto (tenant_id, id) here, so this migration
-- adds the composite tenant key up front (same pattern as 0008).
--
-- Visual identity: every product carries a `color` hex the UI binds to.
-- The three seeded fuels reuse the --color-fuel-* tokens from
-- packages/config/tailwind.preset.css (PMS orange, AGO blue, Kerosene
-- purple).

CREATE TABLE products (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    code                  text NOT NULL,
    name                  text NOT NULL,
    category              text NOT NULL DEFAULT 'fuel',
    unit                  text NOT NULL DEFAULT 'litre',
    default_price         numeric(14, 2) NOT NULL DEFAULT 0,
    tax_rate              numeric(5, 2)  NOT NULL DEFAULT 0,
    density_kg_m3         numeric(10, 3),
    loss_tolerance_percent numeric(5, 2) NOT NULL DEFAULT 0,
    color                 text NOT NULL DEFAULT '#64748b',
    status                text NOT NULL DEFAULT 'active',
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_products_status   CHECK (status IN ('active', 'inactive', 'deleted')),
    CONSTRAINT chk_products_category CHECK (category IN ('fuel', 'gas', 'lubricant', 'additive', 'other')),
    CONSTRAINT chk_products_unit     CHECK (unit IN ('litre', 'kg', 'unit')),
    CONSTRAINT chk_products_color    CHECK (color ~ '^#[0-9A-Fa-f]{6}$'),
    CONSTRAINT chk_products_tax_rate CHECK (tax_rate BETWEEN 0 AND 100),
    CONSTRAINT chk_products_loss_tol CHECK (loss_tolerance_percent >= 0),
    CONSTRAINT chk_products_density  CHECK (density_kg_m3 IS NULL OR density_kg_m3 > 0)
);

CREATE INDEX idx_products_tenant_id ON products(tenant_id);
CREATE UNIQUE INDEX idx_products_tenant_code
    ON products(tenant_id, lower(code)) WHERE status <> 'deleted';

-- Composite tenant key: FK target for Stage 2 tanks (tenant_id, product_id).
ALTER TABLE products ADD CONSTRAINT uq_products_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER products_set_updated_at
    BEFORE UPDATE ON products
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Defense-in-depth tenant isolation, matching 0005_rls.
ALTER TABLE products ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON products
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: products.manage is tenant-wide (not station-scoped). Reads
-- ride the existing station.read grant every operating role already holds.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('products.manage', 'Create, edit, soft-delete products', 'admin', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'products.manage' AND r.code IN ('system_admin', 'executive');
