-- 0101_reading_verifications: supervisor verification of closing meter
-- readings — the dual-value model (Mobile Attendant App, Phase 0).
--
-- The attendant's original submission is preserved FOREVER: meter_readings
-- rows are never mutated by verification. A verification row snapshots the
-- attendant's submitted figure, optionally the supervisor's differing figure,
-- and the final approved figure that downstream money math (close lines,
-- expected collection) must use:
--
--   approved  — final = attendant's submission, supervisor agreed as-is.
--   corrected — final = supervisor's figure; reason mandatory.
--   rejected  — reserved for the rejection flow (reason mandatory).
--
-- One verification per meter reading (UNIQUE). A post-verification correction
-- of the underlying reading inserts a NEW active meter_readings row
-- (supersedes chain), which then needs its own verification — the shift
-- approval gate counts ACTIVE closing readings without one.

CREATE TABLE reading_verifications (
    id                          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id                  uuid NOT NULL,
    shift_id                    uuid NOT NULL,
    nozzle_id                   uuid NOT NULL,
    reading_id                  uuid NOT NULL,
    -- Dual-value snapshot, all numeric(14,3) like meter_readings.reading and
    -- bound from exact decimal strings ($N::numeric) — never a Go float.
    attendant_submitted_reading numeric(14, 3) NOT NULL,
    supervisor_verified_reading numeric(14, 3),
    final_approved_reading      numeric(14, 3) NOT NULL,
    status                      text NOT NULL,
    reason                      text,
    verified_by                 uuid NOT NULL,
    verified_at                 timestamptz NOT NULL DEFAULT now(),
    created_at                  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_reading_verifications_status
        CHECK (status IN ('approved', 'corrected', 'rejected')),
    -- A reason is mandatory whenever the supervisor diverges from the
    -- attendant's submission.
    CONSTRAINT chk_reading_verifications_reason
        CHECK (status = 'approved' OR reason IS NOT NULL),
    -- A corrected verification must carry the supervisor's figure, and the
    -- final figure must be whichever value the status says prevails.
    CONSTRAINT chk_reading_verifications_corrected
        CHECK (status <> 'corrected' OR supervisor_verified_reading IS NOT NULL),
    CONSTRAINT chk_reading_verifications_nonneg
        CHECK (attendant_submitted_reading >= 0 AND final_approved_reading >= 0),

    CONSTRAINT reading_verifications_shift_fk
        FOREIGN KEY (tenant_id, station_id, shift_id)
        REFERENCES shifts(tenant_id, station_id, id) ON DELETE RESTRICT,
    CONSTRAINT reading_verifications_nozzle_fk
        FOREIGN KEY (tenant_id, nozzle_id) REFERENCES nozzles(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT reading_verifications_reading_fk
        FOREIGN KEY (tenant_id, reading_id) REFERENCES meter_readings(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT reading_verifications_verified_by_fk
        FOREIGN KEY (tenant_id, verified_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,

    -- Exactly one verification per meter reading.
    CONSTRAINT uq_reading_verifications_reading UNIQUE (tenant_id, reading_id)
);

CREATE INDEX idx_reading_verifications_tenant ON reading_verifications(tenant_id);
CREATE INDEX idx_reading_verifications_shift  ON reading_verifications(shift_id);

ALTER TABLE reading_verifications ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON reading_verifications
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Verification reuses the station-scoped reading.override permission (0021):
-- it is the same supervisor authority over a station's readings, now recorded
-- in the dual-value model instead of overwriting. No new permission is added.
