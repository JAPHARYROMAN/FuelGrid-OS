-- Reverse of 0011_pumps_nozzles.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'pumps.manage');
DELETE FROM permissions WHERE code = 'pumps.manage';

-- nozzles first: it FKs onto pumps (and tanks). Dropping each table also
-- drops its policy, trigger, indexes, and constraints.
DROP TABLE IF EXISTS nozzles;
DROP TABLE IF EXISTS pumps;
