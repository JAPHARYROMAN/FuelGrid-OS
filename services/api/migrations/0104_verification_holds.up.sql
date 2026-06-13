-- 0104_verification_holds: Mobile Attendant App PRD closeout (PRD §7.8, §9.5,
-- §9.6) — the supervisor's non-terminal review verdicts.
--
-- The dual-value model (0101) already records approve/correct decisions and
-- reserved 'rejected' in the status CHECK. This migration finishes the verdict
-- vocabulary so a supervisor can also REJECT a closing reading (send it back to
-- the attendant to re-capture) or FLAG it for investigation, and can FLAG a cash
-- handover. None of these mutate the attendant's original submission: the
-- verdict, its mandatory reason, and the snapshots all live in the verification
-- / receipt row.
--
-- Verdict semantics, and how each interacts with the shift-approval gate:
--
--   reading_verifications.status
--     approved   — terminal good; final = attendant's submission.
--     corrected  — terminal good; final = supervisor's figure (reason).
--     rejected   — HOLD; the attendant must re-capture (reason). A rejection
--                  unlocks the Phase 3 closing-submission lock so the attendant
--                  can supersede the reading; the new ACTIVE reading is then
--                  unverified again and must be re-verified.
--     flagged    — HOLD; under investigation (reason). Blocks approval until a
--                  terminal verdict replaces it (the reading is re-captured or
--                  the flag is resolved by correcting/approving the reading).
--
--   collection_receipts.status
--     received                 — terminal good; difference = 0.
--     approved_with_difference — terminal good; difference ≠ 0 (reason).
--     rejected                 — handover refused (reason).
--     flagged                  — HOLD; under investigation (reason). Blocks
--                                approval like rejected does.
--
-- The shift-approval gate (extended in Go) now requires every ACTIVE closing
-- reading to carry a TERMINAL-GOOD verification {approved, corrected} and the
-- cash submission (when present) to carry a TERMINAL-GOOD receipt {received,
-- approved_with_difference}; a rejected/flagged hold blocks approval with a
-- machine-readable code.

-- ---------------------------------------------------------------------------
-- 1. reading_verifications: add 'flagged' alongside the already-present
--    'rejected'. 'rejected' was reserved in 0101's CHECK; this only widens it
--    by one value. The existing reason CHECK ("status = 'approved' OR reason
--    IS NOT NULL") already makes a reason mandatory for both rejected and
--    flagged, and the corrected CHECK only constrains 'corrected', so neither
--    new value needs a supervisor figure.
-- ---------------------------------------------------------------------------
ALTER TABLE reading_verifications
    DROP CONSTRAINT chk_reading_verifications_status;
ALTER TABLE reading_verifications
    ADD CONSTRAINT chk_reading_verifications_status
        CHECK (status IN ('approved', 'corrected', 'rejected', 'flagged'));

-- ---------------------------------------------------------------------------
-- 2. collection_receipts: add 'flagged'. Like 'rejected', a flagged receipt is
--    a hold whose difference/status agreement is not enforced (the supervisor
--    refused to finalize the handover), and a reason is mandatory.
-- ---------------------------------------------------------------------------
ALTER TABLE collection_receipts
    DROP CONSTRAINT chk_collection_receipts_status;
ALTER TABLE collection_receipts
    ADD CONSTRAINT chk_collection_receipts_status
        CHECK (status IN ('received', 'approved_with_difference', 'rejected', 'flagged'));

-- The difference-agreement CHECK must let a flagged handover carry any
-- difference, exactly as it already does for rejected.
ALTER TABLE collection_receipts
    DROP CONSTRAINT chk_collection_receipts_difference;
ALTER TABLE collection_receipts
    ADD CONSTRAINT chk_collection_receipts_difference
        CHECK (
            status IN ('rejected', 'flagged')
            OR (status = 'received' AND difference = 0)
            OR (status = 'approved_with_difference' AND difference <> 0)
        );

-- A reason is mandatory whenever the difference is non-zero or the handover is
-- not a clean 'received' (rejected/flagged both demand one).
ALTER TABLE collection_receipts
    DROP CONSTRAINT chk_collection_receipts_reason;
ALTER TABLE collection_receipts
    ADD CONSTRAINT chk_collection_receipts_reason
        CHECK ((difference = 0 AND status = 'received') OR reason IS NOT NULL);
