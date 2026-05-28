-- 0022_assignment_integrity: enforce nozzle assignments reference a real
-- shift attendant at the DB level (Phase 3 audit P2).
--
-- 0015 only FK'd shift_nozzle_assignments.attendant_id to users, so a nozzle
-- could be assigned to someone not on the shift, and unassigning an attendant
-- left orphan nozzle rows. Tie the assignment's (shift, attendant) to a
-- shift_attendants row and cascade the cleanup on unassignment.

ALTER TABLE shift_attendants
    ADD CONSTRAINT uq_shift_attendants_tenant_shift_user UNIQUE (tenant_id, shift_id, user_id);

ALTER TABLE shift_nozzle_assignments
    ADD CONSTRAINT sna_attendant_on_shift_fk
    FOREIGN KEY (tenant_id, shift_id, attendant_id)
    REFERENCES shift_attendants (tenant_id, shift_id, user_id) ON DELETE CASCADE;
