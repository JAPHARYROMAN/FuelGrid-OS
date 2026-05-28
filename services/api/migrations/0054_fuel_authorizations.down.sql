-- Reverse of 0054_fuel_authorizations.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('fuel_authorization.create', 'fuel_authorization.cancel', 'fuel_authorization.override', 'fuel_limit.manage'));
DELETE FROM permissions WHERE code IN ('fuel_authorization.create', 'fuel_authorization.cancel', 'fuel_authorization.override', 'fuel_limit.manage');

DROP TABLE IF EXISTS fuel_authorization_denials;
DROP TABLE IF EXISTS fuel_authorizations;
DROP TABLE IF EXISTS fuel_limits;
