-- Revert 0069_stock_movements_immutable.
DROP TRIGGER IF EXISTS stock_movements_immutable ON stock_movements;
DROP FUNCTION IF EXISTS assert_stock_movement_immutable();
