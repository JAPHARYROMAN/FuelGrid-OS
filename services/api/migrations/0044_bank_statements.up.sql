-- 0044_bank_statements: importing and matching bank statement lines
-- (Phase 7, Stage 6).
--
-- An import carries a content hash so the same file is not ingested twice. Each
-- line starts unmatched and is reconciled against a deposit, supplier payment,
-- customer payment, marked as a bank fee (posted: debit operating expense,
-- credit bank), or marked unknown for follow-up.

CREATE TABLE bank_statement_imports (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    bank_account_id uuid NOT NULL,
    statement_start date,
    statement_end   date,
    import_hash     text NOT NULL,
    line_count      integer NOT NULL DEFAULT 0,
    imported_by     uuid NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT bsi_account_fk
        FOREIGN KEY (tenant_id, bank_account_id) REFERENCES bank_accounts(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT bsi_imported_by_fk
        FOREIGN KEY (tenant_id, imported_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

-- Idempotent import: the same statement content cannot be ingested twice.
CREATE UNIQUE INDEX uq_bsi_hash ON bank_statement_imports(tenant_id, bank_account_id, import_hash);
CREATE INDEX idx_bsi_tenant ON bank_statement_imports(tenant_id);
ALTER TABLE bank_statement_imports ADD CONSTRAINT uq_bsi_tenant_id UNIQUE (tenant_id, id);

ALTER TABLE bank_statement_imports ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON bank_statement_imports
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE bank_statement_lines (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    import_id        uuid NOT NULL,
    bank_account_id  uuid NOT NULL,
    txn_date         date NOT NULL,
    value_date       date,
    amount           numeric(14, 2) NOT NULL,
    reference        text,
    description      text,
    status           text NOT NULL DEFAULT 'unmatched',
    matched_doc_type text,
    matched_doc_id   uuid,
    journal_entry_id uuid,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_bsl_status
        CHECK (status IN ('unmatched', 'matched', 'bank_fee', 'unknown')),
    CONSTRAINT bsl_import_fk
        FOREIGN KEY (tenant_id, import_id) REFERENCES bank_statement_imports(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT bsl_account_fk
        FOREIGN KEY (tenant_id, bank_account_id) REFERENCES bank_accounts(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_bsl_tenant ON bank_statement_lines(tenant_id);
CREATE INDEX idx_bsl_import ON bank_statement_lines(import_id);
CREATE INDEX idx_bsl_status ON bank_statement_lines(tenant_id, status);
ALTER TABLE bank_statement_lines ADD CONSTRAINT uq_bsl_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER bank_statement_lines_set_updated_at
    BEFORE UPDATE ON bank_statement_lines
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE bank_statement_lines ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON bank_statement_lines
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: bank_statement.manage (tenant-wide finance).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('bank_statement.manage', 'Import and match bank statement lines', 'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'bank_statement.manage'
  AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
