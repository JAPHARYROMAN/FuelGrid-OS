-- 0081_mpesa_transactions: M-Pesa (Safaricom Daraja) mobile-money collections.
--
-- One row per STK Push (Lipa na M-Pesa Online) attempt. The row is created
-- 'pending' when the API initiates the push and Daraja acknowledges with a
-- CheckoutRequestID; the asynchronous result callback (POST to the public
-- callback URL) flips it to 'paid' (ResultCode 0, with the M-Pesa receipt) or
-- 'failed' (any other result code). raw_payload keeps the verbatim Daraja
-- callback body for audit/dispute.
--
-- A 'paid' transaction can later be RECONCILED against a revenue day's
-- mobile-money tender: reconciled_revenue_day_id points at the revenue_days row
-- the operator matched it to, so the ledger and the wallet agree.
--
-- Money is a TEXT decimal string end-to-end (never float), consistent with the
-- payments/revenue tables; the Go layer reads/writes it as ::text/::numeric.

CREATE TABLE mpesa_transactions (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    station_id          uuid NOT NULL,
    -- Daraja correlation ids. checkout_request_id is the join key the callback
    -- carries back; it is unique per tenant so a duplicated callback is an
    -- idempotent UPDATE, not a second row.
    checkout_request_id text NOT NULL,
    merchant_request_id text,
    -- Whole-shilling-or-decimal amount requested / settled, as a decimal string.
    amount              numeric(14, 2) NOT NULL,
    phone               text NOT NULL,
    status              text NOT NULL DEFAULT 'pending',
    -- Daraja ResultCode from the callback (0 = success); NULL until callback.
    result_code         integer,
    -- M-Pesa receipt (e.g. NLJ7RT61SV), only set once paid.
    mpesa_receipt       text,
    account_reference   text,
    description         text,
    -- Verbatim Daraja callback body, for audit and dispute resolution.
    raw_payload         jsonb,
    -- The revenue day this paid transaction was reconciled against (nullable
    -- until an operator matches it). FK keeps a match honest and tenant-scoped.
    reconciled_revenue_day_id uuid,
    reconciled_at       timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chk_mpesa_status
        CHECK (status IN ('pending', 'paid', 'failed', 'cancelled')),
    CONSTRAINT chk_mpesa_amount CHECK (amount >= 0),

    CONSTRAINT uq_mpesa_tenant_checkout UNIQUE (tenant_id, checkout_request_id),

    CONSTRAINT mpesa_station_fk
        FOREIGN KEY (tenant_id, station_id) REFERENCES stations(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT mpesa_revenue_day_fk
        FOREIGN KEY (tenant_id, reconciled_revenue_day_id) REFERENCES revenue_days(tenant_id, id) ON DELETE SET NULL
);

CREATE INDEX idx_mpesa_tenant_id  ON mpesa_transactions(tenant_id);
CREATE INDEX idx_mpesa_station    ON mpesa_transactions(tenant_id, station_id, created_at DESC);
CREATE INDEX idx_mpesa_checkout   ON mpesa_transactions(checkout_request_id);
CREATE INDEX idx_mpesa_revenue_day
    ON mpesa_transactions(reconciled_revenue_day_id)
    WHERE reconciled_revenue_day_id IS NOT NULL;

CREATE TRIGGER mpesa_transactions_set_updated_at
    BEFORE UPDATE ON mpesa_transactions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Tenant isolation under RLS: the request-scoped fuelgrid_app role only sees
-- rows for the tenant bound on the connection (app.current_tenant). The
-- owner/migration role bypasses, as everywhere. The Daraja callback is
-- unauthenticated (no session), so its handler runs on the owner pool and
-- scopes the lookup by checkout_request_id explicitly.
ALTER TABLE mpesa_transactions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mpesa_transactions
    USING      (tenant_id::text = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id::text = current_setting('app.current_tenant', true));

-- ---------------------------------------------------------------------------
-- Permission: payment.mpesa.manage — initiate STK pushes and reconcile
-- collections. Folded into the finance/management roles that already carry
-- revenue.manage-style authority.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (code, description, category, station_scoped) VALUES
    ('payment.mpesa.manage', 'Initiate M-Pesa STK pushes and reconcile collections', 'finance', true);

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.is_system AND p.code = 'payment.mpesa.manage'
  AND r.code IN ('system_admin', 'regional_manager', 'station_manager', 'supervisor', 'finance_officer')
ON CONFLICT (role_id, permission_id) DO NOTHING;
