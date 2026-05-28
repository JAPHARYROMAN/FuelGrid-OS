-- 0036_revenue_days: daily revenue close per station (Phase 6, Stage 7).
--
-- One row per (station, operating_day): the rolled-up gross/net/tax revenue,
-- COGS, margin, and tender mix, computed from the day's sales (0033) and
-- payments (0035). Locking freezes the figures — the financial analog of the
-- Phase-4 reconciliation seal.

CREATE TABLE revenue_days (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id         uuid NOT NULL,
    operating_day_id   uuid NOT NULL,
    business_date      date NOT NULL,
    gross_revenue      numeric(14, 2) NOT NULL DEFAULT 0,
    net_revenue        numeric(14, 2) NOT NULL DEFAULT 0,
    tax_total          numeric(14, 2) NOT NULL DEFAULT 0,
    cogs_total         numeric(14, 2) NOT NULL DEFAULT 0,
    margin_total       numeric(14, 2) NOT NULL DEFAULT 0,
    cash_total         numeric(14, 2) NOT NULL DEFAULT 0,
    mobile_money_total numeric(14, 2) NOT NULL DEFAULT 0,
    card_total         numeric(14, 2) NOT NULL DEFAULT 0,
    credit_total       numeric(14, 2) NOT NULL DEFAULT 0,
    voucher_total      numeric(14, 2) NOT NULL DEFAULT 0,
    tender_total       numeric(14, 2) NOT NULL DEFAULT 0,
    cash_variance      numeric(14, 2) NOT NULL DEFAULT 0,
    status             text NOT NULL DEFAULT 'draft',
    locked_by          uuid,
    locked_at          timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_revenue_days_status CHECK (status IN ('draft', 'locked')),

    CONSTRAINT revenue_days_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT revenue_days_day_fk
        FOREIGN KEY (tenant_id, operating_day_id) REFERENCES operating_days(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT revenue_days_locked_by_fk
        FOREIGN KEY (tenant_id, locked_by) REFERENCES users(tenant_id, id) ON DELETE RESTRICT,

    CONSTRAINT uq_revenue_days_station_day UNIQUE (station_id, operating_day_id)
);

CREATE INDEX idx_revenue_days_tenant_id ON revenue_days(tenant_id);
CREATE INDEX idx_revenue_days_station   ON revenue_days(station_id, business_date DESC);

ALTER TABLE revenue_days ADD CONSTRAINT uq_revenue_days_tenant_id UNIQUE (tenant_id, id);

CREATE TRIGGER revenue_days_set_updated_at
    BEFORE UPDATE ON revenue_days
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE revenue_days ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON revenue_days
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- Reads ride revenue.read (0033); locking rides the existing period.lock (0004).
