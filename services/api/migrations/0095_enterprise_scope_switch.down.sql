-- Reverse 0093_enterprise_scope_switch. role_permissions.permission_id is
-- ON DELETE RESTRICT, so the grant rows must be removed before the permission
-- itself.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'enterprise.scope.switch');

DELETE FROM permissions WHERE code = 'enterprise.scope.switch';
