-- 0024_stock_movements: the per-tank append-only stock ledger (Phase 4,
-- Stage 1).
--
-- Every litre that moves in or out of a tank is one row here. Book stock is
-- DERIVED by summing the ledger (CurrentBalance), never stored as an
-- authoritative running total — balance_after is only a per-row snapshot of
-- the balance at post time. The ledger is the source of truth.
--
-- Corrections never mutate a posted movement's litres. A reversal marks the
-- original 'reversed' (status annotation only) and posts a contra entry with
-- negated litres and supersedes_id pointing back at it, so the original and
-- its contra net to zero in the sum. The status column is informational —
-- the book balance sums all rows regardless of status precisely because a
-- reversed movement is offset by its contra entry, not removed.
--
-- source_ref_(type|id) trace each movement to the row that caused it (a
-- delivery, a shift, a reconciliation, …). It is polymorphic — the id points
-- into different tables depending on type — so there is no single FK for it;
-- tank_id carries the usual composite (tenant_id, …) FK from 0008/0010.

CREATE TABLE stock_movements (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Monotonic insertion order: the true append sequence of the ledger.
    -- now()/created_at share one value across a multi-movement transaction,
    -- so the ledger is ordered by seq, not by timestamp.
    seq             bigint GENERATED ALWAYS AS IDENTITY,
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    tank_id         uuid NOT NULL,
    movement_type   text NOT NULL,
    source_ref_type text,
    source_ref_id   uuid,
    litres          numeric(14, 3) NOT NULL,
    balance_after   numeric(14, 3) NOT NULL,
    recorded_by     uuid NOT NULL,
    recorded_at     timestamptz NOT NULL DEFAULT now(),
    supersedes_id   uuid REFERENCES stock_movements(id) ON DELETE SET NULL,
    status          text NOT NULL DEFAULT 'posted',
    notes           text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_stock_mvt_type CHECK (
        movement_type IN ('opening', 'delivery', 'sales', 'adjustment', 'transfer')
    ),
    CONSTRAINT chk_stock_mvt_source_ref_type CHECK (
        source_ref_type IS NULL OR source_ref_type IN (
            'opening', 'delivery', 'shift', 'adjustment', 'reconciliation',
            'transfer', 'correction'
        )
    ),
    CONSTRAINT chk_stock_mvt_status CHECK (status IN ('posted', 'reversed')),

    CONSTRAINT stock_mvt_tank_fk
        FOREIGN KEY (tenant_id, tank_id) REFERENCES tanks(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT stock_mvt_recorded_by_fk
        FOREIGN KEY (tenant_id, recorded_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_stock_mvt_tenant_id ON stock_movements(tenant_id);
-- The ledger read: a tank's movements in append order.
CREATE INDEX idx_stock_mvt_tank_seq  ON stock_movements(tank_id, seq);
-- Trace a source row back to the movement(s) it caused.
CREATE INDEX idx_stock_mvt_source_ref
    ON stock_movements(source_ref_type, source_ref_id) WHERE source_ref_id IS NOT NULL;

-- Composite tenant key: FK target for later children that reference a movement.
ALTER TABLE stock_movements ADD CONSTRAINT uq_stock_mvt_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER stock_movements_set_updated_at
    BEFORE UPDATE ON stock_movements
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE stock_movements ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON stock_movements
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: inventory.read is station-scoped — the gate for the per-tank
-- ledger and book-balance reads. Manual movement writes (Stage 2 opening
-- balances, Stage 6 adjustments) reuse the existing station-scoped
-- stock.adjust permission seeded in 0004; we deliberately do not add a
-- redundant inventory.adjust.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('inventory.read', 'View tank stock ledger and book balances', 'inventory', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'inventory.read'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor', 'executive', 'auditor')
ON CONFLICT (role_id, permission_id) DO NOTHING;
