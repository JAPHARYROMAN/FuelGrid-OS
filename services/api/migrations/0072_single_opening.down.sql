-- 0072_single_opening (down): drop the single-opening uniqueness guard.

DROP INDEX IF EXISTS uq_stock_mvt_one_opening;
