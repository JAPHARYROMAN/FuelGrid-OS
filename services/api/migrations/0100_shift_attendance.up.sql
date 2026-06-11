-- 0100_shift_attendance: attendant check-in/out + nozzle-assignment
-- confirmation (Mobile Attendant App, Phase 0).
--
-- shift_attendance records when each rostered attendant physically checked in
-- to (and out of) a shift, with optional device info from the mobile client.
-- One row per attendant per shift (UNIQUE), flipped checked_in -> checked_out
-- in place; the check-in/out instants are immutable once stamped (the API only
-- ever sets check_out_at once).
--
-- shift_nozzle_assignments.confirmed_at is the attendant's own acknowledgement
-- of the nozzle they were handed. A reassignment (delete + recreate, the only
-- write path) naturally yields a fresh row with confirmed_at NULL, so a
-- reassigned nozzle always requires a fresh confirmation.

CREATE TABLE shift_attendance (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id   uuid NOT NULL,
    shift_id     uuid NOT NULL,
    attendant_id uuid NOT NULL,
    status       text NOT NULL DEFAULT 'checked_in',
    check_in_at  timestamptz NOT NULL DEFAULT now(),
    check_out_at timestamptz,
    device_info  jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_shift_attendance_status
        CHECK (status IN ('checked_in', 'checked_out')),
    -- A checked-out row must carry its check-out instant (and vice versa).
    CONSTRAINT chk_shift_attendance_out
        CHECK ((status = 'checked_out') = (check_out_at IS NOT NULL)),

    -- Tenant-bound composite FKs mirror the sibling shift tables; the
    -- station_id leg rides 0023's (tenant_id, station_id, id) key so an
    -- attendance row can never claim a different station than its shift.
    CONSTRAINT shift_attendance_shift_fk
        FOREIGN KEY (tenant_id, station_id, shift_id)
        REFERENCES shifts(tenant_id, station_id, id) ON DELETE CASCADE,
    CONSTRAINT shift_attendance_attendant_fk
        FOREIGN KEY (tenant_id, attendant_id) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,

    -- One attendance record per attendant per shift; duplicate check-ins are
    -- idempotent at the API.
    CONSTRAINT uq_shift_attendance UNIQUE (tenant_id, shift_id, attendant_id)
);

CREATE INDEX idx_shift_attendance_tenant_id ON shift_attendance(tenant_id);
CREATE INDEX idx_shift_attendance_shift_id  ON shift_attendance(shift_id);
CREATE INDEX idx_shift_attendance_attendant ON shift_attendance(attendant_id);

CREATE TRIGGER shift_attendance_set_updated_at
    BEFORE UPDATE ON shift_attendance
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE shift_attendance ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON shift_attendance
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Assignment confirmation: NULL until the assigned attendant confirms.
ALTER TABLE shift_nozzle_assignments ADD COLUMN confirmed_at timestamptz;

-- Check-in/out and confirmation are SELF-scoped attendant actions (the actor
-- must be on the shift / be the assignee, enforced in-handler); the attendance
-- list rides station.read. No new permission is needed.
