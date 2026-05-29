-- 0031_supplier_invoices: three-way match and payable handoff
-- (Phase 5, Stages 5-6).
--
-- A supplier invoice is recorded against one purchase order. Lines are matched
-- against PO lines and goods receipts. Over-tolerance mismatches raise open
-- procurement_discrepancies; approval is refused until all are resolved.

CREATE TABLE supplier_invoices (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    supplier_id       uuid NOT NULL,
    purchase_order_id uuid NOT NULL,
    station_id        uuid NOT NULL,
    invoice_number    text NOT NULL,
    status            text NOT NULL DEFAULT 'recorded',
    received_at       timestamptz NOT NULL DEFAULT now(),
    due_date          date,
    total_amount      numeric(14, 2) NOT NULL DEFAULT 0,
    recorded_by       uuid NOT NULL,
    approved_by       uuid,
    approved_at       timestamptz,
    notes             text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_supplier_invoices_status CHECK (
        status IN ('recorded', 'matched', 'discrepancy', 'approved')
    ),
    CONSTRAINT chk_supplier_invoices_total CHECK (total_amount >= 0),
    CONSTRAINT supplier_invoices_supplier_fk
        FOREIGN KEY (tenant_id, supplier_id) REFERENCES suppliers(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT supplier_invoices_po_fk
        FOREIGN KEY (tenant_id, purchase_order_id) REFERENCES purchase_orders(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT supplier_invoices_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT supplier_invoices_recorded_by_fk
        FOREIGN KEY (tenant_id, recorded_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT supplier_invoices_approved_by_fk
        FOREIGN KEY (tenant_id, approved_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_supplier_invoices_tenant_id ON supplier_invoices(tenant_id);
CREATE INDEX idx_supplier_invoices_po_id ON supplier_invoices(purchase_order_id);
CREATE INDEX idx_supplier_invoices_station_status ON supplier_invoices(station_id, status);
CREATE UNIQUE INDEX idx_supplier_invoices_number
    ON supplier_invoices(tenant_id, supplier_id, lower(invoice_number));
ALTER TABLE supplier_invoices ADD CONSTRAINT uq_supplier_invoices_tenant_id UNIQUE (tenant_id, id);

CREATE TABLE supplier_invoice_lines (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    supplier_invoice_id uuid NOT NULL,
    purchase_order_id   uuid NOT NULL,
    po_line_id          uuid NOT NULL,
    delivery_id         uuid,
    product_id          uuid NOT NULL,
    invoiced_litres     numeric(14, 3) NOT NULL,
    unit_price          numeric(14, 2) NOT NULL,
    amount              numeric(14, 2) NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_supplier_invoice_lines_qty CHECK (invoiced_litres > 0),
    CONSTRAINT chk_supplier_invoice_lines_price CHECK (unit_price >= 0),
    CONSTRAINT chk_supplier_invoice_lines_amount CHECK (amount >= 0),
    CONSTRAINT supplier_invoice_lines_invoice_fk
        FOREIGN KEY (tenant_id, supplier_invoice_id) REFERENCES supplier_invoices(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT supplier_invoice_lines_po_fk
        FOREIGN KEY (tenant_id, purchase_order_id) REFERENCES purchase_orders(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT supplier_invoice_lines_po_line_fk
        FOREIGN KEY (tenant_id, purchase_order_id, po_line_id)
        REFERENCES purchase_order_lines(tenant_id, purchase_order_id, id) ON DELETE RESTRICT,
    CONSTRAINT supplier_invoice_lines_delivery_fk
        FOREIGN KEY (tenant_id, delivery_id) REFERENCES deliveries(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT supplier_invoice_lines_product_fk
        FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_supplier_invoice_lines_tenant_id ON supplier_invoice_lines(tenant_id);
CREATE INDEX idx_supplier_invoice_lines_invoice_id ON supplier_invoice_lines(supplier_invoice_id);
CREATE INDEX idx_supplier_invoice_lines_po_line_id ON supplier_invoice_lines(po_line_id);
ALTER TABLE supplier_invoice_lines ADD CONSTRAINT uq_supplier_invoice_lines_tenant_id UNIQUE (tenant_id, id);

CREATE TABLE procurement_discrepancies (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    supplier_invoice_id uuid NOT NULL,
    purchase_order_id   uuid NOT NULL,
    delivery_id         uuid,
    po_line_id          uuid,
    type                text NOT NULL,
    severity            text NOT NULL DEFAULT 'blocking',
    detail              text NOT NULL,
    variance_litres     numeric(14, 3),
    variance_amount     numeric(14, 2),
    status              text NOT NULL DEFAULT 'open',
    raised_at           timestamptz NOT NULL DEFAULT now(),
    resolved_by         uuid,
    resolved_at         timestamptz,

    CONSTRAINT chk_procurement_discrepancies_type CHECK (type IN ('quantity', 'price')),
    CONSTRAINT chk_procurement_discrepancies_severity CHECK (severity IN ('blocking', 'warning')),
    CONSTRAINT chk_procurement_discrepancies_status CHECK (status IN ('open', 'resolved')),
    CONSTRAINT procurement_discrepancies_invoice_fk
        FOREIGN KEY (tenant_id, supplier_invoice_id) REFERENCES supplier_invoices(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT procurement_discrepancies_po_fk
        FOREIGN KEY (tenant_id, purchase_order_id) REFERENCES purchase_orders(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT procurement_discrepancies_delivery_fk
        FOREIGN KEY (tenant_id, delivery_id) REFERENCES deliveries(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT procurement_discrepancies_po_line_fk
        FOREIGN KEY (tenant_id, purchase_order_id, po_line_id)
        REFERENCES purchase_order_lines(tenant_id, purchase_order_id, id) ON DELETE RESTRICT,
    CONSTRAINT procurement_discrepancies_resolved_by_fk
        FOREIGN KEY (tenant_id, resolved_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_procurement_discrepancies_tenant_id ON procurement_discrepancies(tenant_id);
CREATE INDEX idx_procurement_discrepancies_invoice_status
    ON procurement_discrepancies(supplier_invoice_id, status);
ALTER TABLE procurement_discrepancies ADD CONSTRAINT uq_procurement_discrepancies_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER supplier_invoices_set_updated_at
    BEFORE UPDATE ON supplier_invoices
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE supplier_invoices ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supplier_invoices
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE supplier_invoice_lines ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supplier_invoice_lines
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE procurement_discrepancies ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON procurement_discrepancies
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('invoice.manage',  'Record and match supplier invoices', 'procurement', true),
    ('invoice.approve', 'Approve matched supplier invoices',   'procurement', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND (
    (p.code = 'invoice.manage' AND r.code IN (
        'system_admin', 'procurement_officer', 'finance_officer', 'station_manager'
    ))
    OR (p.code = 'invoice.approve' AND r.code IN (
        'system_admin', 'procurement_officer', 'finance_officer', 'executive'
    ))
);
