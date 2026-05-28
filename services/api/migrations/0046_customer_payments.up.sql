-- 0046_customer_payments: receiving money from customers (Phase 7, Stage 10).
--
-- A customer payment allocates an amount across one or more issued invoices,
-- reducing their outstanding balance, and posts debit bank/cash, credit
-- accounts receivable. Allocations cannot exceed the payment amount or an
-- invoice's outstanding balance; unapplied amounts stay as customer credit.

CREATE TABLE customer_payments (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_id        uuid NOT NULL,
    payment_date       date NOT NULL,
    method             text NOT NULL,
    reference          text,
    amount             numeric(14, 2) NOT NULL,
    allocated_amount   numeric(14, 2) NOT NULL DEFAULT 0,
    source_account_key text NOT NULL DEFAULT 'bank',
    status             text NOT NULL DEFAULT 'posted',
    journal_entry_id   uuid,
    created_by         uuid NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_customer_payments_amount CHECK (amount >= 0 AND allocated_amount >= 0),
    CONSTRAINT chk_customer_payments_status CHECK (status IN ('posted', 'voided')),
    CONSTRAINT customer_payments_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT customer_payments_created_by_fk
        FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_customer_payments_tenant   ON customer_payments(tenant_id);
CREATE INDEX idx_customer_payments_customer ON customer_payments(customer_id);
ALTER TABLE customer_payments ADD CONSTRAINT uq_customer_payments_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER customer_payments_set_updated_at
    BEFORE UPDATE ON customer_payments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE customer_payments ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_payments
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE customer_payment_allocations (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    customer_payment_id uuid NOT NULL,
    customer_invoice_id uuid NOT NULL,
    amount              numeric(14, 2) NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_cpa_amount CHECK (amount > 0),
    CONSTRAINT cpa_payment_fk
        FOREIGN KEY (tenant_id, customer_payment_id) REFERENCES customer_payments(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT cpa_invoice_fk
        FOREIGN KEY (tenant_id, customer_invoice_id) REFERENCES customer_invoices(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_cpa_tenant  ON customer_payment_allocations(tenant_id);
CREATE INDEX idx_cpa_payment ON customer_payment_allocations(customer_payment_id);
CREATE INDEX idx_cpa_invoice ON customer_payment_allocations(customer_invoice_id);

ALTER TABLE customer_payment_allocations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_payment_allocations
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: customer_payment.manage / .post (tenant-wide finance).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('customer_payment.manage', 'Record and allocate customer payments', 'finance', false),
    ('customer_payment.post',   'Post customer payments to the ledger',  'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('customer_payment.manage', 'customer_payment.post')
  AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
