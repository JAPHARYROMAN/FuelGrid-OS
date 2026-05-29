-- Reverse of 0064_journal_balance_trigger.

DROP TRIGGER IF EXISTS journal_lines_balanced ON journal_lines;
DROP FUNCTION IF EXISTS assert_journal_entry_balanced();
