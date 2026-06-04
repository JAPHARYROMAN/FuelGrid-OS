-- Reverse of 0093_opening_stock_requests. The stock.adjust /
-- stock.approve_adjustment permissions predate this migration (0004) and are not
-- dropped here.

DROP TABLE IF EXISTS opening_stock_requests;
