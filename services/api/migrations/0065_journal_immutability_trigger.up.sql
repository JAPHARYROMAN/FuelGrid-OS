-- 0065_journal_immutability_trigger: make the posted ledger append-only
-- (audit ACCT — "nothing prevents a later UPDATE journal_lines SET debit = ...").
--
-- 0064 guarantees a journal entry balances at COMMIT, but nothing stopped a
-- bug, a new code path, or a direct write from later editing or deleting a
-- posted entry or its lines — silently rewriting financial history and
-- breaking every report's drill-back to a cause. A real ledger is append-only:
-- corrections are made by posting a *reversing* entry, never by mutating the
-- original. These BEFORE UPDATE/DELETE triggers enforce that:
--
--   * journal_lines are frozen the moment they are inserted — no UPDATE, no
--     DELETE.
--   * journal_entries cannot be deleted, and the only UPDATE permitted is the
--     controlled posted -> reversed transition that accounting.ReverseEntry
--     performs (recording the reversing entry's id). A 'draft' entry — a state
--     no current code path produces, reserved for a future draft workflow —
--     remains mutable until it is posted.
--
-- Escape hatch: a session that sets `app.allow_ledger_delete = 'on'` may delete
-- ledger rows. This exists only for deliberate, whole-tenant teardown (the
-- integration-test cleanup, and a future tenant-offboarding/purge path). No
-- ordinary application path sets it, so the control holds in production. The
-- check uses current_setting(..., true) so an unset GUC reads NULL and fails
-- closed (deletion blocked).

CREATE OR REPLACE FUNCTION assert_journal_entry_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF current_setting('app.allow_ledger_delete', true) = 'on' THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'journal_entries are append-only: entry % cannot be deleted (post a reversing entry instead)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    -- A not-yet-posted draft is still editable.
    IF OLD.status = 'draft' THEN
        RETURN NEW;
    END IF;

    -- A posted entry may only transition to 'reversed', recording the id of the
    -- reversing entry. Every other column is frozen. updated_at is intentionally
    -- not compared: the set_updated_at trigger bumps it on this very UPDATE.
    IF OLD.status = 'posted'
       AND NEW.status = 'reversed'
       AND OLD.reversed_by_entry_id IS NULL
       AND NEW.reversed_by_entry_id IS NOT NULL
       AND NEW.id                = OLD.id
       AND NEW.entry_number      = OLD.entry_number
       AND NEW.tenant_id         = OLD.tenant_id
       AND NEW.period_id         = OLD.period_id
       AND NEW.entry_date        = OLD.entry_date
       AND NEW.source_type       = OLD.source_type
       AND NEW.source_id         IS NOT DISTINCT FROM OLD.source_id
       AND NEW.station_id        IS NOT DISTINCT FROM OLD.station_id
       AND NEW.memo              IS NOT DISTINCT FROM OLD.memo
       AND NEW.reverses_entry_id IS NOT DISTINCT FROM OLD.reverses_entry_id
       AND NEW.posted_by         = OLD.posted_by
       AND NEW.posted_at         = OLD.posted_at
       AND NEW.created_at        = OLD.created_at
    THEN
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'journal_entries are immutable once posted: entry % may only transition posted->reversed (post a reversing entry to correct it)', OLD.id
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

-- Fires before set_updated_at (alphabetical: "immutable" < "set_updated_at"),
-- so a rejected update aborts before any other before-trigger runs.
CREATE TRIGGER journal_entries_immutable
    BEFORE UPDATE OR DELETE ON journal_entries
    FOR EACH ROW EXECUTE FUNCTION assert_journal_entry_immutable();

CREATE OR REPLACE FUNCTION assert_journal_line_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF current_setting('app.allow_ledger_delete', true) = 'on' THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'journal_lines are append-only: line % cannot be deleted (post a reversing entry instead)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    -- No correction ever edits a line in place; it posts a reversing entry.
    RAISE EXCEPTION 'journal_lines are immutable: line % cannot be modified (post a reversing entry instead)', OLD.id
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER journal_lines_immutable
    BEFORE UPDATE OR DELETE ON journal_lines
    FOR EACH ROW EXECUTE FUNCTION assert_journal_line_immutable();
