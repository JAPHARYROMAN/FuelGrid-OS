-- Reverse of 0012_tank_calibration.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'tanks.calibrate');
DELETE FROM permissions WHERE code = 'tanks.calibrate';

-- entries first (FK onto charts). Dropping each table also drops its
-- policy, trigger, indexes, and constraints.
DROP TABLE IF EXISTS tank_calibration_entries;
DROP TABLE IF EXISTS tank_calibration_charts;
