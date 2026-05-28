-- 0048_petty_cash: station petty-cash floats (Phase 7, Stage 12).
--
-- A float is a small cash balance held at a station. Top-ups move bank -> petty
-- cash; spend moves petty cash -> an expense account; reconciliation compares
-- the expected float balance to a physical count and posts the variance to cash
-- over/short. A transaction cannot overdraw the float unless an override is
-- recorded. Float lifecycle: active -> suspended -> closed.

CREATE TABLE petty_cash_floats (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id  uuid NOT NULL,
    name        text NOT NULL,
    balance     numeric(14, 2) NOT NULL DEFAULT 0,
    status      text NOT NULL DEFAULT 'active',
    created_by  uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_pcf_status CHECK (status IN ('active', 'suspended', 'closed')),
    -- balance may go negative only when an authorized overdraw is recorded; the
    -- overdraw guard lives in the repo, not a column CHECK.
    CONSTRAINT pcf_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT pcf_created_by_fk
        FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_pcf_tenant  ON petty_cash_floats(tenant_id);
CREATE INDEX idx_pcf_station ON petty_cash_floats(station_id);
ALTER TABLE petty_cash_floats ADD CONSTRAINT uq_pcf_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER petty_cash_floats_set_updated_at
    BEFORE UPDATE ON petty_cash_floats
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE petty_cash_floats ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON petty_cash_floats
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE petty_cash_transactions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    float_id         uuid NOT NULL,
    txn_type         text NOT NULL,
    amount           numeric(14, 2) NOT NULL,
    balance_after    numeric(14, 2) NOT NULL,
    description      text,
    account_key      text,
    overdraw         boolean NOT NULL DEFAULT false,
    journal_entry_id uuid,
    created_by       uuid NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_pct_amount CHECK (amount > 0),
    CONSTRAINT chk_pct_type
        CHECK (txn_type IN ('topup', 'spend', 'reimbursement', 'adjustment', 'transfer')),
    CONSTRAINT pct_float_fk
        FOREIGN KEY (tenant_id, float_id) REFERENCES petty_cash_floats(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT pct_created_by_fk
        FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_pct_tenant ON petty_cash_transactions(tenant_id);
CREATE INDEX idx_pct_float  ON petty_cash_transactions(float_id);

ALTER TABLE petty_cash_transactions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON petty_cash_transactions
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE petty_cash_reconciliations (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    float_id         uuid NOT NULL,
    expected_balance numeric(14, 2) NOT NULL,
    counted_cash     numeric(14, 2) NOT NULL,
    variance         numeric(14, 2) NOT NULL,
    journal_entry_id uuid,
    reconciled_by    uuid NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT pcr_float_fk
        FOREIGN KEY (tenant_id, float_id) REFERENCES petty_cash_floats(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT pcr_reconciled_by_fk
        FOREIGN KEY (tenant_id, reconciled_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_pcr_tenant ON petty_cash_reconciliations(tenant_id);
CREATE INDEX idx_pcr_float  ON petty_cash_reconciliations(float_id);

ALTER TABLE petty_cash_reconciliations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON petty_cash_reconciliations
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: petty_cash.manage / .approve / .reconcile (tenant-wide finance).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('petty_cash.manage',    'Manage petty cash floats and transactions', 'finance', false),
    ('petty_cash.reconcile', 'Reconcile petty cash floats',               'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('petty_cash.manage', 'petty_cash.reconcile')
  AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
