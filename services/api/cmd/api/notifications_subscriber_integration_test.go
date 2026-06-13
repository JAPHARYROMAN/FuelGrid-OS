package main

// DB-backed integration tests for the Phase 7 notification publishers: each
// per-attendant mapping is driven through the REAL bus + subscriber + repo
// against Postgres, asserting the row lands user-targeted with the right
// type/severity — and that republishing the SAME outbox event (an
// at-least-once redelivery) does not double-create, courtesy of the
// (tenant, source_event_id, target) dedupe from migration 0103.
//
// Gated on TEST_DATABASE_URL (a migrated database) like the server suite; the
// test skips when it is unset so `go test ./...` stays green without infra.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/events"
	"github.com/japharyroman/fuelgrid-os/internal/identity/repo"
	"github.com/japharyroman/fuelgrid-os/internal/notifications"
)

// subscriberTestEnv is the minimal world the subscriber needs: a tenant, two
// attendant users (notification user FK targets), the repo, and a live bus
// with the subscriber attached.
type subscriberTestEnv struct {
	pool       *database.Pool
	bus        *events.InProcessBus
	tenantID   uuid.UUID
	attendantA uuid.UUID
	attendantB uuid.UUID
}

func setupSubscriberEnv(t *testing.T) (*subscriberTestEnv, func()) {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("set TEST_DATABASE_URL to run notification subscriber integration tests")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, database.Config{URL: dbURL})
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}

	suffix := time.Now().UnixNano()
	var tenantID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, slug) VALUES ('Subscriber IT', $1) RETURNING id`,
		fmt.Sprintf("subit-%d", suffix)).Scan(&tenantID); err != nil {
		pool.Close()
		t.Fatalf("seed tenant: %v", err)
	}
	seedUser := func(label string) uuid.UUID {
		var id uuid.UUID
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
			 VALUES ($1, $2, $3, 'active', 'x-not-a-hash', now()) RETURNING id`,
			tenantID, fmt.Sprintf("%s-%d@subit.local", label, suffix), label).Scan(&id); err != nil {
			t.Fatalf("seed user %s: %v", label, err)
		}
		return id
	}
	attendantA := seedUser("attendant-a")
	attendantB := seedUser("attendant-b")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	bus := events.NewInProcessBus(logger)
	// nil email sender: the critical-severity fan-out short-circuits, which is
	// exactly what an isolated subscriber test wants.
	subscribeNotifications(bus, notifications.New(pool), repo.NewUserRepo(pool), nil, logger)

	env := &subscriberTestEnv{
		pool: pool, bus: bus, tenantID: tenantID,
		attendantA: attendantA, attendantB: attendantB,
	}
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM notifications WHERE tenant_id = $1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE tenant_id = $1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
		pool.Close()
	}
	return env, cleanup
}

// notifRow is the projection the assertions read back.
type notifRow struct {
	UserID   *uuid.UUID
	Type     string
	Severity string
	Body     string
}

