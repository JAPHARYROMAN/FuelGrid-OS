-- 0026_sales_idempotency: one sales movement per (tank, shift) (Phase 4,
-- Stage 4).
--
-- Approving a Phase-3 shift posts its metered litres-sold to the stock ledger
-- as 'sales' movements keyed to the shift (source_ref_type='shift',
-- source_ref_id=shift_id). This partial unique index is the hard backstop
-- that makes the posting idempotent: a re-approval or an outbox replay can
-- never create a second sales movement for the same tank and shift. Reversal
-- contras are excluded — they carry source_ref_type='correction'.

CREATE UNIQUE INDEX uq_stock_mvt_sales_per_shift_tank
    ON stock_movements (tank_id, source_ref_id)
    WHERE movement_type = 'sales' AND source_ref_type = 'shift';
