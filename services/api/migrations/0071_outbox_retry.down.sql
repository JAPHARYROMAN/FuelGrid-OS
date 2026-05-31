-- Revert 0071_outbox_retry: restore the original hot-path index, then drop
-- the retry-tracking columns.

DROP INDEX IF EXISTS idx_outbox_events_unpublished;
CREATE INDEX idx_outbox_events_unpublished
    ON outbox_events(occurred_at) WHERE published_at IS NULL;

ALTER TABLE outbox_events
    DROP COLUMN IF EXISTS failed_at,
    DROP COLUMN IF EXISTS attempt_count;
