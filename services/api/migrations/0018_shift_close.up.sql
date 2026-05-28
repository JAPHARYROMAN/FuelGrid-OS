-- 0018_shift_close: shift close snapshot + cash reconciliation (Phase 3, Stage 5).
--
-- Closing a shift freezes a per-nozzle line (opening/closing meter, litres
-- sold, unit price, expected value) into shift_close_lines and totals the
-- expected cash. The attendant's tender breakdown and the resulting
-- shortage/excess land in cash_submissions.

CREATE TABLE shift_close_lines (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    shift_id       uuid NOT NULL,
    nozzle_id      uuid NOT NULL,
    opening_reading numeric(14, 3) NOT NULL,
    closing_reading numeric(14, 3) NOT NULL,
    litres_sold    numeric(14, 3) NOT NULL,
    unit_price     numeric(14, 2) NOT NULL,
    expected_value numeric(14, 2) NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT shift_close_lines_shift_fk
        FOREIGN KEY (tenant_id, shift_id) REFERENCES shifts(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT shift_close_lines_nozzle_fk
        FOREIGN KEY (tenant_id, nozzle_id) REFERENCES nozzles(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT uq_shift_close_lines UNIQUE (shift_id, nozzle_id)
);

CREATE INDEX idx_shift_close_lines_tenant_id ON shift_close_lines(tenant_id);
CREATE INDEX idx_shift_close_lines_shift_id  ON shift_close_lines(shift_id);

ALTER TABLE shift_close_lines ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON shift_close_lines
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE cash_submissions (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    shift_id            uuid NOT NULL,
    expected_cash       numeric(14, 2) NOT NULL,
    cash_amount         numeric(14, 2) NOT NULL DEFAULT 0,
    mobile_money_amount numeric(14, 2) NOT NULL DEFAULT 0,
    card_amount         numeric(14, 2) NOT NULL DEFAULT 0,
    credit_amount       numeric(14, 2) NOT NULL DEFAULT 0,
    submitted_total     numeric(14, 2) NOT NULL,
    variance            numeric(14, 2) NOT NULL,
    submitted_by        uuid NOT NULL,
    submitted_at        timestamptz NOT NULL DEFAULT now(),
    notes               text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT cash_submissions_shift_fk
        FOREIGN KEY (tenant_id, shift_id) REFERENCES shifts(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT cash_submissions_submitted_by_fk
        FOREIGN KEY (tenant_id, submitted_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    -- One cash submission per shift.
    CONSTRAINT uq_cash_submissions_shift UNIQUE (shift_id)
);

CREATE INDEX idx_cash_submissions_tenant_id ON cash_submissions(tenant_id);

CREATE TRIGGER cash_submissions_set_updated_at
    BEFORE UPDATE ON cash_submissions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE cash_submissions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON cash_submissions
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: cash.submit (station-scoped). Attendants submit cash; close
-- stays on shift.close.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('cash.submit', 'Submit a shift cash reconciliation', 'shift', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'cash.submit'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor', 'attendant');
