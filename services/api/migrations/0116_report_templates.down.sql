-- 0116_report_templates (down): drop the Custom Report Builder's saved templates
-- cleanly and restore the catalog to its pre-Phase-11 state. The dataset registry
-- + safe composer live entirely in Go (internal/reportbuilder) and never depended
-- on this table, so reverting here only removes saved templates + the permission.

-- Remove the catalog rows for the builder + revert the Custom category to the
-- placeholder it was seeded as in 0105.
DELETE FROM reports
    WHERE tenant_id IS NULL AND key IN ('builder-datasets', 'builder-templates');

UPDATE report_categories
   SET availability = 'placeholder',
       required_permission = 'reports.read',
       target_route = '/reports/custom',
       updated_at = now()
 WHERE tenant_id IS NULL AND key = 'custom';

DROP TABLE IF EXISTS report_templates;

-- Revoke the management permission. role_permissions.permission_id is ON DELETE
-- RESTRICT, so the grants must be removed first, then the permission row.
DELETE FROM role_permissions
    WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'reports.builder');
DELETE FROM permissions WHERE code = 'reports.builder';
