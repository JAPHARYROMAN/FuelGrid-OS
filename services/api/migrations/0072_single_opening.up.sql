-- 0072_single_opening: enforce AT MOST ONE genuine opening movement per tank
-- at the database (INV-010 / FIN-6).
--
-- A tank's ledger is seeded by exactly one 'opening' movement; every flow
-- (delivery/sales/transfer) and adjustment posts on top of it, and book stock
-- is the sum of the ledger (0024). SetOpeningBalance guarded against a second
-- opening with a check-then-act (SELECT EXISTS … then INSERT), but that races:
-- two concurrent opening requests for the same tank both read "no opening" and
-- both insert, double-counting the opening litres in every reconciliation that
-- derives from the ledger. Nothing at the database stopped it.
--
-- This partial unique index is the real enforcement: at most one row per
-- (tenant_id, tank_id) may be a posted, genuine opening. The predicate mirrors
-- inventory.hasOpeningPredicate exactly so the two agree on what "an opening"
-- is — it deliberately EXCLUDES the reversal contra of an opening
-- (source_ref_type = 'correction'), which also carries movement_type='opening'
-- and status='posted'. Without that exclusion, reversing an opening (a legit
-- correction path) would collide with its own original, and re-seeding after a
-- reversal would be impossible. tenant_id is part of the key so the constraint
-- is naturally tenant-scoped.
--
-- The seed (services/api/cmd/seed) inserts no stock_movements, so no existing
-- data violates this — the index builds cleanly on migrate-up.

CREATE UNIQUE INDEX uq_stock_mvt_one_opening
    ON stock_movements (tenant_id, tank_id)
    WHERE movement_type = 'opening'
      AND status = 'posted'
      AND (source_ref_type IS NULL OR source_ref_type <> 'correction');
