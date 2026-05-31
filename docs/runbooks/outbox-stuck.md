# Runbook: Outbox backlog / publisher stalled

**Alerts:** `OutboxDeadLetterBacklog` (unpublished > 500 for 10m),
`OutboxPublisherStalled` (oldest unpublished > 300s for 5m).

**Severity:** warning (backlog) / critical (stalled).

## Background

FuelGrid uses the transactional outbox pattern. Domain changes write a row to
`outbox_events` in the same transaction as the business write; a publisher loop
later reads unpublished rows (`published_at IS NULL`), delivers the event, and
stamps `published_at`.

Two gauges (`internal/observability/metrics.go`, refreshed by `ObserveOutbox`)
expose health:

- `fuelgrid_outbox_unpublished_events` — count of rows with `published_at IS NULL`.
- `fuelgrid_outbox_oldest_unpublished_age_seconds` — age of the oldest such row.

A rising **count** with a small **age** means the publisher is keeping up but
under volume. A rising **age** means the publisher is effectively stalled.

## First 5 minutes — triage

1. Open the **Outbox backlog & lag** dashboard panel. Is age growing
   monotonically (stalled) or is count high but age bounded (volume spike)?
2. Confirm the publisher process/worker is running and not crash-looping
   (check logs for the outbox publisher loop).
3. Check the size and oldest rows directly:
   ```sql
   SELECT count(*) AS backlog,
          extract(epoch FROM (now() - min(occurred_at))) AS oldest_age_s
   FROM outbox_events
   WHERE published_at IS NULL;

   SELECT id, aggregate_type, event_type, occurred_at
   FROM outbox_events
   WHERE published_at IS NULL
   ORDER BY occurred_at ASC
   LIMIT 20;
   ```

## Common causes & actions

| Symptom                                          | Likely cause                                   | Action                                                                     |
| ------------------------------------------------ | ---------------------------------------------- | -------------------------------------------------------------------------- |
| Age climbing, publisher logs show errors         | Downstream broker/consumer rejecting/unreachable | Restore the downstream; publisher should resume and drain                |
| Age climbing, publisher not running              | Worker crashed / not scheduled                 | Restart the publisher; confirm it is in the deployment                     |
| Age climbing, same row repeatedly failing        | Poison message (bad payload)                   | Inspect the oldest row; quarantine/skip it so the queue head can advance   |
| Count high, age bounded                          | Volume spike, publisher keeping up             | Usually self-resolves; verify throughput and scale the publisher if needed |
| Backlog + DB slow                                | DB pool saturation blocking the publisher      | See [db-unavailable.md](./db-unavailable.md)                               |

## Mitigation

- **Downstream outage:** fix/await the consumer; the publisher drains the
  backlog once delivery succeeds. Confirm idempotency on the consumer side so
  redelivery is safe.
- **Stalled worker:** restart it; watch the age gauge start falling.
- **Poison message:** identify the offending row, capture its payload for
  debugging, then unblock the head of the queue (skip/quarantine) per the
  team's data-fix procedure. Do not blindly delete events.

## Recovery & follow-up

- Confirm `fuelgrid_outbox_oldest_unpublished_age_seconds` is falling and the
  backlog count returns to baseline; alerts resolve.
- Verify no events were lost (publish is at-least-once; consumers must be
  idempotent).
- Post-incident: add a dead-letter path for poison messages if one was hit, and
  consider publisher autoscaling if it was a sustained volume spike.
