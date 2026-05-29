-- Reverse of 0029_purchase_orders.

DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('purchase_order.read', 'purchase_order.manage', 'purchase_order.approve')
);
DELETE FROM permissions
WHERE code IN ('purchase_order.read', 'purchase_order.manage', 'purchase_order.approve');

DROP TABLE IF EXISTS purchase_order_lines;
DROP TABLE IF EXISTS purchase_orders;
