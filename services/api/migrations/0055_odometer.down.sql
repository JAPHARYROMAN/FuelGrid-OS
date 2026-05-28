-- Reverse of 0055_odometer.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'fleet_report.read');
DELETE FROM permissions WHERE code = 'fleet_report.read';

DROP TABLE IF EXISTS vehicle_odometer_readings;
