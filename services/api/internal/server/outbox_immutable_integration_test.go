package server_test

// DB-backed integration test for OUTBOX-IMMUT-3 (migration 0075). Reuses the
// Phase 2 harness; gated on TEST_DATABASE_URL + TEST_REDIS_URL.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestOutboxEventsColumnScopedImmutability proves outbox_events is append-only
// EXCEPT the publisher's three progress columns (published_at, attempt_count,
// failed_at). 0006 documented the table as append-only with one intentional
// exception — the publisher recording dispatch progress — but only by
// convention. Migration 0075 enforces it at the database: the publisher's
// drain UPDATE (touching only progress columns) is allowed, while any UPDATE
// that rewrites the immutable payload/identity (which would replay a different
// payload or re-target the aggregate) is refused, and undelivered events
// cannot be silently DELETE-d. The trigger sits beneath every code path, so a
// bug or a direct write cannot corrupt the event stream.
func TestOutboxEventsColumnScopedImmutability(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, _ := h.adminContext(t, ctx)

	// Seed an unpublished outbox row directly — exactly the columns WriteOutbox
	// writes (internal/events/outbox.go). attempt_count/failed_at default.
	var eventID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO outbox_events
		    (tenant_id, event_type, event_version,
		     aggregate_type, aggregate_id, actor_id,
		     payload, occurred_at)
		VALUES ($1, 'product.created', 1, 'product', 'PMS-1', $2, '{"price": 100}'::jsonb, now())
		RETURNING id
	`, h.ids.tenantID, adminID).Scan(&eventID); err != nil {
		t.Fatalf("seed outbox event: %v", err)
	}

	// The publisher's drain UPDATE — touching ONLY published_at — must succeed.
	// This is the exact statement processOnce runs to mark a row dispatched
	// (UPDATE outbox_events SET published_at = now() WHERE id = ANY($1)).
	if _, err := h.pool.Exec(ctx,
		`UPDATE outbox_events SET published_at = now() WHERE id = ANY($1)`,
		[]uuid.UUID{eventID}); err != nil {
		t.Fatalf("publisher-style published_at UPDATE must be allowed, got: %v", err)
	}
	// The publisher's failure-bookkeeping UPDATE — attempt_count + failed_at —
	// must also be allowed (the second statement the publisher issues).
	if _, err := h.pool.Exec(ctx,
		`UPDATE outbox_events SET attempt_count = 1, failed_at = NULL WHERE id = $1`,
		eventID); err != nil {
		t.Fatalf("publisher-style attempt_count/failed_at UPDATE must be allowed, got: %v", err)
	}

	// Every mutation of an immutable column must be refused at the database. The
	// error must come from our trigger (not some incidental failure), so assert
	// the message.
	mustBlock := func(label, want, sql string, args ...any) {
		t.Helper()
		if _, err := h.pool.Exec(ctx, sql, args...); err == nil {
			t.Fatalf("%s: expected the immutability trigger to refuse the write, got nil error", label)
		} else if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error = %q, want it to contain %q", label, err.Error(), want)
		}
	}
	// Rewriting the payload (would replay a different event to consumers).
	mustBlock("UPDATE payload", "immutable",
		`UPDATE outbox_events SET payload = '{"price": 999}'::jsonb WHERE id = $1`, eventID)
	// Re-targeting which aggregate the event describes.
	mustBlock("UPDATE aggregate_id", "immutable",
		`UPDATE outbox_events SET aggregate_id = 'PMS-2' WHERE id = $1`, eventID)
	// Changing the event type.
	mustBlock("UPDATE event_type", "immutable",
		`UPDATE outbox_events SET event_type = 'product.deleted' WHERE id = $1`, eventID)
	// A mixed UPDATE that bumps a progress column AND tampers with payload is
	// still rejected — the guard checks the immutable columns, not the set list.
	mustBlock("UPDATE payload + published_at", "immutable",
		`UPDATE outbox_events SET payload = '{"x": 1}'::jsonb, published_at = now() WHERE id = $1`, eventID)
	// Direct delete is rejected (no app.allow_ledger_delete on this connection).
	mustBlock("DELETE outbox_events", "append-only",
		`DELETE FROM outbox_events WHERE id = $1`, eventID)

	// The event's identity/payload is untouched after the rejected writes.
	var eventType, aggregateID, payload string
	if err := h.pool.QueryRow(ctx, `
		SELECT event_type, aggregate_id, payload::text
		FROM outbox_events WHERE id = $1
	`, eventID).Scan(&eventType, &aggregateID, &payload); err != nil {
		t.Fatalf("reread outbox event: %v", err)
	}
	if eventType != "product.created" || aggregateID != "PMS-1" || !strings.Contains(payload, "100") {
		t.Fatalf("after blocked writes: type=%q aggregate=%q payload=%q, want product.created / PMS-1 / price 100",
			eventType, aggregateID, payload)
	}
}
