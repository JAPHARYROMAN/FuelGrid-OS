-- 0035_payments: shift tender records (Phase 6, Stage 5).
--
-- Discrete per-tender payment records — the finance-grade evolution of the
-- Phase-3 cash_submission's per-tender fields. Reconciled against recognized
-- revenue per shift. A 'credit' tender allocated to a customer also posts an
-- AR charge (0034) in the same transaction.

CREATE TABLE payments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id  uuid NOT NULL,
    shift_id    uuid,
    customer_id uuid,
    tender_type text NOT NULL,
    amount      numeric(14, 2) NOT NULL,
    reference   text,
    received_by uuid NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    status      text NOT NULL DEFAULT 'recorded',
    notes       text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_payments_tender CHECK (tender_type IN ('cash', 'mobile_money', 'card', 'credit', 'voucher')),
    CONSTRAINT chk_payments_amount CHECK (amount >= 0),
    CONSTRAINT chk_payments_status CHECK (status IN ('recorded', 'voided')),

    CONSTRAINT payments_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT payments_shift_fk
        FOREIGN KEY (tenant_id, shift_id) REFERENCES shifts(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT payments_customer_fk
        FOREIGN KEY (tenant_id, customer_id) REFERENCES customers(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT payments_received_by_fk
        FOREIGN KEY (tenant_id, received_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_payments_tenant_id ON payments(tenant_id);
CREATE INDEX idx_payments_shift     ON payments(shift_id);
CREATE INDEX idx_payments_station   ON payments(station_id);
CREATE INDEX idx_payments_customer  ON payments(customer_id) WHERE customer_id IS NOT NULL;

ALTER TABLE payments ADD CONSTRAINT uq_payments_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER payments_set_updated_at
    BEFORE UPDATE ON payments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE payments ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payments
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: payment.record (station-scoped).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('payment.record', 'Record shift tender payments', 'finance', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'payment.record'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor',
                 'attendant', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
