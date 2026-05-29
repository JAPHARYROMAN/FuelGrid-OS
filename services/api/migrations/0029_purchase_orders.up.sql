-- 0029_purchase_orders: station-scoped ordering workflow (Phase 5, Stage 2).
--
-- A purchase order belongs to exactly one station and one supplier. Lines
-- carry the product, ordered litres, agreed unit price, and cumulative
-- received litres so goods receipts can advance the PO lifecycle without
-- mutating historical receipt rows.

CREATE TABLE purchase_orders (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id              uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id             uuid NOT NULL,
    supplier_id            uuid NOT NULL,
    expected_delivery_date date,
    status                 text NOT NULL DEFAULT 'draft',
    raised_by              uuid NOT NULL,
    submitted_by           uuid,
    submitted_at           timestamptz,
    confirmed_by           uuid,
    confirmed_at           timestamptz,
    cancelled_by           uuid,
    cancelled_at           timestamptz,
    closed_by              uuid,
    closed_at              timestamptz,
    notes                  text,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_purchase_orders_status CHECK (
        status IN ('draft', 'submitted', 'confirmed', 'partially_received', 'received', 'closed', 'cancelled')
    ),
    CONSTRAINT purchase_orders_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT purchase_orders_supplier_fk
        FOREIGN KEY (tenant_id, supplier_id) REFERENCES suppliers(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT purchase_orders_raised_by_fk
        FOREIGN KEY (tenant_id, raised_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT purchase_orders_submitted_by_fk
        FOREIGN KEY (tenant_id, submitted_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT purchase_orders_confirmed_by_fk
        FOREIGN KEY (tenant_id, confirmed_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT purchase_orders_cancelled_by_fk
        FOREIGN KEY (tenant_id, cancelled_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT purchase_orders_closed_by_fk
        FOREIGN KEY (tenant_id, closed_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_purchase_orders_tenant_id ON purchase_orders(tenant_id);
CREATE INDEX idx_purchase_orders_station_status ON purchase_orders(station_id, status);
CREATE INDEX idx_purchase_orders_supplier_status ON purchase_orders(supplier_id, status);
ALTER TABLE purchase_orders ADD CONSTRAINT uq_purchase_orders_tenant_id UNIQUE (tenant_id, id);

CREATE TABLE purchase_order_lines (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    purchase_order_id uuid NOT NULL,
    product_id        uuid NOT NULL,
    ordered_litres    numeric(14, 3) NOT NULL,
    unit_price        numeric(14, 2) NOT NULL,
    received_litres   numeric(14, 3) NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_po_lines_ordered_litres CHECK (ordered_litres > 0),
    CONSTRAINT chk_po_lines_unit_price CHECK (unit_price >= 0),
    CONSTRAINT chk_po_lines_received_litres CHECK (received_litres >= 0),
    CONSTRAINT po_lines_order_fk
        FOREIGN KEY (tenant_id, purchase_order_id) REFERENCES purchase_orders(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT po_lines_product_fk
        FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_po_lines_tenant_id ON purchase_order_lines(tenant_id);
CREATE INDEX idx_po_lines_order_id ON purchase_order_lines(purchase_order_id);
CREATE INDEX idx_po_lines_product_id ON purchase_order_lines(product_id);
ALTER TABLE purchase_order_lines ADD CONSTRAINT uq_po_lines_tenant_id UNIQUE (tenant_id, id);
ALTER TABLE purchase_order_lines ADD CONSTRAINT uq_po_lines_tenant_order_id UNIQUE (tenant_id, purchase_order_id, id);

CREATE TRIGGER purchase_orders_set_updated_at
    BEFORE UPDATE ON purchase_orders
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER purchase_order_lines_set_updated_at
    BEFORE UPDATE ON purchase_order_lines
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE purchase_orders ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON purchase_orders
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE purchase_order_lines ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON purchase_order_lines
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('purchase_order.read',    'View suppliers and station purchase orders', 'procurement', true),
    ('purchase_order.manage',  'Raise and edit station purchase orders',      'procurement', true),
    ('purchase_order.approve', 'Submit, confirm, cancel, or close orders',    'procurement', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND (
    (p.code = 'purchase_order.read' AND r.code IN (
        'system_admin', 'regional_manager', 'station_manager', 'supervisor',
        'procurement_officer', 'finance_officer', 'executive', 'auditor'
    ))
    OR (p.code = 'purchase_order.manage' AND r.code IN (
        'system_admin', 'regional_manager', 'station_manager', 'procurement_officer'
    ))
    OR (p.code = 'purchase_order.approve' AND r.code IN (
        'system_admin', 'regional_manager', 'station_manager', 'procurement_officer', 'executive'
    ))
);
