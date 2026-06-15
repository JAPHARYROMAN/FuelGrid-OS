-- Reverse 0114_scheduled_reports: drop the run-history + schedule tables, restore
-- the 'scheduled' catalog category to its placeholder seed, and remove the
-- reports.schedule permission grant + definition.

-- Restore the catalog category to its original placeholder state (0105 seed).
UPDATE report_categories
SET availability        = 'placeholder',
    required_permission = 'reports.read'
WHERE tenant_id IS NULL AND key = 'scheduled';

DROP TABLE IF EXISTS scheduled_report_runs;
DROP TABLE IF EXISTS scheduled_reports;

-- Remove the permission (and its role grants cascade via FK on role_permissions).
DELETE FROM role_permissions
    WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'reports.schedule');
DELETE FROM permissions WHERE code = 'reports.schedule';
