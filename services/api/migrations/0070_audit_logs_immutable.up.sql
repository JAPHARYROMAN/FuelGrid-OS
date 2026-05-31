-- 0070_audit_logs_immutable: make the audit trail append-only at the database.
--
-- 0006 documents audit_logs as append-only ("rows are never UPDATEd in the
-- normal flow"), but only by convention — nothing stopped a bug, a new code
-- path, or a direct write from UPDATE-ing or DELETE-ing a logged action and
-- silently rewriting the record of who did what. An audit log that can be
-- edited after the fact is worthless for forensics or compliance. 0065 froze
-- the journal ledger and 0069 the stock ledger; this does the same for the
-- audit trail.
--
-- audit_logs are strictly append-only: no UPDATE is ever permitted (an audit
-- entry is never amended — it records what happened, immutably). DELETE is
-- blocked too, EXCEPT when current_setting('app.allow_ledger_delete', true) =
-- 'on' — the same escape hatch as 0065/0069, used only for deliberate,
-- whole-tenant teardown (the integration-test cleanup, and a future
-- tenant-offboarding/purge path). No ordinary application path sets it, so the
-- control holds in production. The check uses current_setting(..., true) so an
-- unset GUC reads NULL and fails closed (deletion blocked).

CREATE OR REPLACE FUNCTION assert_audit_log_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF current_setting('app.allow_ledger_delete', true) = 'on' THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'audit_logs are append-only: log % cannot be deleted', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    -- An audit entry is never amended; there is no permitted UPDATE.
    RAISE EXCEPTION 'audit_logs are immutable: log % cannot be modified', OLD.id
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_logs_immutable
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION assert_audit_log_immutable();
