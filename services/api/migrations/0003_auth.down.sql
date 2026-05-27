DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS devices;

ALTER TABLE users
    DROP COLUMN IF EXISTS locked_until,
    DROP COLUMN IF EXISTS failed_login_count,
    DROP COLUMN IF EXISTS last_login_at,
    DROP COLUMN IF EXISTS mfa_enabled,
    DROP COLUMN IF EXISTS mfa_secret,
    DROP COLUMN IF EXISTS password_changed_at,
    DROP COLUMN IF EXISTS password_hash;
