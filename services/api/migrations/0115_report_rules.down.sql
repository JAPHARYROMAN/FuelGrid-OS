-- 0115_report_rules (down): drop the report-insight rules engine cleanly. The
-- composers in internal/reporting are unaffected (they never depended on this
-- table), so report insight output reverts to exactly the pre-Phase-15 default.

DROP TABLE IF EXISTS report_rules;

-- Revoke the management permission. role_permissions.permission_id is ON DELETE
-- RESTRICT, so the grants must be removed first, then the permission row.
DELETE FROM role_permissions
    WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'reports.rules.manage');
DELETE FROM permissions WHERE code = 'reports.rules.manage';
