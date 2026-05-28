-- 0019_shift_exceptions: auto-raised shift anomaly flags (Phase 3, Stage 6).
--
-- Mechanical exceptions surfaced during the shift lifecycle (e.g. a cash
-- variance over the station threshold). An open exception blocks shift
-- approval until it's resolved. Distinct from the Phase-2 incidents queue,
-- which is operator-raised and station-wide.

CREATE TABLE shift_exceptions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    shift_id    uuid NOT NULL,
    type        text NOT NULL,
    severity    text NOT NULL DEFAULT 'medium',
    detail      text,
    status      text NOT NULL DEFAULT 'open',
    raised_at   timestamptz NOT NULL DEFAULT now(),
    resolved_by uuid,
    resolved_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_shift_exc_type CHECK (
        type IN ('missing_reading', 'cash_variance', 'meter_rollback', 'late_close', 'other')
    ),
    CONSTRAINT chk_shift_exc_severity CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    CONSTRAINT chk_shift_exc_status   CHECK (status IN ('open', 'resolved')),

    CONSTRAINT shift_exceptions_shift_fk
        FOREIGN KEY (tenant_id, shift_id) REFERENCES shifts(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT shift_exceptions_resolved_by_fk
        FOREIGN KEY (tenant_id, resolved_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_shift_exceptions_tenant_id ON shift_exceptions(tenant_id);
CREATE INDEX idx_shift_exceptions_shift_id  ON shift_exceptions(shift_id);
CREATE INDEX idx_shift_exceptions_status    ON shift_exceptions(status);

CREATE TRIGGER shift_exceptions_set_updated_at
    BEFORE UPDATE ON shift_exceptions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE shift_exceptions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON shift_exceptions
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Approval + exception resolution reuse the existing shift.approve
-- permission (0004); day lock reuses operations.manage_day (0014).
