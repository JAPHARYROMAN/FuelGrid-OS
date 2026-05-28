-- 0014_operating_days: the business-day bucket (Phase 3, Stage 1).
--
-- Every later operational record (shift, reading, cash) hangs off an
-- operating day, so the station's work is grouped by a known business date
-- with an explicit open -> closed -> locked lifecycle. Tenant-bound
-- composite FKs mirror the Phase-1/2 pattern.

CREATE TABLE operating_days (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id    uuid NOT NULL,
    business_date date NOT NULL,
    status        text NOT NULL DEFAULT 'open',
    opened_by     uuid NOT NULL,
    opened_at     timestamptz NOT NULL DEFAULT now(),
    closed_by     uuid,
    closed_at     timestamptz,
    locked_by     uuid,
    locked_at     timestamptz,
    notes         text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_operating_days_status CHECK (status IN ('open', 'closed', 'locked')),

    CONSTRAINT operating_days_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT operating_days_opened_by_fk
        FOREIGN KEY (tenant_id, opened_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT operating_days_closed_by_fk
        FOREIGN KEY (tenant_id, closed_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT operating_days_locked_by_fk
        FOREIGN KEY (tenant_id, locked_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_operating_days_tenant_id  ON operating_days(tenant_id);
CREATE INDEX idx_operating_days_station_id ON operating_days(station_id);
CREATE INDEX idx_operating_days_date       ON operating_days(business_date);

-- At most one non-locked day per station per date. A locked day is history
-- and doesn't block re-opening that date later if ever needed.
CREATE UNIQUE INDEX idx_operating_days_active
    ON operating_days(station_id, business_date) WHERE status <> 'locked';

-- FK target for Stage-2 shifts.
ALTER TABLE operating_days ADD CONSTRAINT uq_operating_days_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER operating_days_set_updated_at
    BEFORE UPDATE ON operating_days
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE operating_days ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON operating_days
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: operations.manage_day is station-scoped. Reads ride station.read.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('operations.manage_day', 'Open, close, and lock station operating days', 'shift', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'operations.manage_day'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor');
