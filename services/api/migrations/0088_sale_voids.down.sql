-- Reverse of 0088_sale_voids.

DROP TABLE IF EXISTS sale_voids;

-- The sale.void.request / sale.void.approve permissions were introduced in this
-- migration. role_permissions.permission_id is ON DELETE RESTRICT, so the role
-- grants must be removed before the permissions themselves.
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE code IN ('sale.void.request', 'sale.void.approve')
);
DELETE FROM permissions WHERE code IN ('sale.void.request', 'sale.void.approve');
