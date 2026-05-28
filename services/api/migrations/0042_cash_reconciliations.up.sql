-- 0042_cash_reconciliations: verifying station cash against Phase-6 expected
-- cash (Phase 7, Stage 4).
--
-- A cash reconciliation compares the cash a station's shifts *should* have
-- collected (Phase-6 cash tenders, the expected source — never raw meter data)
-- against the cash physically counted. The variance posts to cash over/short.
-- Lifecycle: draft -> submitted -> approved -> posted; rejected returns to
-- draft. Approval posts a balanced journal entry: debit cash on hand, credit
-- sales clearing, with the over/short remainder to the cash over/short account.

CREATE TABLE cash_reconciliations (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id       uuid NOT NULL,
    operating_day_id uuid NOT NULL,
    expected_cash    numeric(14, 2) NOT NULL DEFAULT 0,
    counted_cash     numeric(14, 2) NOT NULL DEFAULT 0,
    variance         numeric(14, 2) NOT NULL DEFAULT 0,
    status           text NOT NULL DEFAULT 'draft',
    notes            text,
    journal_entry_id uuid,
    reviewed_by      uuid,
    created_by       uuid NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_cash_recon_status
        CHECK (status IN ('draft', 'submitted', 'approved', 'posted', 'rejected')),
    CONSTRAINT cash_recon_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT cash_recon_day_fk
        FOREIGN KEY (tenant_id, operating_day_id) REFERENCES operating_days(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT cash_recon_created_by_fk
        FOREIGN KEY (tenant_id, created_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

-- One reconciliation per operating day (idempotent create).
CREATE UNIQUE INDEX uq_cash_recon_day ON cash_reconciliations(tenant_id, operating_day_id);
CREATE INDEX idx_cash_recon_tenant  ON cash_reconciliations(tenant_id);
CREATE INDEX idx_cash_recon_station ON cash_reconciliations(station_id);
ALTER TABLE cash_reconciliations ADD CONSTRAINT uq_cash_recon_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER cash_reconciliations_set_updated_at
    BEFORE UPDATE ON cash_reconciliations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE cash_reconciliations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cash_reconciliations
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE cash_reconciliation_lines (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id              uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    cash_reconciliation_id uuid NOT NULL,
    shift_id               uuid NOT NULL,
    expected_cash          numeric(14, 2) NOT NULL DEFAULT 0,
    created_at             timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT crl_recon_fk
        FOREIGN KEY (tenant_id, cash_reconciliation_id) REFERENCES cash_reconciliations(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT crl_shift_fk
        FOREIGN KEY (tenant_id, shift_id) REFERENCES shifts(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_crl_tenant ON cash_reconciliation_lines(tenant_id);
CREATE INDEX idx_crl_recon  ON cash_reconciliation_lines(cash_reconciliation_id);

ALTER TABLE cash_reconciliation_lines ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cash_reconciliation_lines
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: cash_reconciliation.manage / .approve (tenant-wide finance).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('cash_reconciliation.manage',  'Create and submit cash reconciliations', 'finance', false),
    ('cash_reconciliation.approve', 'Approve and post cash reconciliations',  'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('cash_reconciliation.manage', 'cash_reconciliation.approve')
  AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
