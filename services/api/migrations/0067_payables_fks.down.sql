-- Revert 0067_payables_fks.
DROP INDEX IF EXISTS idx_payables_journal_entry;
DROP INDEX IF EXISTS idx_payables_station;

ALTER TABLE payables
    DROP CONSTRAINT IF EXISTS payables_journal_entry_fk,
    DROP CONSTRAINT IF EXISTS payables_station_fk,
    DROP CONSTRAINT IF EXISTS payables_supplier_fk;