func (env *subscriberTestEnv) rowsForEvent(t *testing.T, eventID uuid.UUID) []notifRow {
	t.Helper()
	rows, err := env.pool.Query(context.Background(), `
		SELECT user_id, type, severity, body
		FROM notifications
		WHERE tenant_id = $1 AND source_event_id = $2
		ORDER BY created_at, id`, env.tenantID, eventID)
	if err != nil {
		t.Fatalf("query notifications: %v", err)
	}
	defer rows.Close()
	var out []notifRow
	for rows.Next() {
		var n notifRow
		if err := rows.Scan(&n.UserID, &n.Type, &n.Severity, &n.Body); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// publishTwice dispatches the event through the real bus twice — the second
// pass simulates an outbox redelivery (the publisher re-runs EVERY handler for
// an event whose batch failed part-way).
func (env *subscriberTestEnv) publishTwice(t *testing.T, e events.Event) {
	t.Helper()
	ctx := context.Background()
	if err := env.bus.Publish(ctx, e); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := env.bus.Publish(ctx, e); err != nil {
		t.Fatalf("republish: %v", err)
	}
}

// TestNotificationSubscriber_PerAttendantMappings drives every Phase 7 mapping
// end to end (bus -> subscriber -> Postgres): one targeted row per affected
// attendant, correct type/severity, and no duplicate on redelivery.
func TestNotificationSubscriber_PerAttendantMappings(t *testing.T) {
	env, cleanup := setupSubscriberEnv(t)
	defer cleanup()

	tenant := env.tenantID
	att := env.attendantA

	t.Run("nozzle assigned", func(t *testing.T) {
		e := events.Event{
			ID: uuid.New(), TenantID: &tenant, Type: "ShiftNozzleAssigned",
			AggregateType: "shift", AggregateID: uuid.NewString(),
			Payload: json.RawMessage(`{"attendant_id":"` + att.String() + `","nozzle_id":"` + uuid.NewString() + `"}`),
		}
		env.publishTwice(t, e)
		rows := env.rowsForEvent(t, e.ID)
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want 1 (dedupe on redelivery)", len(rows))
		}
		if rows[0].UserID == nil || *rows[0].UserID != att {
			t.Fatalf("user_id = %v, want %s", rows[0].UserID, att)
		}
		if rows[0].Type != "assignment.created" || rows[0].Severity != "info" {
			t.Fatalf("row = %+v, want assignment.created/info", rows[0])
		}
	})

	t.Run("nozzle unassigned (assignment changed)", func(t *testing.T) {
		e := events.Event{
			ID: uuid.New(), TenantID: &tenant, Type: "ShiftNozzleUnassigned",
			AggregateType: "shift", AggregateID: uuid.NewString(),
			Payload: json.RawMessage(`{"attendant_id":"` + att.String() + `","assignment_id":"` + uuid.NewString() + `"}`),
		}
		env.publishTwice(t, e)
		rows := env.rowsForEvent(t, e.ID)
		if len(rows) != 1 || rows[0].UserID == nil || *rows[0].UserID != att ||
			rows[0].Type != "assignment.changed" {
			t.Fatalf("rows = %+v, want one assignment.changed targeted at %s", rows, att)
		}
	})

	t.Run("reading approved targets the recorder, deduped on redelivery", func(t *testing.T) {
		e := events.Event{
			ID: uuid.New(), TenantID: &tenant, Type: "ReadingVerificationApproved",
			AggregateType: "reading_verification", AggregateID: uuid.NewString(),
			Payload: json.RawMessage(`{"recorded_by":"` + att.String() + `",
				"verification":{"attendant_submitted_reading":"1500.000","final_approved_reading":"1500.000"}}`),
		}
		env.publishTwice(t, e)
		rows := env.rowsForEvent(t, e.ID)
		if len(rows) != 1 || rows[0].UserID == nil || *rows[0].UserID != att {
			t.Fatalf("rows = %+v, want one row targeted at recorder (deduped)", rows)
		}
		if rows[0].Type != "reading.approved" || rows[0].Severity != "success" {
			t.Fatalf("row = %+v, want reading.approved/success", rows[0])
		}
	})

	t.Run("reading rejected targets the recorder with reason", func(t *testing.T) {
		e := events.Event{
			ID: uuid.New(), TenantID: &tenant, Type: "ReadingVerificationRejected",
			AggregateType: "reading_verification", AggregateID: uuid.NewString(),
			Payload: json.RawMessage(`{"recorded_by":"` + att.String() + `",
				"verification":{"attendant_submitted_reading":"1500.000","final_approved_reading":"1500.000","reason":"meter photo unreadable"}}`),
		}
		env.publishTwice(t, e)
		rows := env.rowsForEvent(t, e.ID)
		if len(rows) != 1 || rows[0].UserID == nil || *rows[0].UserID != att {
			t.Fatalf("rows = %+v, want one row targeted at recorder", rows)
		}
		if rows[0].Type != "reading.rejected" || rows[0].Severity != "warning" {
			t.Fatalf("row = %+v, want reading.rejected/warning", rows[0])
		}
		for _, want := range []string{"re-capture", "meter photo unreadable"} {
			if !strings.Contains(rows[0].Body, want) {
				t.Errorf("body %q missing %q", rows[0].Body, want)
			}
		}
	})

	t.Run("reading flagged targets the recorder with reason", func(t *testing.T) {
		e := events.Event{
			ID: uuid.New(), TenantID: &tenant, Type: "ReadingVerificationFlagged",
			AggregateType: "reading_verification", AggregateID: uuid.NewString(),
			Payload: json.RawMessage(`{"recorded_by":"` + att.String() + `",
				"verification":{"attendant_submitted_reading":"1500.000","final_approved_reading":"1500.000","reason":"possible tampering"}}`),
		}
		env.publishTwice(t, e)
		rows := env.rowsForEvent(t, e.ID)
		if len(rows) != 1 || rows[0].UserID == nil || *rows[0].UserID != att {
			t.Fatalf("rows = %+v, want one row targeted at recorder", rows)
		}
		if rows[0].Type != "reading.flagged" || rows[0].Severity != "warning" {
			t.Fatalf("row = %+v, want reading.flagged/warning", rows[0])
		}
		for _, want := range []string{"investigation", "possible tampering"} {
			if !strings.Contains(rows[0].Body, want) {
				t.Errorf("body %q missing %q", rows[0].Body, want)
			}
		}
	})

	t.Run("reading corrected targets the recorder", func(t *testing.T) {
		e := events.Event{
			ID: uuid.New(), TenantID: &tenant, Type: "ReadingVerificationCorrected",
			AggregateType: "reading_verification", AggregateID: uuid.NewString(),
			Payload: json.RawMessage(`{"recorded_by":"` + att.String() + `",
				"verification":{"attendant_submitted_reading":"1500.000","final_approved_reading":"1490.000","reason":"misread"}}`),
		}
		env.publishTwice(t, e)
		rows := env.rowsForEvent(t, e.ID)
		if len(rows) != 1 || rows[0].UserID == nil || *rows[0].UserID != att {
			t.Fatalf("rows = %+v, want one row targeted at recorder", rows)
		}
		if rows[0].Type != "reading.corrected" || rows[0].Severity != "warning" {
			t.Fatalf("row = %+v, want reading.corrected/warning", rows[0])
		}
	})

	t.Run("collection receipt with shortage targets the submitter", func(t *testing.T) {
		e := events.Event{
			ID: uuid.New(), TenantID: &tenant, Type: "CashCollectionConfirmed",
			AggregateType: "collection_receipt", AggregateID: uuid.NewString(),
			Payload: json.RawMessage(`{"submitted_by":"` + att.String() + `",
				"expected_amount":"100000.00","supervisor_received_total":"95000.00",
				"difference":"-5000.00","status":"approved_with_difference","reason":"till short"}`),
		}
		env.publishTwice(t, e)
		rows := env.rowsForEvent(t, e.ID)
		if len(rows) != 1 || rows[0].UserID == nil || *rows[0].UserID != att {
			t.Fatalf("rows = %+v, want one row targeted at submitter", rows)
		}
		if rows[0].Type != "collection.receipt_recorded" || rows[0].Severity != "warning" {
			t.Fatalf("row = %+v, want collection.receipt_recorded/warning", rows[0])
		}
		for _, want := range []string{"95000.00", "100000.00", "Shortage of 5000.00", "till short"} {
			if !strings.Contains(rows[0].Body, want) {
				t.Errorf("body %q missing %q", rows[0].Body, want)
			}
		}
	})

	t.Run("shift approved fans out to each checked-in attendant", func(t *testing.T) {
		e := events.Event{
			ID: uuid.New(), TenantID: &tenant, Type: "ShiftApproved",
			AggregateType: "shift", AggregateID: uuid.NewString(),
			Payload: json.RawMessage(`{"checked_in_attendant_ids":["` +
				env.attendantA.String() + `","` + env.attendantB.String() + `"]}`),
		}
		env.publishTwice(t, e)
		rows := env.rowsForEvent(t, e.ID)
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want 2 (one per attendant, no dupes)", len(rows))
		}
		got := map[uuid.UUID]bool{}
		for _, r := range rows {
			if r.UserID == nil || r.Type != "shift.approved" || r.Severity != "success" {
				t.Fatalf("row = %+v, want targeted shift.approved/success", r)
			}
			got[*r.UserID] = true
		}
		if !got[env.attendantA] || !got[env.attendantB] {
			t.Fatalf("targets = %v, want both attendants", got)
		}
	})

	t.Run("tenant-wide mapping still dedupes", func(t *testing.T) {
		e := events.Event{
			ID: uuid.New(), TenantID: &tenant, Type: "ApprovalRequested",
			AggregateType: "approval", AggregateID: uuid.NewString(),
		}
		env.publishTwice(t, e)
		rows := env.rowsForEvent(t, e.ID)
		if len(rows) != 1 || rows[0].UserID != nil {
			t.Fatalf("rows = %+v, want exactly one tenant-wide row", rows)
		}
	})
}
