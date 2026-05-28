-- 0047_expenses: controlled operating spend (Phase 7, Stage 11).
--
-- An expense captures non-fuel operational spend against an expense account
-- (via an optional category) and a payment mode. It moves draft -> submitted ->
-- approved -> posted; posting debits the expense account and credits the
-- payment-mode account (cash / bank / accounts payable). History is preserved:
-- corrections reverse the posted journal entry rather than deleting the row.

CREATE TABLE expense_categories (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    name        text NOT NULL,
    account_key text NOT NULL DEFAULT 'operating_expense',
    status      text NOT NULL DEFAULT 'active',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_expense_category_status CHECK (status IN ('active', 'archived'))
);

CREATE UNIQUE INDEX uq_expense_categories_name ON expense_categories(tenant_id, lower(name));
CREATE INDEX idx_expense_categories_tenant ON expense_categories(tenant_id);
ALTER TABLE expense_categories ADD CONSTRAINT uq_expense_categories_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER expense_categories_set_updated_at
    BEFORE UPDATE ON expense_categories
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE expense_categories ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON expense_categories
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE expenses (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id       uuid,
    category_id      uuid,
    payee            text,
    expense_date     date NOT NULL DEFAULT CURRENT_DATE,
    amount           numeric(14, 2) NOT NULL,
    account_key      text NOT NULL DEFAULT 'operating_expense',
    payment_mode     text NOT NULL DEFAULT 'cash',
    reference        text,
    notes            text,
    status           text NOT NULL DEFAULT 'draft',
    journal_entry_id uuid,
    approved_by      uuid,
    created_by       uuid NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_expense_amount CHECK (amount > 0),
    CONSTRAINT chk_expense_status
        CHECK (status IN ('draft', 'submitted', 'approved', 'posted', 'rejected', 'voided')),
    CONSTRAINT chk_expense_payment_mode CHECK (payment_mode IN ('cash', 'bank', 'payable', 'petty_cash')),
    CONSTRAINT expense_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT expense_category_fk
        FOREIGN KEY (tenant_id, category_id) REFERENCES expense_categories(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT expense_created_by_fk
        FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_expenses_tenant  ON expenses(tenant_id);
CREATE INDEX idx_expenses_station ON expenses(station_id);
CREATE INDEX idx_expenses_status  ON expenses(tenant_id, status);
ALTER TABLE expenses ADD CONSTRAINT uq_expenses_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER expenses_set_updated_at
    BEFORE UPDATE ON expenses
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE expenses ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON expenses
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: expense.manage / .approve / .post (tenant-wide finance).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('expense.manage',  'Create and submit expenses',        'finance', false),
    ('expense.approve', 'Approve expenses',                  'finance', false),
    ('expense.post',    'Post approved expenses to ledger',  'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('expense.manage', 'expense.approve', 'expense.post')
  AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
