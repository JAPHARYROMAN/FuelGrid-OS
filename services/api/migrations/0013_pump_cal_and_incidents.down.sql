-- Reverse of 0013_pump_cal_and_incidents.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('pumps.calibrate', 'incidents.manage'));
DELETE FROM permissions WHERE code IN ('pumps.calibrate', 'incidents.manage');

DROP TABLE IF EXISTS incidents;
DROP TABLE IF EXISTS pump_calibrations;
