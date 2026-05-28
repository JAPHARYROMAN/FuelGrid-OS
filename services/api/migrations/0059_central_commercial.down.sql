-- Reverse of 0059_central_commercial.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('central_pricing.manage', 'central_pricing.approve', 'central_pricing.publish', 'central_procurement.manage', 'central_procurement.release', 'stock_transfer.manage', 'stock_transfer.approve', 'stock_transfer.receive'));
DELETE FROM permissions WHERE code IN ('central_pricing.manage', 'central_pricing.approve', 'central_pricing.publish', 'central_procurement.manage', 'central_procurement.release', 'stock_transfer.manage', 'stock_transfer.approve', 'stock_transfer.receive');

DROP TABLE IF EXISTS stock_transfer_orders;
DROP TABLE IF EXISTS central_procurement_plan_lines;
DROP TABLE IF EXISTS central_procurement_plans;
DROP TABLE IF EXISTS central_price_rollouts;
