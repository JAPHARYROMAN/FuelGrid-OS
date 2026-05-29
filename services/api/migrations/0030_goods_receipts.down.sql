-- Reverse of 0030_goods_receipts.

DROP INDEX IF EXISTS idx_stock_mvt_delivery_source_once;
DROP INDEX IF EXISTS idx_stock_mvt_purchase_order_id;
DROP INDEX IF EXISTS idx_stock_mvt_supplier_id;

ALTER TABLE stock_movements
    DROP CONSTRAINT IF EXISTS stock_mvt_po_fk,
    DROP CONSTRAINT IF EXISTS stock_mvt_supplier_fk,
    DROP CONSTRAINT IF EXISTS chk_stock_mvt_costs_nonnegative,
    DROP COLUMN IF EXISTS landed_cost_per_litre,
    DROP COLUMN IF EXISTS landed_cost_total,
    DROP COLUMN IF EXISTS purchase_order_id,
    DROP COLUMN IF EXISTS supplier_id;

DROP INDEX IF EXISTS idx_deliveries_po_line_id;
DROP INDEX IF EXISTS idx_deliveries_purchase_order_id;
DROP INDEX IF EXISTS idx_deliveries_supplier_id;

ALTER TABLE deliveries
    DROP CONSTRAINT IF EXISTS deliveries_po_line_fk,
    DROP CONSTRAINT IF EXISTS deliveries_po_fk,
    DROP CONSTRAINT IF EXISTS deliveries_supplier_fk,
    DROP CONSTRAINT IF EXISTS chk_deliveries_match_status,
    DROP CONSTRAINT IF EXISTS chk_deliveries_costs_nonnegative,
    DROP COLUMN IF EXISTS quantity_variance_litres,
    DROP COLUMN IF EXISTS match_status,
    DROP COLUMN IF EXISTS landed_cost_per_litre,
    DROP COLUMN IF EXISTS landed_cost_total,
    DROP COLUMN IF EXISTS levies_amount,
    DROP COLUMN IF EXISTS duty_amount,
    DROP COLUMN IF EXISTS freight_amount,
    DROP COLUMN IF EXISTS line_unit_price,
    DROP COLUMN IF EXISTS po_line_id,
    DROP COLUMN IF EXISTS purchase_order_id,
    DROP COLUMN IF EXISTS supplier_id;
