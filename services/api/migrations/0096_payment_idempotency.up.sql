-- 0096_payment_idempotency: client-supplied idempotency key for payment records
-- (SR-M2).
--
-- The payment-record write path (payments.Record, called from
-- handleRecordPayment) had no duplicate protection: a double-clicked submit, a
-- client/network retry, or a replayed request would insert a SECOND payment row
-- for the same logical tender, double-counting cash/mobile-money/card against a
-- shift and creating reconciliation variances. (The M-Pesa callback path and the
-- credit-charge GL journal already dedup; this closes the gap on the base
-- payments insert.)
--
-- Fix: an optional client-supplied idempotency_key column with a PARTIAL UNIQUE
-- index scoped per tenant — (tenant_id, idempotency_key) WHERE idempotency_key
-- IS NOT NULL. The partial predicate means:
--   * the column is nullable and existing rows are all NULL, so the index can be
--     added to the populated table with NO backfill and NO conflict (NULLs are
--     not indexed by the partial predicate);
--   * a replay carrying the same key collides on the unique index instead of
--     double-inserting, so the repo can detect the conflict and return the
--     already-recorded payment (idempotent) rather than applying the amount twice;
--   * tenant_id is the leading column, so the SAME key in a DIFFERENT tenant does
--     not collide — the guard is tenant-scoped, matching every other constraint
--     on this table.
-- A request that supplies no key keeps the prior behaviour (the key is the
-- recommended path).

ALTER TABLE payments
    ADD COLUMN idempotency_key text;

-- Bound the key length so a client cannot store unbounded blobs; 255 is ample
-- for a UUID/ULID/opaque token. NULL (no key supplied) is always allowed.
ALTER TABLE payments
    ADD CONSTRAINT chk_payments_idempotency_key_len
        CHECK (idempotency_key IS NULL OR char_length(idempotency_key) BETWEEN 1 AND 255);

-- Partial unique index: one payment per (tenant, idempotency_key). Safe to add
-- to the existing table because every current row has idempotency_key IS NULL
-- and the predicate excludes NULLs from the index.
CREATE UNIQUE INDEX uq_payments_tenant_idempotency_key
    ON payments (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
