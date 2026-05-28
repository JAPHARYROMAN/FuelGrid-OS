-- Reverse of 0014_operating_days.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'operations.manage_day');
DELETE FROM permissions WHERE code = 'operations.manage_day';

DROP TABLE IF EXISTS operating_days;
