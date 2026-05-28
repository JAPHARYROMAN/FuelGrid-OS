-- 0041_supplier_payments: paying down accounts payable (Phase 7, Stage 8).
--
-- A supplier payment allocates an amount across one or more payables, reducing
-- their outstanding balance, and posts to the journal (debit AP, credit
-- bank/cash). Allocations cannot exceed the payment amount or a payable's
-- outstanding balance.

CREATE TABLE supplier_payments (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    supplier_id      uuid NOT NULL,
    payment_date     date NOT NULL,
    method           text NOT NULL,
    reference        text,
    amount           numeric(14, 2) NOT NULL,
    allocated_amount numeric(14, 2) NOT NULL DEFAULT 0,
    source_account_key text NOT NULL DEFAULT 'bank',
    status           text NOT NULL DEFAULT 'posted',
    journal_entry_id uuid,
    created_by       uuid NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_supplier_payments_amount CHECK (amount >= 0 AND allocated_amount >= 0),
    CONSTRAINT chk_supplier_payments_status CHECK (status IN ('posted', 'voided')),

    CONSTRAINT supplier_payments_created_by_fk
        FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_supplier_payments_tenant_id ON supplier_payments(tenant_id);
CREATE INDEX idx_supplier_payments_supplier  ON supplier_payments(supplier_id);
ALTER TABLE supplier_payments ADD CONSTRAINT uq_supplier_payments_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER supplier_payments_set_updated_at
    BEFORE UPDATE ON supplier_payments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE supplier_payments ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supplier_payments
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE supplier_payment_allocations (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    supplier_payment_id uuid NOT NULL,
    payable_id         uuid NOT NULL,
    amount             numeric(14, 2) NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_spa_amount CHECK (amount > 0),
    CONSTRAINT spa_payment_fk
        FOREIGN KEY (tenant_id, supplier_payment_id) REFERENCES supplier_payments(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT spa_payable_fk
        FOREIGN KEY (tenant_id, payable_id) REFERENCES payables(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_spa_tenant_id ON supplier_payment_allocations(tenant_id);
CREATE INDEX idx_spa_payment   ON supplier_payment_allocations(supplier_payment_id);
CREATE INDEX idx_spa_payable   ON supplier_payment_allocations(payable_id);

ALTER TABLE supplier_payment_allocations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supplier_payment_allocations
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: supplier_payment.manage / .approve / .post (tenant-wide).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('supplier_payment.manage', 'Record and allocate supplier payments', 'finance', false),
    ('supplier_payment.post',   'Post supplier payments to the ledger',  'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('supplier_payment.manage', 'supplier_payment.post')
  AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
