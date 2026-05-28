-- 0045_customer_invoices: billing customers on the finance ledger
-- (Phase 7, Stage 9).
--
-- A customer invoice is a finance document sourced from a Phase-6 credit sale,
-- a manual finance charge, or an adjustment. Issuing posts debit accounts
-- receivable / credit revenue through the journal engine. Outstanding amount
-- falls as customer payments allocate against it.
-- Lifecycle: draft -> issued -> partially_paid -> paid; voided.

CREATE TABLE customer_invoices (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id        uuid NOT NULL,
    invoice_number     text,
    invoice_date       date NOT NULL DEFAULT CURRENT_DATE,
    due_date           date,
    amount             numeric(14, 2) NOT NULL,
    outstanding_amount numeric(14, 2) NOT NULL,
    source_type        text NOT NULL DEFAULT 'manual',
    source_id          uuid,
    station_id         uuid,
    status             text NOT NULL DEFAULT 'draft',
    journal_entry_id   uuid,
    created_by         uuid NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_customer_invoice_amount CHECK (amount >= 0 AND outstanding_amount >= 0),
    CONSTRAINT chk_customer_invoice_status
        CHECK (status IN ('draft', 'issued', 'partially_paid', 'paid', 'voided')),
    CONSTRAINT chk_customer_invoice_source
        CHECK (source_type IN ('credit_sale', 'manual', 'adjustment')),
    CONSTRAINT customer_invoice_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT customer_invoice_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT customer_invoice_created_by_fk
        FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_customer_invoices_tenant   ON customer_invoices(tenant_id);
CREATE INDEX idx_customer_invoices_customer ON customer_invoices(customer_id);
ALTER TABLE customer_invoices ADD CONSTRAINT uq_customer_invoices_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER customer_invoices_set_updated_at
    BEFORE UPDATE ON customer_invoices
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE customer_invoices ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_invoices
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE customer_invoice_lines (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_invoice_id uuid NOT NULL,
    description         text,
    amount              numeric(14, 2) NOT NULL,
    revenue_account_key text NOT NULL DEFAULT 'sales_revenue',
    created_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_cil_amount CHECK (amount > 0),
    CONSTRAINT cil_invoice_fk
        FOREIGN KEY (tenant_id, customer_invoice_id) REFERENCES customer_invoices(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_cil_tenant  ON customer_invoice_lines(tenant_id);
CREATE INDEX idx_cil_invoice ON customer_invoice_lines(customer_invoice_id);

ALTER TABLE customer_invoice_lines ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_invoice_lines
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: customer_invoice.manage / .issue (tenant-wide finance).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('customer_invoice.manage', 'Create and manage customer invoices', 'finance', false),
    ('customer_invoice.issue',  'Issue customer invoices to the ledger', 'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('customer_invoice.manage', 'customer_invoice.issue')
  AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
