-- 0030_goods_receipts: evolve Phase-4 deliveries into priced goods receipts
-- (Phase 5, Stages 3-4).
--
-- The existing deliveries table remains the receipt table. Legacy rows stay
-- valid with NULL procurement/cost attribution; PO-backed receipts fill these
-- columns and still post exactly one stock_movements row with source_ref_type
-- = 'delivery'.

ALTER TABLE deliveries
    ADD COLUMN supplier_id              uuid,
    ADD COLUMN purchase_order_id        uuid,
    ADD COLUMN po_line_id               uuid,
    ADD COLUMN line_unit_price          numeric(14, 2),
    ADD COLUMN freight_amount           numeric(14, 2) NOT NULL DEFAULT 0,
    ADD COLUMN duty_amount              numeric(14, 2) NOT NULL DEFAULT 0,
    ADD COLUMN levies_amount            numeric(14, 2) NOT NULL DEFAULT 0,
    ADD COLUMN landed_cost_total        numeric(14, 2),
    ADD COLUMN landed_cost_per_litre    numeric(14, 4),
    ADD COLUMN match_status             text NOT NULL DEFAULT 'legacy',
    ADD COLUMN quantity_variance_litres numeric(14, 3);

ALTER TABLE deliveries
    ADD CONSTRAINT chk_deliveries_costs_nonnegative CHECK (
        (line_unit_price IS NULL OR line_unit_price >= 0)
        AND freight_amount >= 0
        AND duty_amount >= 0
        AND levies_amount >= 0
        AND (landed_cost_total IS NULL OR landed_cost_total >= 0)
        AND (landed_cost_per_litre IS NULL OR landed_cost_per_litre >= 0)
    ),
    ADD CONSTRAINT chk_deliveries_match_status CHECK (
        match_status IN ('legacy', 'matched', 'short', 'over', 'unmatched')
    ),
    ADD CONSTRAINT deliveries_supplier_fk
        FOREIGN KEY (tenant_id, supplier_id) REFERENCES suppliers(tenant_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT deliveries_po_fk
        FOREIGN KEY (tenant_id, purchase_order_id) REFERENCES purchase_orders(tenant_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT deliveries_po_line_fk
        FOREIGN KEY (tenant_id, purchase_order_id, po_line_id)
        REFERENCES purchase_order_lines(tenant_id, purchase_order_id, id) ON DELETE RESTRICT;

CREATE INDEX idx_deliveries_supplier_id ON deliveries(supplier_id) WHERE supplier_id IS NOT NULL;
CREATE INDEX idx_deliveries_purchase_order_id ON deliveries(purchase_order_id) WHERE purchase_order_id IS NOT NULL;
CREATE INDEX idx_deliveries_po_line_id ON deliveries(po_line_id) WHERE po_line_id IS NOT NULL;

ALTER TABLE stock_movements
    ADD COLUMN supplier_id           uuid,
    ADD COLUMN purchase_order_id     uuid,
    ADD COLUMN landed_cost_total     numeric(14, 2),
    ADD COLUMN landed_cost_per_litre numeric(14, 4);

ALTER TABLE stock_movements
    ADD CONSTRAINT chk_stock_mvt_costs_nonnegative CHECK (
        (landed_cost_total IS NULL OR landed_cost_total >= 0)
        AND (landed_cost_per_litre IS NULL OR landed_cost_per_litre >= 0)
    ),
    ADD CONSTRAINT stock_mvt_supplier_fk
        FOREIGN KEY (tenant_id, supplier_id) REFERENCES suppliers(tenant_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT stock_mvt_po_fk
        FOREIGN KEY (tenant_id, purchase_order_id) REFERENCES purchase_orders(tenant_id, id) ON DELETE RESTRICT;

CREATE INDEX idx_stock_mvt_supplier_id ON stock_movements(supplier_id) WHERE supplier_id IS NOT NULL;
CREATE INDEX idx_stock_mvt_purchase_order_id ON stock_movements(purchase_order_id) WHERE purchase_order_id IS NOT NULL;

-- Exactly one posted delivery movement may point at a delivery receipt.
CREATE UNIQUE INDEX idx_stock_mvt_delivery_source_once
    ON stock_movements(tenant_id, source_ref_id)
    WHERE movement_type = 'delivery'
      AND source_ref_type = 'delivery'
      AND source_ref_id IS NOT NULL
      AND supersedes_id IS NULL;
