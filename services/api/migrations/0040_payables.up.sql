-- 0040_payables: accounts-payable ledger (Phase 7, Stage 7).
--
-- A payable is created once per approved Phase-5 supplier invoice
-- (source_invoice_id is unique — the idempotency key), ages by due date, and
-- posts to AP through the journal engine (debit inventory, credit AP). Supplier
-- payments (0041) draw the outstanding amount down. No FK to the Phase-5
-- tables — the source ids are carried as a contract, scoped by tenant.

CREATE TABLE payables (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    supplier_id        uuid NOT NULL,
    source_invoice_id  uuid NOT NULL,
    invoice_number     text,
    invoice_date       date,
    due_date           date,
    amount             numeric(14, 2) NOT NULL,
    outstanding_amount numeric(14, 2) NOT NULL,
    station_id         uuid,
    status             text NOT NULL DEFAULT 'open',
    journal_entry_id   uuid,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_payables_status CHECK (status IN ('open', 'partially_paid', 'paid', 'voided')),
    CONSTRAINT chk_payables_amounts CHECK (amount >= 0 AND outstanding_amount >= 0),
    CONSTRAINT uq_payables_source_invoice UNIQUE (tenant_id, source_invoice_id)
);

CREATE INDEX idx_payables_tenant_id ON payables(tenant_id);
CREATE INDEX idx_payables_supplier  ON payables(supplier_id);
CREATE INDEX idx_payables_status    ON payables(status) WHERE status <> 'paid';

ALTER TABLE payables ADD CONSTRAINT uq_payables_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER payables_set_updated_at
    BEFORE UPDATE ON payables
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE payables ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON payables
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: payable.read, payable.manage (tenant-wide).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('payable.read',   'View accounts payable and aging', 'finance', false),
    ('payable.manage', 'Import and adjust payables',      'finance', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'payable.read'
  AND r.code IN ('system_admin', 'finance_officer', 'procurement_officer', 'regional_manager', 'executive', 'auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'payable.manage' AND r.code IN ('system_admin', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
