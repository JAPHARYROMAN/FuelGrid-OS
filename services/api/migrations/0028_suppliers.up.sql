-- 0028_suppliers: tenant-wide supplier master (Phase 5, Stage 1).
--
-- Suppliers are catalogue data: a vendor may serve many stations, and every
-- purchase order / receipt / invoice links back here. Product coverage is a
-- join table so product ids remain tenant-bound by composite FK rather than
-- being hidden in JSON.

CREATE TABLE suppliers (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    code               text NOT NULL,
    name               text NOT NULL,
    contact_name       text,
    contact_email      text,
    contact_phone      text,
    payment_terms_days integer NOT NULL DEFAULT 0,
    status             text NOT NULL DEFAULT 'active',
    deactivated_at     timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_suppliers_status CHECK (status IN ('active', 'inactive', 'deactivated')),
    CONSTRAINT chk_suppliers_payment_terms CHECK (payment_terms_days >= 0)
);

CREATE INDEX idx_suppliers_tenant_id ON suppliers(tenant_id);
CREATE UNIQUE INDEX idx_suppliers_tenant_code
    ON suppliers(tenant_id, lower(code));

ALTER TABLE suppliers ADD CONSTRAINT uq_suppliers_tenant_id UNIQUE (tenant_id, id);

CREATE TABLE supplier_products (
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    supplier_id uuid NOT NULL,
    product_id  uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (supplier_id, product_id),
    CONSTRAINT supplier_products_supplier_fk
        FOREIGN KEY (tenant_id, supplier_id) REFERENCES suppliers(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT supplier_products_product_fk
        FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_supplier_products_tenant_id ON supplier_products(tenant_id);
CREATE INDEX idx_supplier_products_product_id ON supplier_products(product_id);

CREATE TRIGGER suppliers_set_updated_at
    BEFORE UPDATE ON suppliers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE suppliers ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON suppliers
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE supplier_products ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supplier_products
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Supplier writes are tenant-wide. Reads ride purchase_order.read once the PO
-- migration introduces that permission.
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('supplier.manage', 'Create, edit, and deactivate suppliers', 'procurement', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'supplier.manage'
  AND r.code IN ('system_admin', 'procurement_officer', 'executive');
