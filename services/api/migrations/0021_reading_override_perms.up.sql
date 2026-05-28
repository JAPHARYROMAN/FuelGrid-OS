-- 0021_reading_override_perms: separate attendant self-scope from supervisor
-- override (Phase 3 audit P1).
--
-- 0020 grants the attendant role reading.edit + cash.submit. Those stay, but
-- attendants are now self-scoped in-handler: they may only write against
-- shifts/nozzles/tanks they're assigned to. Supervisors and managers need to
-- write across a station's shifts (correct an attendant's reading, submit
-- cash for any shift), so they get dedicated override permissions that bypass
-- the assignment check while staying station-scoped.

INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('reading.override', 'Capture/correct meter or dip readings on any shift at a station', 'reading', true),
    ('cash.override',    'Submit cash for any shift at a station',                          'cash',    true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code IN ('reading.override', 'cash.override')
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
