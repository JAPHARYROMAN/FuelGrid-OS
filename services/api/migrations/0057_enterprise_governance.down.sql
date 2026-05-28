-- Reverse of 0057_enterprise_governance.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('enterprise_structure.manage', 'enterprise_access.manage', 'enterprise_access.read', 'approval_policy.manage', 'approval_request.decide', 'enterprise.read'));
DELETE FROM permissions WHERE code IN ('enterprise_structure.manage', 'enterprise_access.manage', 'enterprise_access.read', 'approval_policy.manage', 'approval_request.decide', 'enterprise.read');

DROP TABLE IF EXISTS approval_decisions;
DROP TABLE IF EXISTS approval_requests;
DROP TABLE IF EXISTS approval_policies;
DROP TABLE IF EXISTS enterprise_scope_grants;
DROP TABLE IF EXISTS station_group_memberships;
DROP TABLE IF EXISTS station_groups;
