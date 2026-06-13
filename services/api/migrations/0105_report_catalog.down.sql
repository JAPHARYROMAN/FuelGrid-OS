-- Reverse 0105_report_catalog. Drop the catalog tables (their RLS policies,
-- indexes and triggers fall with them), then remove the reports.read grants and
-- the permission itself so the schema returns to its pre-0105 state.
DROP TABLE IF EXISTS reports;
DROP TABLE IF EXISTS report_categories;

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'reports.read');
DELETE FROM permissions WHERE code = 'reports.read';
