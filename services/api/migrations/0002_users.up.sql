-- 0002_users: bare-bones user table.
-- Auth fields (password_hash, mfa_secret, last_login_at, etc.) land in Stage 4.

CREATE TABLE users (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    email       text NOT NULL,
    full_name   text NOT NULL,
    status      text NOT NULL DEFAULT 'invited',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_users_status CHECK (status IN ('active', 'invited', 'suspended', 'deleted')),
    CONSTRAINT chk_users_email  CHECK (email ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$')
);

CREATE INDEX idx_users_tenant_id ON users(tenant_id);
CREATE UNIQUE INDEX idx_users_tenant_email
    ON users(tenant_id, lower(email)) WHERE status <> 'deleted';

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
