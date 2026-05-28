-- 0058_enterprise_projections: enterprise read models (Phase 9, Stages 4-6).
-- station_daily_kpis is an idempotent projection rebuilt from posted Phase-6
-- revenue days; dashboards and station ranking read from it. Projection state
-- tracks freshness so the UI can show lag.

CREATE TABLE station_daily_kpis (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id    uuid NOT NULL,
    business_date date NOT NULL,
    gross_revenue numeric(14, 2) NOT NULL DEFAULT 0,
    net_revenue   numeric(14, 2) NOT NULL DEFAULT 0,
    margin_total  numeric(14, 2) NOT NULL DEFAULT 0,
    cogs_total    numeric(14, 2) NOT NULL DEFAULT 0,
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT sdk_station_fk FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX uq_sdk_station_day ON station_daily_kpis(tenant_id, station_id, business_date);
CREATE INDEX idx_sdk_tenant ON station_daily_kpis(tenant_id, business_date);
ALTER TABLE station_daily_kpis ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON station_daily_kpis
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

CREATE TABLE enterprise_projection_state (
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    projection_type text NOT NULL,
    last_rebuilt_at timestamptz NOT NULL DEFAULT now(),
    row_count       integer NOT NULL DEFAULT 0,

    PRIMARY KEY (tenant_id, projection_type)
);
ALTER TABLE enterprise_projection_state ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON enterprise_projection_state
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('enterprise_projection.admin', 'Rebuild enterprise projections', 'enterprise', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'enterprise_projection.admin'
  AND r.code IN ('system_admin', 'executive')
ON CONFLICT (role_id, permission_id) DO NOTHING;
