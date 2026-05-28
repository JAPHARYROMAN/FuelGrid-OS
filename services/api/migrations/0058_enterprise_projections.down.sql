-- Reverse of 0058_enterprise_projections.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'enterprise_projection.admin');
DELETE FROM permissions WHERE code = 'enterprise_projection.admin';

DROP TABLE IF EXISTS enterprise_projection_state;
DROP TABLE IF EXISTS station_daily_kpis;
