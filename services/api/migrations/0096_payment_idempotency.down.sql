-- Reverse of 0096_payment_idempotency.

DROP INDEX IF EXISTS uq_payments_tenant_idempotency_key;

ALTER TABLE payments
    DROP CONSTRAINT IF EXISTS chk_payments_idempotency_key_len;

ALTER TABLE payments
    DROP COLUMN IF EXISTS idempotency_key;
