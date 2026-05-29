-- 0069_stock_movements_immutable: make the per-tank stock ledger append-only
-- at the database (INV-002).
--
-- 0024 documents the stock ledger as append-only ("Corrections never mutate a
-- posted movement's litres"), but only by convention — nothing stopped a bug,
-- a new code path, or a direct write from UPDATE-ing litres/balance_after or
-- DELETE-ing a movement, silently corrupting book stock and every
-- reconciliation that derives from it. 0065 froze the *journal* ledger; this
-- does the same for the *stock* ledger.
--
-- The only legitimate UPDATE is the posted -> reversed status annotation that
-- ReverseMovement performs (its litres are untouched; a contra movement nets
-- the original to zero). Every quantity/identity column is frozen; DELETE is
-- blocked. The app.allow_ledger_delete escape hatch (same as 0065) exists only
-- for whole-tenant teardown (integration cleanup / future purge); it reads via
-- current_setting(..., true) so an unset GUC fails closed.

CREATE OR REPLACE FUNCTION assert_stock_movement_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF current_setting('app.allow_ledger_delete', true) = 'on' THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'stock_movements are append-only: movement % cannot be deleted (post a contra movement instead)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    -- A posted movement may only be annotated reversed. Quantity and identity
    -- columns are frozen; updated_at is intentionally not compared (the
    -- set_updated_at trigger bumps it on this very UPDATE).
    IF OLD.status = 'posted'
       AND NEW.status          = 'reversed'
       AND NEW.id              = OLD.id
       AND NEW.seq             = OLD.seq
       AND NEW.tenant_id       = OLD.tenant_id
       AND NEW.tank_id         = OLD.tank_id
       AND NEW.movement_type   = OLD.movement_type
       AND NEW.litres          = OLD.litres
       AND NEW.balance_after   = OLD.balance_after
       AND NEW.source_ref_type IS NOT DISTINCT FROM OLD.source_ref_type
       AND NEW.source_ref_id   IS NOT DISTINCT FROM OLD.source_ref_id
       AND NEW.recorded_by     = OLD.recorded_by
       AND NEW.recorded_at     = OLD.recorded_at
       AND NEW.supersedes_id   IS NOT DISTINCT FROM OLD.supersedes_id
       AND NEW.created_at      = OLD.created_at
    THEN
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'stock_movements are immutable: movement % may only transition posted->reversed (post a contra movement to correct it)', OLD.id
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

-- Fires before stock_movements_set_updated_at (alphabetical: "immutable" <
-- "set_updated_at"), so a rejected update aborts before any other trigger runs.
CREATE TRIGGER stock_movements_immutable
    BEFORE UPDATE OR DELETE ON stock_movements
    FOR EACH ROW EXECUTE FUNCTION assert_stock_movement_immutable();
