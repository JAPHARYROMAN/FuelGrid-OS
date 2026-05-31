-- 0076_stock_transfer_product_alignment: DB-level guard for ENT-25.
--
-- ENT-25 is enforced in application code (ReceiveTransfer rejects a transfer
-- whose product does not match both tanks). This adds the defense-in-depth
-- database guard so the invariant holds even against direct SQL.
--
-- 0059 created stock_transfer_orders with separate FKs proving from_tank_id /
-- to_tank_id / product_id each belong to the tenant, but nothing tied the
-- transfer's product to the tanks' product. Here we add composite FKs that
-- require a transfer's product_id to equal the product_id of BOTH the source
-- and destination tank. The FK targets (tenant_id, id, product_id) on tanks —
-- 0010 only created UNIQUE (tenant_id, id) and UNIQUE
-- (tenant_id, id, station_id, product_id), so we add the needed
-- (tenant_id, id, product_id) unique first.
--
-- All columns involved are NOT NULL, so MATCH SIMPLE always evaluates the
-- check (no NULL-component skip). Existing data is aligned: seeds create no
-- transfers, and the Phase-9 integration test moves PMS between two PMS tanks.

ALTER TABLE tanks
    ADD CONSTRAINT uq_tanks_tenant_id_product UNIQUE (tenant_id, id, product_id);

ALTER TABLE stock_transfer_orders
    ADD CONSTRAINT sto_from_tank_product_fk
        FOREIGN KEY (tenant_id, from_tank_id, product_id)
        REFERENCES tanks (tenant_id, id, product_id) ON DELETE RESTRICT,
    ADD CONSTRAINT sto_to_tank_product_fk
        FOREIGN KEY (tenant_id, to_tank_id, product_id)
        REFERENCES tanks (tenant_id, id, product_id) ON DELETE RESTRICT;
