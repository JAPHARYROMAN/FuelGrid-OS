-- Reverse of 0015_shifts.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'shift.assign');
DELETE FROM permissions WHERE code = 'shift.assign';

DROP TABLE IF EXISTS shift_nozzle_assignments;
DROP TABLE IF EXISTS shift_attendants;
DROP TABLE IF EXISTS shifts;

ALTER TABLE nozzles DROP CONSTRAINT IF EXISTS uq_nozzles_tenant_id;
