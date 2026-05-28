-- 0043_bank_deposits: moving station cash into the bank (Phase 7, Stage 5).
--
-- A deposit groups one or more approved/posted cash reconciliations into a slip
-- carried to the bank. Funds move cash on hand -> bank clearing at preparation,
-- then bank clearing -> bank account on confirmation. A cash reconciliation can
-- be deposited at most once (unique guard on the deposit line).
-- Lifecycle: draft -> prepared -> in_transit -> confirmed -> posted; voided.

CREATE TABLE bank_accounts (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    name           text NOT NULL,
    account_number text,
    currency       text NOT NULL DEFAULT 'NGN',
    status         text NOT NULL DEFAULT 'active',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_bank_account_status CHECK (status IN ('active', 'closed'))
);

CREATE INDEX idx_bank_accounts_tenant ON bank_accounts(tenant_id);
ALTER TABLE bank_accounts ADD CONSTRAINT uq_bank_accounts_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER bank_accounts_set_updated_at
    BEFORE UPDATE ON bank_accounts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE bank_accounts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON bank_accounts
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE bank_deposits (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id         uuid NOT NULL,
    bank_account_id    uuid NOT NULL,
    slip_number        text,
    amount             numeric(14, 2) NOT NULL DEFAULT 0,
    reference          text,
    expected_bank_date date,
    actual_bank_date   date,
    status             text NOT NULL DEFAULT 'draft',
    prepared_entry_id  uuid,
    confirmed_entry_id uuid,
    created_by         uuid NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_bank_deposit_status
        CHECK (status IN ('draft', 'prepared', 'in_transit', 'confirmed', 'posted', 'voided')),
    CONSTRAINT bank_deposit_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT bank_deposit_account_fk
        FOREIGN KEY (tenant_id, bank_account_id) REFERENCES bank_accounts(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT bank_deposit_created_by_fk
        FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_bank_deposits_tenant  ON bank_deposits(tenant_id);
CREATE INDEX idx_bank_deposits_station ON bank_deposits(station_id);
ALTER TABLE bank_deposits ADD CONSTRAINT uq_bank_deposits_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER bank_deposits_set_updated_at
    BEFORE UPDATE ON bank_deposits
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE bank_deposits ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON bank_deposits
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE bank_deposit_lines (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id              uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    bank_deposit_id        uuid NOT NULL,
    cash_reconciliation_id uuid NOT NULL,
    amount                 numeric(14, 2) NOT NULL,
    created_at             timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_bdl_amount CHECK (amount > 0),
    CONSTRAINT bdl_deposit_fk
        FOREIGN KEY (tenant_id, bank_deposit_id) REFERENCES bank_deposits(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT bdl_recon_fk
        FOREIGN KEY (tenant_id, cash_reconciliation_id) REFERENCES cash_reconciliations(tenant_id, id) ON DELETE RESTRICT
);

-- A given cash reconciliation can only be deposited once.
CREATE UNIQUE INDEX uq_bdl_recon ON bank_deposit_lines(tenant_id, cash_reconciliation_id);
CREATE INDEX idx_bdl_tenant  ON bank_deposit_lines(tenant_id);
CREATE INDEX idx_bdl_deposit ON bank_deposit_lines(bank_deposit_id);

ALTER TABLE bank_deposit_lines ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON bank_deposit_lines
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: bank_account.manage / bank_deposit.manage / bank_deposit.confirm.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('bank_account.manage',   'Maintain bank accounts',                 'finance', false),
    ('bank_deposit.manage',   'Prepare and manage bank deposits',       'finance', false),
    ('bank_deposit.confirm',  'Confirm and post bank deposits',         'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('bank_account.manage', 'bank_deposit.manage', 'bank_deposit.confirm')
  AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
