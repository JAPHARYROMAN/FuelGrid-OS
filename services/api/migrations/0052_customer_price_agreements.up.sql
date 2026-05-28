-- 0052_customer_price_agreements: controlled fleet/credit pricing (Phase 8,
-- Stage 3). A credit sale resolves its unit price from the customer's active
-- agreement first, then falls back to the Phase-6 configured retail price. The
-- applied price is snapshotted on the sale (Stage 9), so historical sales never
-- change when an agreement changes.

CREATE TABLE customer_price_agreements (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id    uuid NOT NULL,
    product_id     uuid NOT NULL,
    station_id     uuid,
    price_type     text NOT NULL DEFAULT 'fixed',
    fixed_price    numeric(14, 4),
    discount       numeric(14, 4),
    markup         numeric(14, 4),
    effective_from date NOT NULL DEFAULT CURRENT_DATE,
    effective_to   date,
    status         text NOT NULL DEFAULT 'draft',
    version        integer NOT NULL DEFAULT 1,
    approved_by    uuid,
    created_by     uuid NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_cpa_price_type CHECK (price_type IN ('fixed', 'discount', 'markup')),
    CONSTRAINT chk_cpa_status
        CHECK (status IN ('draft', 'approved', 'active', 'expired', 'cancelled')),
    CONSTRAINT cpa_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT cpa_product_fk
        FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT cpa_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT cpa_created_by_fk
        FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

-- At most one active agreement per customer/product/station scope. The NULL
-- station scope (tenant-wide for the customer/product) is distinguished from
-- station-specific scopes via two partial indexes.
CREATE UNIQUE INDEX uq_cpagr_active_scoped
    ON customer_price_agreements(tenant_id, customer_id, product_id, station_id)
    WHERE status = 'active' AND station_id IS NOT NULL;
CREATE UNIQUE INDEX uq_cpagr_active_tenantwide
    ON customer_price_agreements(tenant_id, customer_id, product_id)
    WHERE status = 'active' AND station_id IS NULL;
CREATE INDEX idx_cpagr_tenant   ON customer_price_agreements(tenant_id);
CREATE INDEX idx_cpagr_customer ON customer_price_agreements(customer_id);
ALTER TABLE customer_price_agreements ADD CONSTRAINT uq_cpagr_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER customer_price_agreements_set_updated_at
    BEFORE UPDATE ON customer_price_agreements
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE customer_price_agreements ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_price_agreements
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: customer_pricing.manage / .approve (tenant-wide).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('customer_pricing.manage',  'Create and manage customer price agreements', 'pricing', false),
    ('customer_pricing.approve', 'Approve and activate customer price agreements', 'pricing', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('customer_pricing.manage', 'customer_pricing.approve')
  AND r.code IN ('system_admin', 'regional_manager', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
