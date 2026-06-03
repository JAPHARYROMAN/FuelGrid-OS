-- 0087_stock_adjustments: the request -> approve -> post lifecycle for manual
-- stock adjustments (Feature 5.4).
--
-- A stock adjustment is a controlled correction to a tank's book stock: someone
-- requests a signed litre delta with a reason and a classification, a different
-- person approves (or rejects) it, and on posting it appends a single
-- 'adjustment' movement to the append-only stock_movements ledger (migration
-- 0024). The ledger row carries balance_after, so the before/after book stock is
-- recorded there; this table snapshots balance_before / balance_after at post
-- time too, for a self-contained audit trail.
--
-- Separation of duties: the approver must not be the requester (enforced in the
-- repo under a row lock). Posting is idempotent — once an adjustment reaches
-- 'posted' its movement_id is set and it can never be re-posted or mutated; the
-- lifecycle is a one-way ratchet (requested -> approved -> posted, with
-- requested|approved -> rejected). The permissions reuse the existing
-- station-scoped stock.adjust (request) and stock.approve_adjustment
-- (approve/reject/post) seeded in 0004; no new permission is added.

CREATE TABLE stock_adjustments (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    tank_id         uuid NOT NULL,
    -- Signed decimal delta: +litres increases book stock, -litres decreases it.
    delta_litres    numeric(14, 3) NOT NULL,
    -- Why the book stock is being corrected (free text) + a coarse machine
    -- classification used for reporting and approval scrutiny.
    reason          text NOT NULL,
    classification  text NOT NULL,
    status          text NOT NULL DEFAULT 'requested',
    -- Book-stock snapshots captured when the adjustment posts (the ledger's
    -- balance_after on the prior tail row, and after this movement applies).
    balance_before  numeric(14, 3),
    balance_after   numeric(14, 3),
    -- The posted ledger row this adjustment produced; set exactly once at post.
    movement_id     uuid,
    requested_by    uuid NOT NULL,
    approved_by     uuid,
    posted_by       uuid,
    rejected_by     uuid,
    decision_note   text,
    requested_at    timestamptz NOT NULL DEFAULT now(),
    decided_at      timestamptz,
    posted_at       timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_stock_adj_delta_nonzero CHECK (delta_litres <> 0),
    CONSTRAINT chk_stock_adj_status
        CHECK (status IN ('requested', 'approved', 'posted', 'rejected')),
    CONSTRAINT chk_stock_adj_classification
        CHECK (classification IN (
            'evaporation', 'measurement_error', 'theft', 'spillage',
            'temperature', 'data_entry', 'other'
        )),
    CONSTRAINT stock_adj_tank_fk
        FOREIGN KEY (tenant_id, tank_id) REFERENCES tanks(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT stock_adj_movement_fk
        FOREIGN KEY (tenant_id, movement_id) REFERENCES stock_movements(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT stock_adj_requested_by_fk
        FOREIGN KEY (tenant_id, requested_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_stock_adj_tenant  ON stock_adjustments(tenant_id);
CREATE INDEX idx_stock_adj_tank    ON stock_adjustments(tank_id);
CREATE INDEX idx_stock_adj_status  ON stock_adjustments(tenant_id, status);
-- A movement backs at most one adjustment; the unique index makes the
-- one-way "posted" ratchet a hard invariant.
CREATE UNIQUE INDEX uq_stock_adj_movement
    ON stock_adjustments(tenant_id, movement_id) WHERE movement_id IS NOT NULL;

CREATE TRIGGER stock_adjustments_set_updated_at
    BEFORE UPDATE ON stock_adjustments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE stock_adjustments ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON stock_adjustments
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));
