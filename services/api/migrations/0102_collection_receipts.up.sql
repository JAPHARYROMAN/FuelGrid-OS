-- 0102_collection_receipts: supervisor confirmation of a shift's cash
-- submission (Mobile Attendant App, Phase 0 — the handover chain).
--
-- A collection receipt is the supervisor's acknowledgement of the physical
-- cash handed over for a closed shift. It snapshots the expected amount and
-- the attendant's submitted total at confirmation time, records what the
-- supervisor actually received, and computes the difference (received −
-- expected) in SQL numeric — never Go float. A reason is mandatory whenever
-- the difference is non-zero or the handover is rejected.
--
--   received                 — difference = 0, cash accepted as-is.
--   approved_with_difference — difference ≠ 0, accepted with a reason.
--   rejected                 — handover refused; reason mandatory.
--
-- One receipt per cash submission (UNIQUE). Shift approval is gated on a
-- non-rejected receipt existing for the shift's cash submission.

-- Tenant-bound FK target (mirrors 0042's uq_cash_recon_tenant_id).
ALTER TABLE cash_submissions
    ADD CONSTRAINT uq_cash_submissions_tenant_id UNIQUE (tenant_id, id);

CREATE TABLE collection_receipts (
    id                        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                 uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id                uuid NOT NULL,
    shift_id                  uuid NOT NULL,
    cash_submission_id        uuid NOT NULL,
    -- Snapshots at confirmation time, all numeric(14,2) bound from exact
    -- decimal strings ($N::numeric) — never a Go float.
    expected_amount           numeric(14, 2) NOT NULL,
    attendant_submitted_total numeric(14, 2) NOT NULL,
    supervisor_received_total numeric(14, 2) NOT NULL,
    -- difference = supervisor_received_total − expected_amount, computed in
    -- SQL numeric on insert.
    difference                numeric(14, 2) NOT NULL,
    status                    text NOT NULL,
    reason                    text,
    supervisor_comment        text,
    received_by               uuid NOT NULL,
    received_at               timestamptz NOT NULL DEFAULT now(),
    created_at                timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_collection_receipts_status
        CHECK (status IN ('received', 'approved_with_difference', 'rejected')),
    -- The status must agree with the difference: a clean handover is
    -- 'received', a divergent one is 'approved_with_difference' (or rejected).
    CONSTRAINT chk_collection_receipts_difference
        CHECK (
            status = 'rejected'
            OR (status = 'received' AND difference = 0)
            OR (status = 'approved_with_difference' AND difference <> 0)
        ),
    -- A reason is mandatory whenever the difference is non-zero or the
    -- handover is rejected.
    CONSTRAINT chk_collection_receipts_reason
        CHECK ((difference = 0 AND status <> 'rejected') OR reason IS NOT NULL),
    CONSTRAINT chk_collection_receipts_difference_math
        CHECK (difference = supervisor_received_total - expected_amount),

    -- Tenant-bound composite FKs; the station leg rides 0023's
    -- (tenant_id, station_id, id) key so a receipt can never claim a
    -- different station than its shift.
    CONSTRAINT collection_receipts_shift_fk
        FOREIGN KEY (tenant_id, station_id, shift_id)
        REFERENCES shifts(tenant_id, station_id, id) ON DELETE RESTRICT,
    CONSTRAINT collection_receipts_submission_fk
        FOREIGN KEY (tenant_id, cash_submission_id)
        REFERENCES cash_submissions(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT collection_receipts_received_by_fk
        FOREIGN KEY (tenant_id, received_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,

    -- Exactly one receipt per cash submission.
    CONSTRAINT uq_collection_receipts_submission UNIQUE (cash_submission_id)
);

CREATE INDEX idx_collection_receipts_tenant ON collection_receipts(tenant_id);
CREATE INDEX idx_collection_receipts_shift  ON collection_receipts(shift_id);

ALTER TABLE collection_receipts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON collection_receipts
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: cash.confirm (station-scoped). Confirming a cash handover is a
-- supervisor-class action distinct from submitting cash (cash.submit, 0018)
-- and submitting on someone's behalf (cash.override, 0021).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('cash.confirm', 'Confirm receipt of a shift''s cash submission', 'cash', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'cash.confirm'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
