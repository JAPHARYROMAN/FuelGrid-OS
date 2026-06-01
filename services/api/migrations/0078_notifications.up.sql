-- 0078_notifications: in-app notification feed.
--
-- A notification is a per-user (or tenant-wide, when user_id IS NULL) message
-- surfaced in the topbar bell. Rows are created by the event subscriber when a
-- domain event of interest fires (revenue recognized, shift closed with
-- variance, risk alert raised, incident opened, approval requested) and read
-- back by the authenticated owner via the /notifications API.
--
-- read_at is NULL until the user marks the notification read; UnreadCount and
-- the unread filter key off it. related_entity_type / related_entity_id link a
-- notification back to the row that produced it (e.g. an incident) so the UI
-- can deep-link without a separate lookup.

CREATE TABLE notifications (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    -- NULL = tenant-wide (every user in the tenant sees it). When set, the
    -- notification is private to that user.
    user_id             uuid,
    type                text NOT NULL,
    title               text NOT NULL,
    body                text NOT NULL DEFAULT '',
    severity            text NOT NULL DEFAULT 'info',
    related_entity_type text,
    related_entity_id   text,
    read_at             timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_notifications_severity
        CHECK (severity IN ('info', 'success', 'warning', 'critical')),
    CONSTRAINT notifications_user_fk
        FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_notifications_tenant_id ON notifications(tenant_id);
-- The hot query: a user's feed (their own rows + tenant-wide rows), newest
-- first, optionally filtered to unread. Two partial/composite indexes keep both
-- the "all" and "unread-only" reads cheap.
CREATE INDEX idx_notifications_user_feed
    ON notifications(tenant_id, user_id, created_at DESC);
CREATE INDEX idx_notifications_user_unread
    ON notifications(tenant_id, user_id, created_at DESC)
    WHERE read_at IS NULL;

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notifications
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));
