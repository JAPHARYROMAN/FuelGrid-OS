-- Revert 0070_audit_logs_immutable.
DROP TRIGGER IF EXISTS audit_logs_immutable ON audit_logs;
DROP FUNCTION IF EXISTS assert_audit_log_immutable();
