-- Revert 0103_attendant_reporting.

DELETE FROM role_permissions rp
USING permissions p
WHERE rp.permission_id = p.id AND p.code = 'incidents.report';

DELETE FROM permissions WHERE code = 'incidents.report';

DROP INDEX IF EXISTS uq_incidents_tenant_dedupe_key;
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS chk_incidents_dedupe_key_len;
ALTER TABLE incidents DROP COLUMN IF EXISTS dedupe_key;

-- Restore the pre-0103 type vocabulary. Remap any rows created with the
-- attendant issue types first so the narrower CHECK can be re-added.
UPDATE incidents SET type = 'equipment' WHERE type IN ('pump', 'nozzle', 'meter');
UPDATE incidents SET type = 'other'     WHERE type = 'payment';
ALTER TABLE incidents DROP CONSTRAINT chk_incidents_type;
ALTER TABLE incidents
    ADD CONSTRAINT chk_incidents_type CHECK (
        type IN ('equipment', 'leak', 'variance', 'safety', 'calibration', 'other')
    );

DROP INDEX IF EXISTS uq_notifications_event_target;
ALTER TABLE notifications DROP COLUMN IF EXISTS source_event_id;
