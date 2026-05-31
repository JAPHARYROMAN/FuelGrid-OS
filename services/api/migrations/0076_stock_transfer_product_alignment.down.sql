-- Revert 0076_stock_transfer_product_alignment.
ALTER TABLE stock_transfer_orders
    DROP CONSTRAINT IF EXISTS sto_to_tank_product_fk,
    DROP CONSTRAINT IF EXISTS sto_from_tank_product_fk;

ALTER TABLE tanks
    DROP CONSTRAINT IF EXISTS uq_tanks_tenant_id_product;
