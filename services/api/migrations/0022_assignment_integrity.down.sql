-- Revert 0022.
ALTER TABLE shift_nozzle_assignments DROP CONSTRAINT sna_attendant_on_shift_fk;
ALTER TABLE shift_attendants DROP CONSTRAINT uq_shift_attendants_tenant_shift_user;
