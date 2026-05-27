DROP POLICY IF EXISTS tenant_or_platform ON outbox_events;
DROP POLICY IF EXISTS tenant_or_platform ON audit_logs;

DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS audit_logs;
