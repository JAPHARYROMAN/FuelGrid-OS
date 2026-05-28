-- Reverse of 0010_tanks.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'tanks.manage');
DELETE FROM permissions WHERE code = 'tanks.manage';

-- Dropping the table also drops its policy, trigger, indexes, and constraints.
DROP TABLE IF EXISTS tanks;
