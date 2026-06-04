-- Reverse of 0090_retention.

DROP TABLE IF EXISTS closed_period_change_requests;
DROP TABLE IF EXISTS retention_policies;

-- The retention.manage / closed_period.change permissions were introduced in
-- this migration. role_permissions.permission_id is ON DELETE RESTRICT, so the
-- role grants must be removed before the permissions themselves.
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE code IN ('retention.manage', 'closed_period.change')
);
DELETE FROM permissions WHERE code IN ('retention.manage', 'closed_period.change');
