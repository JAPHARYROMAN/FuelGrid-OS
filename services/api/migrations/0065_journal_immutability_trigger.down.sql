-- Reverse of 0065_journal_immutability_trigger.

DROP TRIGGER IF EXISTS journal_lines_immutable ON journal_lines;
DROP TRIGGER IF EXISTS journal_entries_immutable ON journal_entries;
DROP FUNCTION IF EXISTS assert_journal_line_immutable();
DROP FUNCTION IF EXISTS assert_journal_entry_immutable();
