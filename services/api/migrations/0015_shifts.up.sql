-- 0015_shifts: shifts + attendant/nozzle assignments (Phase 3, Stage 2).
--
-- A shift runs inside one operating day at one station. Attendants are
-- assigned to the shift, and nozzles are assigned to a specific attendant
-- so every later reading and sale traces to a person. Tenant-bound
-- composite FKs mirror the Phase-1/2 pattern.

-- FK targets needed by the child tables below.
ALTER TABLE nozzles ADD CONSTRAINT uq_nozzles_tenant_id UNIQUE (tenant_id, id);

CREATE TABLE shifts (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id       uuid NOT NULL,
    operating_day_id uuid NOT NULL,
    name             text NOT NULL,
    status           text NOT NULL DEFAULT 'open',
    opened_by        uuid NOT NULL,
    opened_at        timestamptz NOT NULL DEFAULT now(),
    closed_by        uuid,
    closed_at        timestamptz,
    approved_by      uuid,
    approved_at      timestamptz,
    notes            text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_shifts_status CHECK (status IN ('open', 'closed', 'approved')),

    CONSTRAINT shifts_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT shifts_day_fk
        FOREIGN KEY (tenant_id, operating_day_id) REFERENCES operating_days(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT shifts_opened_by_fk
        FOREIGN KEY (tenant_id, opened_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT shifts_closed_by_fk
        FOREIGN KEY (tenant_id, closed_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT shifts_approved_by_fk
        FOREIGN KEY (tenant_id, approved_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_shifts_tenant_id  ON shifts(tenant_id);
CREATE INDEX idx_shifts_station_id ON shifts(station_id);
CREATE INDEX idx_shifts_day_id     ON shifts(operating_day_id);
CREATE INDEX idx_shifts_status     ON shifts(status);

-- FK target for the assignment tables + Stage-3 readings.
ALTER TABLE shifts ADD CONSTRAINT uq_shifts_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER shifts_set_updated_at
    BEFORE UPDATE ON shifts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE shifts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON shifts
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- shift_attendants — who is working a shift.
-- ---------------------------------------------------------------------------
CREATE TABLE shift_attendants (
    shift_id    uuid NOT NULL,
    user_id     uuid NOT NULL,
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    assigned_by uuid NOT NULL,
    assigned_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (shift_id, user_id),
    CONSTRAINT shift_attendants_shift_fk
        FOREIGN KEY (tenant_id, shift_id) REFERENCES shifts(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT shift_attendants_user_fk
        FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT shift_attendants_assigned_by_fk
        FOREIGN KEY (tenant_id, assigned_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_shift_attendants_tenant_id ON shift_attendants(tenant_id);
CREATE INDEX idx_shift_attendants_user_id   ON shift_attendants(user_id);

ALTER TABLE shift_attendants ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON shift_attendants
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- shift_nozzle_assignments — which attendant runs which nozzle this shift.
-- At most one attendant per nozzle per shift (unique shift_id, nozzle_id).
-- ---------------------------------------------------------------------------
CREATE TABLE shift_nozzle_assignments (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    shift_id     uuid NOT NULL,
    nozzle_id    uuid NOT NULL,
    attendant_id uuid NOT NULL,
    assigned_by  uuid NOT NULL,
    assigned_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT sna_shift_fk
        FOREIGN KEY (tenant_id, shift_id) REFERENCES shifts(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT sna_nozzle_fk
        FOREIGN KEY (tenant_id, nozzle_id) REFERENCES nozzles(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sna_attendant_fk
        FOREIGN KEY (tenant_id, attendant_id) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sna_assigned_by_fk
        FOREIGN KEY (tenant_id, assigned_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_sna_tenant_id    ON shift_nozzle_assignments(tenant_id);
CREATE INDEX idx_sna_shift_id     ON shift_nozzle_assignments(shift_id);
CREATE INDEX idx_sna_attendant_id ON shift_nozzle_assignments(attendant_id);
CREATE UNIQUE INDEX uq_sna_shift_nozzle ON shift_nozzle_assignments(shift_id, nozzle_id);

ALTER TABLE shift_nozzle_assignments ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON shift_nozzle_assignments
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: shift.assign (station-scoped) for managing assignments.
-- shift.open / shift.close / shift.approve already exist from 0004.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('shift.assign', 'Assign attendants and nozzles on a shift', 'shift', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'shift.assign'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor');
