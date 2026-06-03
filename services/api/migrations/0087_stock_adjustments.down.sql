-- Reverse of 0087_stock_adjustments. The stock.adjust / stock.approve_adjustment
-- permissions predate this migration (0004) and are not dropped here.

DROP TABLE IF EXISTS stock_adjustments;
