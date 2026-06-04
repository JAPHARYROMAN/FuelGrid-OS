-- 0093_opening_stock_requests: the draft -> approve(lock) / reject lifecycle for
-- a tank's opening stock (Feature 1.6).
--
-- Before this migration a tank's opening balance was seeded directly by anyone
-- holding stock.adjust (POST /tanks/{id}/opening-balance), with no review step.
-- This table adds the same request -> approve -> post discipline the stock
-- adjustments lifecycle uses (migration 0087): an opening figure is entered as a
-- 'draft'; a different person (separation of duties, enforced in the repo)
-- either approves it — which posts the genesis 'opening' movement to the
-- append-only stock_movements ledger (migration 0024) and LOCKS the request — or
-- rejects it with a reason. An approved (locked) request links the posted
-- movement and snapshots the balance; it can never be re-approved or silently
-- overwritten (status guard + the uq_osr_movement unique index make the lock a
-- hard one-way ratchet).
--
-- Permissions reuse the station-scoped stock.adjust (enter a draft) and
-- stock.approve_adjustment (approve/reject) seeded in 0004; no new permission is
-- added — entering and approving opening stock are the same controlled
-- book-stock authorities that govern adjustments.

CREATE TABLE opening_stock_requests (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    tank_id         uuid NOT NULL,
    -- Opening litres: a non-negative exact decimal (the ledger's numeric(14,3)
    -- precision). Bound into the column via $N::numeric — never a Go float.
    litres          numeric(14, 3) NOT NULL,
    notes           text,
    status          text NOT NULL DEFAULT 'draft',
    -- The 'opening' ledger row this request produced; set exactly once at
    -- approval (when the request locks).
    movement_id     uuid,
    balance_after   numeric(14, 3),
    requested_by    uuid NOT NULL,
    approved_by     uuid,
    rejected_by     uuid,
    decision_note   text,
    requested_at    timestamptz NOT NULL DEFAULT now(),
    decided_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_osr_litres_nonneg CHECK (litres >= 0),
    CONSTRAINT chk_osr_status
        CHECK (status IN ('draft', 'approved', 'rejected')),
    CONSTRAINT osr_tank_fk
        FOREIGN KEY (tenant_id, tank_id) REFERENCES tanks(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT osr_movement_fk
        FOREIGN KEY (tenant_id, movement_id) REFERENCES stock_movements(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT osr_requested_by_fk
        FOREIGN KEY (tenant_id, requested_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_osr_tenant ON opening_stock_requests(tenant_id);
CREATE INDEX idx_osr_tank   ON opening_stock_requests(tank_id);
CREATE INDEX idx_osr_status ON opening_stock_requests(tenant_id, status);

-- At most one live (draft or approved) request per tank: a tank has exactly one
-- opening, and once approved the request is the locked record of it. Rejected
-- requests are exempt so a corrected figure can be re-entered after a rejection.
CREATE UNIQUE INDEX uq_osr_one_live_per_tank
    ON opening_stock_requests(tenant_id, tank_id) WHERE status IN ('draft', 'approved');

-- A movement backs at most one request; the unique index makes the approved
-- "locked" ratchet a hard invariant.
CREATE UNIQUE INDEX uq_osr_movement
    ON opening_stock_requests(tenant_id, movement_id) WHERE movement_id IS NOT NULL;

CREATE TRIGGER opening_stock_requests_set_updated_at
    BEFORE UPDATE ON opening_stock_requests
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE opening_stock_requests ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON opening_stock_requests
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));
