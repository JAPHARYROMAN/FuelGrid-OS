-- Reverse 0080_user_mfa.

DROP POLICY IF EXISTS tenant_isolation ON user_mfa;
DROP INDEX IF EXISTS idx_user_mfa_tenant_id;
DROP TABLE IF EXISTS user_mfa;
