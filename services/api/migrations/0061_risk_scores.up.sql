-- 0061_risk_scores: explainable per-entity risk scores (Phase 10, Stage 9).
-- A score is derived from a dimension's open alerts (severity-weighted) with a
-- stored component breakdown, so a score change is always explainable. Scores
-- are advisory, not punitive automation.

CREATE TABLE risk_scores (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    dimension   text NOT NULL,
    entity_id   uuid NOT NULL,
    score       integer NOT NULL DEFAULT 0,
    band        text NOT NULL DEFAULT 'low',
    open_alerts integer NOT NULL DEFAULT 0,
    components  jsonb NOT NULL DEFAULT '{}'::jsonb,
    computed_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_risk_score_band CHECK (band IN ('low', 'watch', 'elevated', 'high', 'critical'))
);
CREATE UNIQUE INDEX uq_risk_score_entity ON risk_scores(tenant_id, dimension, entity_id);
CREATE INDEX idx_risk_scores_tenant ON risk_scores(tenant_id, score DESC);
ALTER TABLE risk_scores ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON risk_scores
    USING (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('risk_score.admin', 'Recompute risk scores', 'risk', false);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'risk_score.admin'
  AND r.code IN ('system_admin', 'executive')
ON CONFLICT (role_id, permission_id) DO NOTHING;
