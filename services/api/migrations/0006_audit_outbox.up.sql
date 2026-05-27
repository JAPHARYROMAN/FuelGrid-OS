-- 0006_audit_outbox: append-only audit log + transactional outbox.
--
-- Both tables follow the "append-only" convention documented in
-- docs/db-conventions.md: rows are never UPDATEd in the normal flow.
-- (The publisher does mutate outbox_events.published_at — that's the
-- single intentional exception, gated by FOR UPDATE SKIP LOCKED.)

-- ---------------------------------------------------------------------------
-- audit_logs — every sensitive action emits one row.
-- tenant_id is nullable for platform-level events that have no tenant
-- context (e.g., creation of a tenant itself).
-- actor_id uses ON DELETE SET NULL because audit trails must outlive
-- the users they describe.
-- ---------------------------------------------------------------------------
CREATE TABLE audit_logs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid REFERENCES tenants(id) ON DELETE RESTRICT,
    actor_id        uuid REFERENCES users(id)   ON DELETE SET NULL,
    action          text NOT NULL,
    entity_type     text NOT NULL,
    entity_id       text,
    previous_value  jsonb,
    new_value       jsonb,
    reason          text,
    ip              inet,
    user_agent      text,
    request_id      text,
    occurred_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_tenant_id   ON audit_logs(tenant_id);
CREATE INDEX idx_audit_logs_actor_id    ON audit_logs(actor_id);
CREATE INDEX idx_audit_logs_entity      ON audit_logs(entity_type, entity_id);
CREATE INDEX idx_audit_logs_occurred_at ON audit_logs(occurred_at);
CREATE INDEX idx_audit_logs_action      ON audit_logs(action);

-- ---------------------------------------------------------------------------
-- outbox_events — transactional outbox for domain events.
-- Written in the same DB transaction as the business change. A
-- background publisher polls unpublished rows and dispatches them.
--
-- The partial index on (occurred_at) WHERE published_at IS NULL is the
-- publisher's hot path. We rely on FOR UPDATE SKIP LOCKED to let many
-- publisher replicas run safely.
-- ---------------------------------------------------------------------------
CREATE TABLE outbox_events (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid REFERENCES tenants(id) ON DELETE RESTRICT,
    event_type      text    NOT NULL,
    event_version   integer NOT NULL DEFAULT 1,
    aggregate_type  text    NOT NULL,
    aggregate_id    text    NOT NULL,
    actor_id        uuid REFERENCES users(id) ON DELETE SET NULL,
    payload         jsonb   NOT NULL,
    metadata        jsonb,
    occurred_at     timestamptz NOT NULL DEFAULT now(),
    published_at    timestamptz,
    correlation_id  text,
    causation_id    text
);

CREATE INDEX idx_outbox_events_unpublished
    ON outbox_events(occurred_at) WHERE published_at IS NULL;
CREATE INDEX idx_outbox_events_tenant
    ON outbox_events(tenant_id);
CREATE INDEX idx_outbox_events_aggregate
    ON outbox_events(aggregate_type, aggregate_id);

-- ---------------------------------------------------------------------------
-- RLS. Both tables are tenant-scoped (with NULL tenant_id allowed for
-- platform events). Policy mirrors the `tenant_or_system` shape used by
-- `roles` in 0005_rls.
-- ---------------------------------------------------------------------------
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_or_platform ON audit_logs
    USING      (tenant_id IS NULL OR tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id IS NULL OR tenant_id::text = current_setting('app.current_tenant', true));

ALTER TABLE outbox_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_or_platform ON outbox_events
    USING      (tenant_id IS NULL OR tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id IS NULL OR tenant_id::text = current_setting('app.current_tenant', true));
