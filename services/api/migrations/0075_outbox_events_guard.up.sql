-- 0075_outbox_events_guard: make outbox_events append-only EXCEPT the
-- publisher's legitimate progress columns (OUTBOX-IMMUT-3).
--
-- 0006 documents outbox_events as append-only ("rows are never UPDATEd in the
-- normal flow"), with one intentional exception: the background publisher
-- (internal/events/publisher.go) records dispatch progress on a row. But that
-- was convention only — nothing stopped a bug, a new code path, or a direct
-- write from rewriting an event's payload/identity (re-targeting which
-- aggregate it describes, or replaying a different payload to consumers) or
-- from DELETE-ing an undelivered event, silently dropping it. 0065 froze the
-- journal ledger, 0069 the stock ledger, 0070 the audit trail; this freezes the
-- outbox's immutable surface while leaving the publisher's drain path working.
--
-- The publisher mutates exactly three "progress" columns and nothing else:
--
--   published_at    set to now() when a row is successfully dispatched
--                   (UPDATE ... SET published_at = now() WHERE id = ANY($1)).
--   attempt_count   bumped on each failed dispatch.
--   failed_at       stamped once attempt_count exhausts MaxOutboxAttempts
--                   (UPDATE ... SET attempt_count = $2, failed_at = $3 ...).
--
-- Every other column — id, tenant_id, event_type, event_version,
-- aggregate_type, aggregate_id, actor_id, payload, metadata, occurred_at,
-- correlation_id, causation_id — is the immutable identity/payload of the
-- event and is frozen. An UPDATE that changes only the three progress columns
-- (the publisher's drain UPDATE) is allowed; any UPDATE that touches a frozen
-- column is rejected.
--
-- DELETE is blocked EXCEPT when current_setting('app.allow_ledger_delete',
-- true) = 'on' — the same escape hatch as 0065/0069/0070, used only for
-- deliberate whole-tenant teardown (integration-test cleanup / future
-- tenant-offboarding purge). No ordinary application path sets it, so the
-- control holds in production. The check uses current_setting(..., true) so an
-- unset GUC reads NULL and fails closed (deletion blocked).

CREATE OR REPLACE FUNCTION assert_outbox_event_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF current_setting('app.allow_ledger_delete', true) = 'on' THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'outbox_events are append-only: event % cannot be deleted', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    -- An UPDATE is permitted only if every immutable identity/payload column is
    -- unchanged. The three progress columns the publisher writes
    -- (published_at, attempt_count, failed_at) are deliberately NOT compared,
    -- so the drain UPDATE passes; any change to a frozen column is refused.
    IF NEW.id             = OLD.id
       AND NEW.tenant_id      IS NOT DISTINCT FROM OLD.tenant_id
       AND NEW.event_type     = OLD.event_type
       AND NEW.event_version  = OLD.event_version
       AND NEW.aggregate_type = OLD.aggregate_type
       AND NEW.aggregate_id   = OLD.aggregate_id
       AND NEW.actor_id       IS NOT DISTINCT FROM OLD.actor_id
       AND NEW.payload        = OLD.payload
       AND NEW.metadata       IS NOT DISTINCT FROM OLD.metadata
       AND NEW.occurred_at    = OLD.occurred_at
       AND NEW.correlation_id IS NOT DISTINCT FROM OLD.correlation_id
       AND NEW.causation_id   IS NOT DISTINCT FROM OLD.causation_id
    THEN
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'outbox_events are immutable: event % may only have its publisher progress columns (published_at, attempt_count, failed_at) updated', OLD.id
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER outbox_events_immutable
    BEFORE UPDATE OR DELETE ON outbox_events
    FOR EACH ROW EXECUTE FUNCTION assert_outbox_event_immutable();
