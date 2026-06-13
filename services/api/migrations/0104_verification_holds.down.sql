-- Revert 0104_verification_holds. Remap any rows created with the new verdicts
-- to the nearest pre-0104 value first so the narrower CHECKs can be re-added.

-- ---------------------------------------------------------------------------
-- collection_receipts: drop 'flagged' (fold into 'rejected', the other hold).
-- ---------------------------------------------------------------------------
UPDATE collection_receipts SET status = 'rejected' WHERE status = 'flagged';

ALTER TABLE collection_receipts
    DROP CONSTRAINT chk_collection_receipts_reason;
ALTER TABLE collection_receipts
    ADD CONSTRAINT chk_collection_receipts_reason
        CHECK ((difference = 0 AND status <> 'rejected') OR reason IS NOT NULL);

ALTER TABLE collection_receipts
    DROP CONSTRAINT chk_collection_receipts_difference;
ALTER TABLE collection_receipts
    ADD CONSTRAINT chk_collection_receipts_difference
        CHECK (
            status = 'rejected'
            OR (status = 'received' AND difference = 0)
            OR (status = 'approved_with_difference' AND difference <> 0)
        );

ALTER TABLE collection_receipts
    DROP CONSTRAINT chk_collection_receipts_status;
ALTER TABLE collection_receipts
    ADD CONSTRAINT chk_collection_receipts_status
        CHECK (status IN ('received', 'approved_with_difference', 'rejected'));

-- ---------------------------------------------------------------------------
-- reading_verifications: drop 'flagged' (fold into 'rejected', the other
-- hold). 'rejected' stays — it was reserved in 0101's CHECK.
-- ---------------------------------------------------------------------------
UPDATE reading_verifications SET status = 'rejected' WHERE status = 'flagged';

ALTER TABLE reading_verifications
    DROP CONSTRAINT chk_reading_verifications_status;
ALTER TABLE reading_verifications
    ADD CONSTRAINT chk_reading_verifications_status
        CHECK (status IN ('approved', 'corrected', 'rejected'));
