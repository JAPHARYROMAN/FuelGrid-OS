-- 0073_user_session_epoch: per-user session epoch for authoritative revocation.
--
-- Session revocation (SEC-1 / AUTH-04) was best-effort: the only enforcement
-- was deleting the live Redis entry. If that delete was missed (Redis flake,
-- a replica lag, a session minted on another node) a "revoked" session kept
-- resolving until its TTL lapsed. There was no durable, authoritative signal.
--
-- session_epoch is that signal. Each session records the user's epoch at the
-- moment it was minted. A global revoke (password reset, password change,
-- "log out everywhere") bumps the user's epoch by one inside the same
-- transaction as the durable session revoke. On every protected request the
-- auth hot path compares the session's stamped epoch against the user's
-- current epoch in Postgres; a mismatch is an authoritative revocation that
-- no longer depends on Redis having been cleaned up.

ALTER TABLE users
    ADD COLUMN session_epoch integer NOT NULL DEFAULT 0;
