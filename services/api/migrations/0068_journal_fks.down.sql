-- Revert 0068_journal_fks.
DROP INDEX IF EXISTS idx_journal_lines_station;
DROP INDEX IF EXISTS idx_journal_entries_reversed_by;
DROP INDEX IF EXISTS idx_journal_entries_reverses;
DROP INDEX IF EXISTS idx_journal_entries_station;

ALTER TABLE journal_lines
    DROP CONSTRAINT IF EXISTS journal_lines_station_fk;

ALTER TABLE journal_entries
    DROP CONSTRAINT IF EXISTS journal_entries_reversed_by_fk,
    DROP CONSTRAINT IF EXISTS journal_entries_reverses_fk,
    DROP CONSTRAINT IF EXISTS journal_entries_station_fk;
