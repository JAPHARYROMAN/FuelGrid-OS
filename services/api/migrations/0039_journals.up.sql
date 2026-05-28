-- 0039_journals: the double-entry journal engine (Phase 7, Stage 3).
--
-- Every financial document posts a balanced journal entry: total debits equal
-- total credits. Posted entries are immutable — corrections are reversals
-- (a new balanced entry linked to the original). Entries link to their source
-- document (cash reconciliation, deposit, payable, payment, invoice, expense,
-- adjustment) so every report total drills back to a cause.

CREATE TABLE journal_entries (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_number         bigint GENERATED ALWAYS AS IDENTITY,
    tenant_id            uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    period_id            uuid NOT NULL,
    entry_date           date NOT NULL,
    source_type          text NOT NULL,
    source_id            uuid,
    station_id           uuid,
    status               text NOT NULL DEFAULT 'posted',
    memo                 text,
    reverses_entry_id    uuid,
    reversed_by_entry_id uuid,
    posted_by            uuid NOT NULL,
    posted_at            timestamptz NOT NULL DEFAULT now(),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_journal_status CHECK (status IN ('draft', 'posted', 'reversed')),

    CONSTRAINT journal_entries_period_fk
        FOREIGN KEY (tenant_id, period_id) REFERENCES accounting_periods(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT journal_entries_posted_by_fk
        FOREIGN KEY (tenant_id, posted_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_journal_entries_tenant_id ON journal_entries(tenant_id);
CREATE INDEX idx_journal_entries_period    ON journal_entries(period_id);
CREATE INDEX idx_journal_entries_source    ON journal_entries(source_type, source_id) WHERE source_id IS NOT NULL;
CREATE INDEX idx_journal_entries_date      ON journal_entries(entry_date);

ALTER TABLE journal_entries ADD CONSTRAINT uq_journal_entries_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER journal_entries_set_updated_at
    BEFORE UPDATE ON journal_entries
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE journal_entries ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON journal_entries
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE journal_lines (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    journal_entry_id uuid NOT NULL,
    account_id       uuid NOT NULL,
    debit            numeric(14, 2) NOT NULL DEFAULT 0,
    credit           numeric(14, 2) NOT NULL DEFAULT 0,
    station_id       uuid,
    memo             text,
    created_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_journal_line_amounts CHECK (
        debit >= 0 AND credit >= 0 AND NOT (debit > 0 AND credit > 0)
    ),

    CONSTRAINT journal_lines_entry_fk
        FOREIGN KEY (tenant_id, journal_entry_id) REFERENCES journal_entries(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT journal_lines_account_fk
        FOREIGN KEY (tenant_id, account_id) REFERENCES accounts(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_journal_lines_tenant_id ON journal_lines(tenant_id);
CREATE INDEX idx_journal_lines_entry     ON journal_lines(journal_entry_id);
CREATE INDEX idx_journal_lines_account   ON journal_lines(account_id);

ALTER TABLE journal_lines ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON journal_lines
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: journal.read (views), journal.adjust (manual adjustments +
-- reversals), tenant-wide.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('journal.read',   'View journal entries and the general ledger', 'finance', false),
    ('journal.adjust', 'Post manual journal adjustments and reversals', 'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'journal.read'
  AND r.code IN ('system_admin', 'finance_officer', 'regional_manager', 'executive', 'auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'journal.adjust' AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
