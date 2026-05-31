-- Revert 0075_outbox_events_guard.
DROP TRIGGER IF EXISTS outbox_events_immutable ON outbox_events;
DROP FUNCTION IF EXISTS assert_outbox_event_immutable();
