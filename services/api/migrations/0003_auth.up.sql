-- 0003_auth: add credentials and MFA to users, create devices and sessions.

ALTER TABLE users
    ADD COLUMN password_hash        text,
    ADD COLUMN password_changed_at  timestamptz,
    ADD COLUMN mfa_secret           text,
    ADD COLUMN mfa_enabled          boolean NOT NULL DEFAULT false,
    ADD COLUMN last_login_at        timestamptz,
    ADD COLUMN failed_login_count   integer NOT NULL DEFAULT 0,
    ADD COLUMN locked_until         timestamptz;

-- ---------------------------------------------------------------------------
-- devices — a fingerprintable client (browser, attendant tablet, API client).
-- Lets us list "active sessions by device" in profile UIs and helps the risk
-- engine spot suspicious logins from new locations.
-- ---------------------------------------------------------------------------
CREATE TABLE devices (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    label         text,
    fingerprint   text,
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_devices_user_fingerprint UNIQUE (user_id, fingerprint)
);

CREATE INDEX idx_devices_user_id   ON devices(user_id);
CREATE INDEX idx_devices_tenant_id ON devices(tenant_id);

CREATE TRIGGER devices_set_updated_at
    BEFORE UPDATE ON devices
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- sessions — durable audit trail for every login. Redis is the hot path
-- (active sessions keyed by token); this table records issued/revoked
-- timestamps so we can answer "did this user have an active session at T?".
--
-- token_hash is sha256 of the raw token. The raw token only ever exists
-- client-side and (briefly) in the Redis key. Compromising this table does
-- not yield active sessions.
-- ---------------------------------------------------------------------------
CREATE TABLE sessions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash    bytea NOT NULL UNIQUE,
    user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    device_id     uuid REFERENCES devices(id) ON DELETE SET NULL,
    ip            inet,
    user_agent    text,
    issued_at     timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    revoked_at    timestamptz,
    revoke_reason text
);

CREATE INDEX idx_sessions_user_id    ON sessions(user_id);
CREATE INDEX idx_sessions_tenant_id  ON sessions(tenant_id);
CREATE INDEX idx_sessions_active     ON sessions(expires_at) WHERE revoked_at IS NULL;
