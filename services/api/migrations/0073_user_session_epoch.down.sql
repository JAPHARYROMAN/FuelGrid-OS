-- Revert 0073_user_session_epoch: drop the per-user session epoch column.

ALTER TABLE users
    DROP COLUMN IF EXISTS session_epoch;
