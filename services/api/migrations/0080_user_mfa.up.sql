-- 0080_user_mfa: one-time backup recovery codes for TOTP MFA (AUTH MFA).
--
-- The TOTP seed itself continues to live on users.mfa_secret (encrypted at
-- rest, AUTH-13) with users.mfa_enabled as the authoritative enabled flag —
-- the login hot path and enroll/verify flow already read those columns. This
-- table is the companion store the secret-on-users design lacked: a per-user
-- set of HASHED, single-use backup codes plus an enabled_at audit timestamp,
-- so a user who loses their authenticator can still complete the second factor.
--
-- Backup codes are stored ONLY as Argon2id hashes (never plaintext): the
-- plaintext set is shown to the user exactly once at generation time. Each code
-- is consumed at most once — VerifyLogin deletes the matched hash — so a code
-- replay is rejected. used_count / generated_at give the profile UI enough to
-- tell the user how many codes remain without revealing them.

CREATE TABLE user_mfa (
    user_id       uuid PRIMARY KEY,
    tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    -- Argon2id hashes of the single-use recovery codes. A consumed code is
    -- removed from the array; an empty array means "no unused codes left".
    backup_codes  text[] NOT NULL DEFAULT '{}',
    -- When the current backup-code set was generated, for the "regenerate"
    -- prompt and audit. NULL until the first set is issued.
    generated_at  timestamptz,
    -- Mirror of users.mfa_enabled activation time, for audit/display.
    enabled_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT user_mfa_user_fk
        FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_user_mfa_tenant_id ON user_mfa(tenant_id);

-- Tenant isolation under RLS: the request-scoped fuelgrid_app role only ever
-- sees rows for the tenant bound on the connection (app.current_tenant). The
-- owner/migration role bypasses, as everywhere.
ALTER TABLE user_mfa ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON user_mfa
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));
