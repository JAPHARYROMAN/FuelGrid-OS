-- 0077_workforce: employees, shift teams, and the daily 3-team rotation.
--
-- A station's workforce is split into exactly THREE teams (rotation_order
-- 0,1,2). Each day two teams work (one the Morning slot, one the Evening slot)
-- and the third rests; the assignment advances by one position every day, on a
-- 3-day cycle anchored at stations.rotation_anchor_date. Teams are assigned
-- once; the rotation is then computed deterministically (no stored roster).
--
-- A shift records which slot it covers and which team is on it; opening a shift
-- auto-populates its attendants from that team (the existing shift_attendants
-- table, for team members linked to a login user).

-- ---------------------------------------------------------------------------
-- employees — the station workforce. Optionally linked to a login `user`
-- (attendants who capture readings need one; back-office staff may not).
-- ---------------------------------------------------------------------------
CREATE TABLE employees (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id    uuid NOT NULL,
    user_id       uuid,
    full_name     text NOT NULL,
    role          text NOT NULL DEFAULT 'pump_attendant',
    employee_code text,
    phone         text,
    email         text,
    status        text NOT NULL DEFAULT 'active',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_employees_role
        CHECK (role IN ('pump_attendant', 'cashier', 'supervisor', 'manager', 'other')),
    CONSTRAINT chk_employees_status
        CHECK (status IN ('active', 'inactive')),
    CONSTRAINT employees_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT employees_user_fk
        FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE SET NULL
);

CREATE INDEX idx_employees_tenant_id  ON employees(tenant_id);
CREATE INDEX idx_employees_station_id ON employees(station_id);
-- At most one employee record per linked user.
CREATE UNIQUE INDEX uq_employees_user ON employees(tenant_id, user_id) WHERE user_id IS NOT NULL;
ALTER TABLE employees ADD CONSTRAINT uq_employees_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER employees_set_updated_at
    BEFORE UPDATE ON employees
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE employees ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON employees
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- shift_teams — exactly 3 per station, ordered 0..2 for the rotation.
-- ---------------------------------------------------------------------------
CREATE TABLE shift_teams (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id     uuid NOT NULL,
    name           text NOT NULL,
    rotation_order int  NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_shift_teams_order CHECK (rotation_order BETWEEN 0 AND 2),
    CONSTRAINT shift_teams_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_shift_teams_tenant_id  ON shift_teams(tenant_id);
CREATE INDEX idx_shift_teams_station_id ON shift_teams(station_id);
-- One team per rotation slot per station (enforces the 3-team model).
CREATE UNIQUE INDEX uq_shift_teams_order ON shift_teams(tenant_id, station_id, rotation_order);
ALTER TABLE shift_teams ADD CONSTRAINT uq_shift_teams_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER shift_teams_set_updated_at
    BEFORE UPDATE ON shift_teams
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE shift_teams ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON shift_teams
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- shift_team_members — which employees belong to a team. An employee belongs
-- to at most one team (within their tenant/station).
-- ---------------------------------------------------------------------------
CREATE TABLE shift_team_members (
    team_id     uuid NOT NULL,
    employee_id uuid NOT NULL,
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (team_id, employee_id),
    CONSTRAINT shift_team_members_team_fk
        FOREIGN KEY (tenant_id, team_id) REFERENCES shift_teams(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT shift_team_members_employee_fk
        FOREIGN KEY (tenant_id, employee_id) REFERENCES employees(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_shift_team_members_tenant_id ON shift_team_members(tenant_id);
-- An employee can be on only one team.
CREATE UNIQUE INDEX uq_shift_team_members_employee ON shift_team_members(tenant_id, employee_id);

ALTER TABLE shift_team_members ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON shift_team_members
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Rotation anchor on the station: cycle day 0 of the 3-team rotation.
-- ---------------------------------------------------------------------------
ALTER TABLE stations ADD COLUMN rotation_anchor_date date;

-- ---------------------------------------------------------------------------
-- Tie shifts to a rotation slot + the team on duty. Nullable so pre-existing
-- shifts (and stations not yet using rotation) remain valid.
-- ---------------------------------------------------------------------------
ALTER TABLE shifts ADD COLUMN slot    text;
ALTER TABLE shifts ADD COLUMN team_id uuid;

ALTER TABLE shifts ADD CONSTRAINT chk_shifts_slot
    CHECK (slot IS NULL OR slot IN ('morning', 'evening'));
ALTER TABLE shifts ADD CONSTRAINT shifts_team_fk
    FOREIGN KEY (tenant_id, team_id) REFERENCES shift_teams(tenant_id, id) ON DELETE SET NULL;

CREATE INDEX idx_shifts_team_id ON shifts(team_id);
