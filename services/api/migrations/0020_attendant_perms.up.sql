-- 0020_attendant_perms: expand the attendant role for Phase-3 workflows.
--
-- 0004 seeded the attendant role intentionally minimal (shift.open only),
-- noting it would grow "when pump/shift workflows arrive". They've arrived:
-- the attendant "My Shift" console needs attendants to record meter/dip
-- readings (reading.edit) and submit cash. cash.submit was already granted
-- to attendant in 0018, so ON CONFLICT keeps this idempotent.

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND r.code = 'attendant'
  AND p.code IN ('reading.edit', 'cash.submit')
ON CONFLICT (role_id, permission_id) DO NOTHING;
