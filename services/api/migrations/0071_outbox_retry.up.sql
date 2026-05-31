-- 0071_outbox_retry: retry tracking + dead-letter for the outbox publisher.
--
-- The publisher (internal/events/publisher.go) drains unpublished rows and
-- dispatches them to the bus. Before this migration a row that always failed
-- to dispatch would be retried forever on every poll tick. These columns let
-- the publisher count attempts and dead-letter a row once it exhausts the
-- retry budget, so a single poison event can no longer monopolise the loop.
--
--   attempt_count  number of failed dispatch attempts so far. Incremented on
--                  each failure; reset is never needed because a successful
--                  dispatch sets published_at and the row leaves the hot path.
--   failed_at      set when attempt_count reaches MaxOutboxAttempts. A non-NULL
--                  failed_at marks the row as dead-lettered: the publisher skips
--                  it on future ticks (it is still unpublished, but parked).

ALTER TABLE outbox_events
    ADD COLUMN attempt_count integer     NOT NULL DEFAULT 0,
    ADD COLUMN failed_at     timestamptz;

-- Keep the publisher's hot-path index aligned with the new skip condition:
-- only rows that are unpublished AND not dead-lettered are eligible for
-- dispatch, so exclude failed_at rows from the partial index.
DROP INDEX IF EXISTS idx_outbox_events_unpublished;
CREATE INDEX idx_outbox_events_unpublished
    ON outbox_events(occurred_at)
    WHERE published_at IS NULL AND failed_at IS NULL;
