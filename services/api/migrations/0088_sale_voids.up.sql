-- 0088_sale_voids: the request -> approve|reject lifecycle for voiding a
-- recognized sale (Feature 4.3).
--
-- A recognized sale (migration 0033) is APPEND-ONLY: once a shift is approved and
-- its metered litres are valued into a sale row, that row is never deleted or
-- mutated. To "void" a sale we record a controlled reversal: someone requests the
-- void with a reason, a DIFFERENT person approves (or rejects) it, and on approve
-- this row becomes the reversal record — it snapshots the sale's recognized
-- amounts NEGATED (gross/net/tax/cogs/margin and litres), so revenue rollups that
-- net approved voids against their sales (e.g. revenue.DaySummary) reflect the
-- reversal WITHOUT touching the original sale.
--
-- Separation of duties: the approver/rejecter must not be the requester (enforced
-- in the repo under a row lock), mirroring the stock-adjustment lifecycle
-- (migration 0087). The lifecycle is a one-way ratchet:
--   requested -> approved   (reversal recorded; terminal)
--   requested -> rejected   (terminal)
-- A second approve is a no-op/409 (status guard); a sale can carry at most one
-- non-rejected void at a time (partial unique index), which prevents double-void.
--
-- Permissions sale.void.request and sale.void.approve are NEW (they did not exist
-- before this migration) and are seeded + granted to the appropriate system roles
-- below, mirroring migration 0004's grant pattern.

CREATE TABLE sale_voids (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    sale_id         uuid NOT NULL,
    status          text NOT NULL DEFAULT 'requested',
    reason          text NOT NULL,

    -- Reversal record: the sale's recognized amounts NEGATED, snapshotted at
    -- approve time. NULL until the void is approved. Money is numeric(14,2),
    -- litres numeric(14,3) — same precision as the sales row they reverse.
    reversal_litres numeric(14, 3),
    reversal_gross  numeric(14, 2),
    reversal_tax    numeric(14, 2),
    reversal_net    numeric(14, 2),
    reversal_cogs   numeric(14, 2),
    reversal_margin numeric(14, 2),

    requested_by    uuid NOT NULL,
    decided_by      uuid,
    decision_note   text,
    requested_at    timestamptz NOT NULL DEFAULT now(),
    decided_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_sale_void_status
        CHECK (status IN ('requested', 'approved', 'rejected')),
    -- Reversal amounts are present exactly when (and only when) the void is
    -- approved — the row is the reversal record only in the 'approved' state.
    CONSTRAINT chk_sale_void_reversal_on_approve CHECK (
        (status = 'approved' AND reversal_gross IS NOT NULL)
        OR (status <> 'approved' AND reversal_gross IS NULL)
    ),
    CONSTRAINT sale_void_sale_fk
        FOREIGN KEY (tenant_id, sale_id) REFERENCES sales(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sale_void_requested_by_fk
        FOREIGN KEY (tenant_id, requested_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT sale_void_decided_by_fk
        FOREIGN KEY (tenant_id, decided_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_sale_voids_tenant ON sale_voids(tenant_id);
CREATE INDEX idx_sale_voids_sale   ON sale_voids(sale_id);
CREATE INDEX idx_sale_voids_status ON sale_voids(tenant_id, status);

-- A sale may carry at most ONE non-rejected void at a time: this partial unique
-- index makes "no double-void" a hard DB invariant. A rejected void leaves the
-- sale free to be requested again.
CREATE UNIQUE INDEX uq_sale_void_active
    ON sale_voids(tenant_id, sale_id) WHERE status <> 'rejected';

CREATE TRIGGER sale_voids_set_updated_at
    BEFORE UPDATE ON sale_voids
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE sale_voids ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sale_voids
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permissions: sale.void.request (request a void) and sale.void.approve
-- (approve/reject a void). Both station-scoped — authorized against the sale's
-- station. NEW in this migration. Granted to mirror 0004's role conventions:
--   request : station-floor roles that recognize sales — supervisor,
--             station_manager, regional_manager — plus system_admin.
--   approve : the oversight/finance roles — finance_officer, regional_manager,
--             executive — plus system_admin (separation of duties is enforced
--             in code: requester != approver).
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('sale.void.request', 'Request a recognized sale be voided',  'finance', true),
    ('sale.void.approve', 'Approve or reject a sale void request', 'finance', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND (
    (p.code = 'sale.void.request' AND r.code IN (
        'system_admin', 'regional_manager', 'station_manager', 'supervisor'
    ))
    OR (p.code = 'sale.void.approve' AND r.code IN (
        'system_admin', 'regional_manager', 'finance_officer', 'executive'
    ))
)
ON CONFLICT (role_id, permission_id) DO NOTHING;
