-- 0068_journal_fks: referential integrity for journal station + reversal links
-- (DB-002, DB-003).
--
-- DB-002: journal_entries.station_id and journal_lines.station_id were bare
-- uuids — a journal could be tagged with a non-existent or cross-tenant
-- station, and the column was unindexed (slow per-station P&L). Add composite
-- (tenant_id, station_id) FKs to stations + supporting indexes.
--
-- DB-003: reverses_entry_id / reversed_by_entry_id (the correction chain) had
-- no FK, so a reversal link could dangle or point across tenants, undermining
-- the very audit trail reversals exist to provide. Add self-referential
-- composite FKs back to journal_entries(tenant_id, id). All four columns are
-- nullable; a NULL component makes the composite FK skip the check.

ALTER TABLE journal_entries
    ADD CONSTRAINT journal_entries_station_fk
        FOREIGN KEY (tenant_id, station_id)
        REFERENCES stations (tenant_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT journal_entries_reverses_fk
        FOREIGN KEY (tenant_id, reverses_entry_id)
        REFERENCES journal_entries (tenant_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT journal_entries_reversed_by_fk
        FOREIGN KEY (tenant_id, reversed_by_entry_id)
        REFERENCES journal_entries (tenant_id, id) ON DELETE RESTRICT;

ALTER TABLE journal_lines
    ADD CONSTRAINT journal_lines_station_fk
        FOREIGN KEY (tenant_id, station_id)
        REFERENCES stations (tenant_id, id) ON DELETE RESTRICT;

CREATE INDEX idx_journal_entries_station     ON journal_entries (station_id)           WHERE station_id IS NOT NULL;
CREATE INDEX idx_journal_entries_reverses    ON journal_entries (reverses_entry_id)    WHERE reverses_entry_id IS NOT NULL;
CREATE INDEX idx_journal_entries_reversed_by ON journal_entries (reversed_by_entry_id) WHERE reversed_by_entry_id IS NOT NULL;
CREATE INDEX idx_journal_lines_station       ON journal_lines (station_id)             WHERE station_id IS NOT NULL;
