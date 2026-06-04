package server_test

// DB-backed idempotency proof for the general payment-record write path (SR-M2).
//
// payments.Record had no duplicate protection: a double-clicked submit, a
// client/network retry, or a replayed request inserted a SECOND payment row for
// the same logical tender, double-counting cash/mobile-money/card against a
// shift. The fix (migration 0096) adds a client-supplied idempotency_key with a
// partial unique index on (tenant_id, idempotency_key) WHERE idempotency_key IS
// NOT NULL; Record returns the already-recorded row (Replayed=true) on conflict
// instead of inserting again.
//
// These tests drive the repo write path directly on the harness pool — the same
// direct-repo style the M-Pesa idempotency suite uses — because the guard lives
// in the repo/DB layer and that is where the at-least-once retry collides.
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL; skips when either is unset.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/payments"
)

// seedOperatingDayShift creates an operating day + an open shift on the given
// station so a payment can reference it (payments.shift_id FK). Returns the
// shift id; the rows are cleaned up by cleanupTenant for the harness tenant.
func seedOperatingDayShift(t *testing.T, ctx context.Context, h *harness, tenantID, stationID, openedBy uuid.UUID) uuid.UUID {
	t.Helper()
	var dayID, shiftID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO operating_days (tenant_id, station_id, business_date, opened_by)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, tenantID, stationID, time.Now().Format("2006-01-02"), openedBy).Scan(&dayID); err != nil {
		t.Fatalf("seed operating day: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO shifts (tenant_id, station_id, operating_day_id, name, opened_by)
		VALUES ($1, $2, $3, 'Idem', $4) RETURNING id
	`, tenantID, stationID, dayID, openedBy).Scan(&shiftID); err != nil {
		t.Fatalf("seed shift: %v", err)
	}
	return shiftID
}

// countPaymentsForKey returns how many payment rows carry the given
// (tenant_id, idempotency_key) — the dedup invariant under test.
func countPaymentsForKey(t *testing.T, ctx context.Context, h *harness, tenantID uuid.UUID, key string) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM payments WHERE tenant_id = $1 AND idempotency_key = $2
	`, tenantID, key).Scan(&n); err != nil {
		t.Fatalf("count payments for key: %v", err)
	}
	return n
}

