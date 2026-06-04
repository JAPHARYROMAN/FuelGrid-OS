-- 0092_notification_preferences (Feature 11.1): per-user notification delivery
-- preferences backing the notification CENTER's settings sub-page.
--
-- The notification FEED (0078) is self-service: every authenticated user reads
-- and marks their own feed with no extra permission beyond a session. These
-- preferences are the same trust model — a row is keyed to (tenant_id, user_id)
-- and a user may only read/write their OWN preferences. There is therefore no
-- new permission code; the routes mount inside the self-service group and scope
-- every query to the caller's user id.
--
-- One row per (user, category, channel). `enabled` is the per-toggle switch the
-- settings UI flips; quiet_hours_start / quiet_hours_end are an OPTIONAL local
-- quiet window (HH:MM, 24h) during which the channel should suppress delivery —
-- both NULL means "no quiet hours". The category/channel value sets are open
-- text validated in the application layer (the notification taxonomy evolves
-- with the subscriber), kept non-empty by CHECK so a blank key cannot slip in.

CREATE TABLE notification_preferences (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    user_id           uuid NOT NULL,
    -- e.g. 'revenue', 'risk', 'incident', 'approval', 'shift'.
    category          text NOT NULL,
    -- e.g. 'in_app', 'email'.
    channel           text NOT NULL,
    enabled           boolean NOT NULL DEFAULT true,
    -- Optional local quiet window (HH:MM 24h). Both NULL = no quiet hours.
    quiet_hours_start text,
    quiet_hours_end   text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_notif_pref_category CHECK (length(btrim(category)) > 0),
    CONSTRAINT chk_notif_pref_channel  CHECK (length(btrim(channel)) > 0),
    CONSTRAINT chk_notif_pref_quiet_start
        CHECK (quiet_hours_start IS NULL OR quiet_hours_start ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    CONSTRAINT chk_notif_pref_quiet_end
        CHECK (quiet_hours_end IS NULL OR quiet_hours_end ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    -- A quiet window is set as a pair: either both bounds or neither.
    CONSTRAINT chk_notif_pref_quiet_pair
        CHECK ((quiet_hours_start IS NULL) = (quiet_hours_end IS NULL)),
    -- The notification belongs to a user IN this tenant (mirrors notifications).
    CONSTRAINT notification_preferences_user_fk
        FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE
);

-- One preference per (user, category, channel); the upsert keys on it.
CREATE UNIQUE INDEX uq_notification_preferences
    ON notification_preferences(tenant_id, user_id, category, channel);
-- The hot read: a user's full preference set.
CREATE INDEX idx_notification_preferences_user
    ON notification_preferences(tenant_id, user_id);

CREATE TRIGGER notification_preferences_set_updated_at
    BEFORE UPDATE ON notification_preferences
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE notification_preferences ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON notification_preferences
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));
