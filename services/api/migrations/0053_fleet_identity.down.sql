-- Reverse of 0053_fleet_identity.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('fuel_credential.manage', 'fuel_credential.issue', 'fuel_credential.revoke'));
DELETE FROM permissions WHERE code IN ('fuel_credential.manage', 'fuel_credential.issue', 'fuel_credential.revoke');

DROP TABLE IF EXISTS fuel_credentials;
DROP TABLE IF EXISTS customer_drivers;
DROP TABLE IF EXISTS customer_vehicles;