// recordOnce runs payments.Record inside its own committed transaction, the way
// the HTTP handler does (one tx per request). Returns the repo result.
func recordOnce(t *testing.T, ctx context.Context, h *harness, repo *payments.Repo, tenantID uuid.UUID, in payments.RecordInput) *payments.RecordResult {
	t.Helper()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	res, err := repo.Record(ctx, tx, tenantID, in)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("record payment: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	return res
}

// TestPayment_IdempotencyKey_Dedup proves the SR-M2 guard end-to-end on the DB:
// two records with the SAME (tenant, idempotency_key) collapse to ONE row and a
// consistent idempotent response; different / no keys insert normally; and the
// same key in a DIFFERENT tenant does NOT collide (tenant scoping).
func TestPayment_IdempotencyKey_Dedup(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()

	repo := payments.New(h.pool)
	shiftID := seedOperatingDayShift(t, ctx, h, h.ids.tenantID, h.ids.station1, h.ids.opID)
	key := fmt.Sprintf("idem-%d", time.Now().UnixNano())

	in := payments.RecordInput{
		StationID:      h.ids.station1,
		ShiftID:        &shiftID,
		TenderType:     "cash",
		Amount:         "15000.50",
		ReceivedBy:     h.ids.opID,
		IdempotencyKey: &key,
	}

	// First submit: a fresh insert.
	first := recordOnce(t, ctx, h, repo, h.ids.tenantID, in)
	if first.Replayed {
		t.Fatal("first record: Replayed = true, want false (fresh insert)")
	}
	if first.Payment.IdempotencyKey == nil || *first.Payment.IdempotencyKey != key {
		t.Fatalf("first record: idempotency_key = %v, want %q", first.Payment.IdempotencyKey, key)
	}
	if first.Payment.Amount != "15000.50" {
		t.Fatalf("first record: amount = %q, want 15000.50 (decimal preserved)", first.Payment.Amount)
	}

	// (a) Replay with the SAME key: returns the existing row, Replayed=true, no
	// second insert and no second application of the amount.
	second := recordOnce(t, ctx, h, repo, h.ids.tenantID, in)
	if !second.Replayed {
		t.Fatal("replay: Replayed = false, want true (idempotent hit)")
	}
	if second.Payment.ID != first.Payment.ID {
		t.Fatalf("replay returned a different row id: %s vs %s", second.Payment.ID, first.Payment.ID)
	}
	if second.Payment.Amount != first.Payment.Amount {
		t.Fatalf("replay amount = %q, want unchanged %q", second.Payment.Amount, first.Payment.Amount)
	}
	if n := countPaymentsForKey(t, ctx, h, h.ids.tenantID, key); n != 1 {
		t.Fatalf("payment rows for key after replay = %d, want exactly 1 (no double-apply)", n)
	}

	// (b) A DIFFERENT key inserts a distinct row (normal behaviour).
	otherKey := key + "-other"
	otherIn := in
	otherIn.IdempotencyKey = &otherKey
	other := recordOnce(t, ctx, h, repo, h.ids.tenantID, otherIn)
	if other.Replayed {
		t.Fatal("different key: Replayed = true, want false")
	}
	if other.Payment.ID == first.Payment.ID {
		t.Fatal("different key reused the first row id; want a distinct insert")
	}

	// (b) NO key falls back to the prior always-insert behaviour: two records
	// with no key are two distinct rows.
	noKeyIn := in
	noKeyIn.IdempotencyKey = nil
	nk1 := recordOnce(t, ctx, h, repo, h.ids.tenantID, noKeyIn)
	nk2 := recordOnce(t, ctx, h, repo, h.ids.tenantID, noKeyIn)
	if nk1.Replayed || nk2.Replayed {
		t.Fatal("no-key records reported Replayed; want always-insert")
	}
	if nk1.Payment.ID == nk2.Payment.ID {
		t.Fatal("no-key records collapsed to one row; want two distinct inserts")
	}

	// (c) Tenant scoping: the SAME key under a DIFFERENT tenant must NOT collide.
	t2 := seedSecondTenant(t, ctx, h)
	defer cleanupTenant(ctx, h.pool, t2.tenantID)
	t2Shift := seedOperatingDayShift(t, ctx, h, t2.tenantID, t2.stationID, t2.userID)
	t2In := payments.RecordInput{
		StationID:      t2.stationID,
		ShiftID:        &t2Shift,
		TenderType:     "cash",
		Amount:         "999.00",
		ReceivedBy:     t2.userID,
		IdempotencyKey: &key, // same literal key as tenant 1
	}
	t2Res := recordOnce(t, ctx, h, repo, t2.tenantID, t2In)
	if t2Res.Replayed {
		t.Fatal("same key in a different tenant: Replayed = true, want false (no cross-tenant collision)")
	}
	if t2Res.Payment.ID == first.Payment.ID {
		t.Fatal("same key in a different tenant returned tenant-1's row; tenant scoping broken")
	}
	if n := countPaymentsForKey(t, ctx, h, t2.tenantID, key); n != 1 {
		t.Fatalf("tenant-2 payment rows for key = %d, want exactly 1", n)
	}
	// Tenant 1 still has exactly its one row for the key — unaffected.
	if n := countPaymentsForKey(t, ctx, h, h.ids.tenantID, key); n != 1 {
		t.Fatalf("tenant-1 payment rows for key after tenant-2 insert = %d, want exactly 1", n)
	}
}

// secondTenant is a minimal independent tenant (tenant + company + station +
// user) used to prove idempotency-key dedup is tenant-scoped.
type secondTenant struct {
	tenantID  uuid.UUID
	stationID uuid.UUID
	userID    uuid.UUID
}

func seedSecondTenant(t *testing.T, ctx context.Context, h *harness) secondTenant {
	t.Helper()
	suffix := time.Now().UnixNano()
	var st secondTenant
	q := func(dest *uuid.UUID, sql string, args ...any) {
		if err := h.pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seed second tenant %q: %v", sql, err)
		}
	}
	q(&st.tenantID, `INSERT INTO tenants (name, slug) VALUES ('IT Co 2', $1) RETURNING id`,
		fmt.Sprintf("ittest2-%d", suffix))
	var companyID uuid.UUID
	q(&companyID, `INSERT INTO companies (tenant_id, name) VALUES ($1, 'IT Co 2') RETURNING id`, st.tenantID)
	q(&st.stationID, `INSERT INTO stations (tenant_id, company_id, name, code) VALUES ($1, $2, 'S2', 'S2-01') RETURNING id`, st.tenantID, companyID)
	q(&st.userID, `INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at) VALUES ($1, $2, 'T2 User', 'active', 'x', now()) RETURNING id`,
		st.tenantID, fmt.Sprintf("t2-%d@it.local", suffix))
	return st
}
