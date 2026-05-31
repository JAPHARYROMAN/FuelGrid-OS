package server_test

// DB-backed concurrency test for INV-003: the per-tank transaction-scoped
// advisory lock (pg_advisory_xact_lock) in inventory.PostMovement must keep
// the stock ledger consistent when many transactions post to the SAME tank at
// once.
//
// balance_after is derived inside the INSERT as SUM(litres) + this row's
// litres. Under READ COMMITTED without the lock, two concurrent posters read
// the same pre-image sum and write identical/inconsistent running balances —
// the snapshots no longer match the authoritative ledger sum. With the lock,
// posts to one tank serialize, so every balance_after is a gapless running
// total and the final book balance is the exact sum of all posted litres.
//
// This test fires N goroutines, each opening its own tx via h.pool.Begin and
// posting one movement to the shared tank, then asserts the invariants. It is
// designed to be run under the race detector (CI runs `go test -race`).
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL via the shared Phase 2 harness;
// it SKIPS when either is unset, so `go test ./...` stays green without infra.
// Locally:
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5433/fuelgrid?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6379/0 \
//	go test ./services/api/internal/server -run Inventory_ConcurrentPosts -race -v

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/inventory"
)

// milliLitres renders an integer count of milli-litres (litres * 1000) as the
// numeric(14,3) text form Postgres produces for the litres / balance_after
// columns — always exactly three decimal places, e.g. 100100000 ->
// "100100.000", -2500 -> "-2.500". Integer math keeps the expected strings
// exact (no float rounding) so the assertions match the DB byte-for-byte.
func milliLitres(mL int64) string {
	neg := mL < 0
	if neg {
		mL = -mL
	}
	s := fmt.Sprintf("%d.%03d", mL/1000, mL%1000)
	if neg {
		s = "-" + s
	}
	return s
}

// parseMilliLitres parses a numeric(14,3) text value (e.g. "100100.000",
// "-2.500", "0") back into an integer count of milli-litres. It reports
// ok=false on any unexpected shape so a malformed snapshot fails the test
// loudly rather than silently coercing.
func parseMilliLitres(s string) (mL int64, ok bool) {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	whole, frac := s, ""
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		whole, frac = s[:dot], s[dot+1:]
	}
	if whole == "" || len(frac) > 3 { // require an integer part; cap precision at milli-litres
		return 0, false
	}

	var w int64
	for i := 0; i < len(whole); i++ {
		if whole[i] < '0' || whole[i] > '9' {
			return 0, false
		}
		w = w*10 + int64(whole[i]-'0')
	}
	// Right-pad the fractional part to exactly three digits (milli-litres).
	var f int64
	for i := 0; i < 3; i++ {
		f *= 10
		if i < len(frac) {
			if frac[i] < '0' || frac[i] > '9' {
				return 0, false
			}
			f += int64(frac[i] - '0')
		}
	}

	mL = w*1000 + f
	if neg {
		mL = -mL
	}
	return mL, true
}

