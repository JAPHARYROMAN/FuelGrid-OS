-- 0064_journal_balance_trigger: enforce double-entry balance at the database
-- (audit ACCT-001).
--
-- Every journal entry must have total debits = total credits. Until now this
-- was enforced only in Go; a logic bug, a new code path, or a direct write
-- could persist an unbalanced entry and silently corrupt every financial
-- report (trial balance, P&L, balance sheet). A DEFERRABLE INITIALLY DEFERRED
-- constraint trigger re-checks each affected entry at COMMIT — after all of its
-- lines have been inserted within the posting transaction — and aborts the
-- transaction when debits <> credits. Deferral is essential: lines are inserted
-- one row at a time, so a non-deferred per-row check would fire mid-insert and
-- reject a legitimately balanced entry.

CREATE OR REPLACE FUNCTION assert_journal_entry_balanced() RETURNS trigger AS $$
DECLARE
    eid          uuid := COALESCE(NEW.journal_entry_id, OLD.journal_entry_id);
    total_debit  numeric(14, 2);
    total_credit numeric(14, 2);
BEGIN
    SELECT COALESCE(SUM(debit), 0), COALESCE(SUM(credit), 0)
      INTO total_debit, total_credit
      FROM journal_lines
     WHERE journal_entry_id = eid;

    -- An entry whose lines were all removed nets 0 = 0 and is fine; a real
    -- entry must balance to the cent.
    IF total_debit <> total_credit THEN
        RAISE EXCEPTION 'journal entry % is unbalanced: debits=% credits=%',
            eid, total_debit, total_credit
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER journal_lines_balanced
    AFTER INSERT OR UPDATE OR DELETE ON journal_lines
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION assert_journal_entry_balanced();
