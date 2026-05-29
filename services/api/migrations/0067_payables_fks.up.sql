-- 0067_payables_fks: add the missing referential integrity to the AP ledger (DB-001).
--
-- 0040 created payables with supplier_id / station_id / journal_entry_id as
-- bare uuids ("carried as a contract, scoped by tenant") on the most
-- fraud-sensitive table in the system — nothing stopped a payable from
-- referencing a non-existent or cross-tenant supplier, station, or journal
-- entry. The composite (tenant_id, …) targets already exist, so we can add
-- real FKs that also enforce tenant alignment. station_id / journal_entry_id
-- are nullable; a NULL component makes the composite FK (MATCH SIMPLE) skip
-- the check, so open payables not yet posted to the GL are unaffected.

ALTER TABLE payables
    ADD CONSTRAINT payables_supplier_fk
        FOREIGN KEY (tenant_id, supplier_id)
        REFERENCES suppliers (tenant_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT payables_station_fk
        FOREIGN KEY (tenant_id, station_id)
        REFERENCES stations (tenant_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT payables_journal_entry_fk
        FOREIGN KEY (tenant_id, journal_entry_id)
        REFERENCES journal_entries (tenant_id, id) ON DELETE RESTRICT;

CREATE INDEX idx_payables_station       ON payables (station_id)       WHERE station_id IS NOT NULL;
CREATE INDEX idx_payables_journal_entry ON payables (journal_entry_id) WHERE journal_entry_id IS NOT NULL;