// TestInventory_ConcurrentPostsHoldLedgerConsistent proves INV-003: concurrent
// posts to one tank, each in its own committed transaction, yield a consistent
// ledger — exact final balance, a gapless running total in balance_after, and
// strictly increasing per-tank seq with no duplicate snapshots.
func TestInventory_ConcurrentPostsHoldLedgerConsistent(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()

	ctx := context.Background()
	repo := inventory.New(h.pool)

	var adminID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, h.ids.adminEmail).Scan(&adminID); err != nil {
		t.Fatalf("lookup admin id: %v", err)
	}

	// Open the tank with a known balance in its own committed tx, so the
	// concurrent posters all post flow movements onto an established ledger.
	const openingML int64 = 100_000_000 // 100000.000 L
	openTx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin opening tx: %v", err)
	}
	if _, err := repo.PostMovement(ctx, openTx, h.ids.tenantID, inventory.PostInput{
		TankID:       h.ids.tankPMS,
		MovementType: inventory.TypeOpening,
		Litres:       milliLitres(openingML),
		RecordedBy:   adminID,
	}); err != nil {
		_ = openTx.Rollback(ctx)
		t.Fatalf("post opening: %v", err)
	}
	if err := openTx.Commit(ctx); err != nil {
		t.Fatalf("commit opening: %v", err)
	}

	// N concurrent deliveries of a fixed size to the SAME tank, each in its own
	// transaction. The advisory lock must serialize them so balance_after stays
	// a true running total.
	const (
		n         = 20
		perPostML = 10_000 // 10.000 L per post
	)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			tx, err := h.pool.Begin(ctx)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("begin: %w", err))
				mu.Unlock()
				return
			}
			if _, err := repo.PostMovement(ctx, tx, h.ids.tenantID, inventory.PostInput{
				TankID:       h.ids.tankPMS,
				MovementType: inventory.TypeDelivery,
				Litres:       milliLitres(perPostML),
				RecordedBy:   adminID,
			}); err != nil {
				_ = tx.Rollback(ctx)
				mu.Lock()
				errs = append(errs, fmt.Errorf("post: %w", err))
				mu.Unlock()
				return
			}
			if err := tx.Commit(ctx); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("commit: %w", err))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("concurrent posts had %d error(s); first: %v", len(errs), errs[0])
	}

	// (a) The final CurrentBalance is the exact decimal-string sum of every
	// posted litre: opening + N * perPost.
	wantFinalML := openingML + int64(n)*int64(perPostML)
	wantFinal := milliLitres(wantFinalML)
	gotFinal, err := repo.CurrentBalance(ctx, h.ids.tenantID, h.ids.tankPMS)
	if err != nil {
		t.Fatalf("current balance: %v", err)
	}
	if gotFinal != wantFinal {
		t.Fatalf("final CurrentBalance = %q, want %q", gotFinal, wantFinal)
	}

	// Read the ledger back in append order to check the snapshots.
	movements, err := repo.ListMovements(ctx, h.ids.tenantID, h.ids.tankPMS)
	if err != nil {
		t.Fatalf("list movements: %v", err)
	}
	if len(movements) != n+1 { // opening + N deliveries
		t.Fatalf("ledger has %d rows, want %d (opening + %d posts)", len(movements), n+1, n)
	}

	// (b)/(c) Walk the ledger ordered by seq (ListMovements ORDERs by seq) and
	// verify: seq strictly increases (gapless append order, no duplicate seq),
	// and balance_after is a gapless running total — each equals the cumulative
	// sum of litres up to and including that row, with no duplicate snapshot. A
	// lost lock would let two posters write the same pre-image sum, producing a
	// duplicate balance_after that no longer equals the cumulative sum.
	var (
		runningML int64
		prevSeq   int64 = -1
		seen            = make(map[string]int, len(movements))
	)
	for i, m := range movements {
		// (c) seq is strictly monotonic across the tank's ledger.
		if m.Seq <= prevSeq {
			t.Fatalf("row %d: seq %d not strictly greater than previous %d (lock failed to serialize)", i, m.Seq, prevSeq)
		}
		prevSeq = m.Seq

		litresML, ok := parseMilliLitres(m.Litres)
		if !ok {
			t.Fatalf("row %d: unparseable litres %q", i, m.Litres)
		}
		runningML += litresML
		wantBal := milliLitres(runningML)

		// (b) balance_after is the exact cumulative running total at this row.
		if m.BalanceAfter != wantBal {
			t.Fatalf("row %d (seq %d): balance_after = %q, want %q (running total broke — lock did not serialize)",
				i, m.Seq, m.BalanceAfter, wantBal)
		}
		// No duplicate snapshot: every post advances the balance, so each
		// balance_after is unique. A duplicate is the classic symptom of two
		// posts reading the same pre-image sum.
		if prev, dup := seen[m.BalanceAfter]; dup {
			t.Fatalf("row %d (seq %d): duplicate balance_after %q (also at row %d) — concurrent posts saw the same sum",
				i, m.Seq, m.BalanceAfter, prev)
		}
		seen[m.BalanceAfter] = i
	}

	// The walked running total must land on the authoritative balance too.
	if got := milliLitres(runningML); got != wantFinal {
		t.Fatalf("walked running total %q != final balance %q", got, wantFinal)
	}
}
