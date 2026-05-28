package server_test

// DB-backed integration tests for the Phase 4 stock ledger (Stage 1). They
// reuse the Phase 2 harness (setupHarness / seedTenant), post movements
// straight onto the ledger via the inventory repo, and assert the read
// surface — ledger ordering, derived book balance, reversal, and the
// station-scoped inventory.read gate — over the real HTTP API.
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the Phase 2 suite; skips
// when either is unset.

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/inventory"
)

func TestPhase4_StockLedger(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()

	ctx := context.Background()
	repo := inventory.New(h.pool)

	var adminID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, h.ids.adminEmail).Scan(&adminID); err != nil {
		t.Fatalf("lookup admin id: %v", err)
	}

	// Seed an opening + delivery + sales movement on the PMS tank in one tx.
	// Expected book balance: 20000 + 10000 - 4200 = 25800.
	var deliveryID uuid.UUID
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	post := func(mvType string, litres float64) *inventory.Movement {
		m, err := repo.PostMovement(ctx, tx, h.ids.tenantID, inventory.PostInput{
			TankID: h.ids.tankPMS, MovementType: mvType, Litres: litres, RecordedBy: adminID,
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("post %s: %v", mvType, err)
		}
		return m
	}
	post(inventory.TypeOpening, 20000)
	delivery := post(inventory.TypeDelivery, 10000)
	deliveryID = delivery.ID
	post(inventory.TypeSales, -4200)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// The tenant slug is "ittest-<suffix>"; recover it from the DB.
	var slug string
	if err := h.pool.QueryRow(ctx, `SELECT slug FROM tenants WHERE id = $1`, h.ids.tenantID).Scan(&slug); err != nil {
		t.Fatalf("lookup slug: %v", err)
	}
	admin := h.login(t, slug, h.ids.adminEmail)
	op := h.login(t, slug, h.ids.opEmail)

	// Book balance reflects the derived sum.
	code, body := h.getJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/book-balance", admin)
	if code != http.StatusOK {
		t.Fatalf("book-balance: status %d: %v", code, body)
	}
	if got := body["book_balance"].(float64); got != 25800 {
		t.Fatalf("book_balance = %v, want 25800", got)
	}

	// Ledger lists the three movements in append order with running balances.
	code, body = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/ledger", admin)
	if code != http.StatusOK {
		t.Fatalf("ledger: status %d: %v", code, body)
	}
	items := body["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("ledger count = %d, want 3", len(items))
	}
	wantTypes := []string{"opening", "delivery", "sales"}
	wantBalances := []float64{20000, 30000, 25800}
	for i, raw := range items {
		it := raw.(map[string]any)
		if it["movement_type"] != wantTypes[i] {
			t.Fatalf("item %d type = %v, want %s", i, it["movement_type"], wantTypes[i])
		}
		if bal := it["balance_after"].(float64); bal != wantBalances[i] {
			t.Fatalf("item %d balance_after = %v, want %v", i, bal, wantBalances[i])
		}
	}

	// Reverse the delivery: a contra entry nets it to zero, balance drops to
	// 15800, and the ledger grows to four rows (original now 'reversed').
	tx, err = h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin reverse: %v", err)
	}
	contra, err := repo.ReverseMovement(ctx, tx, h.ids.tenantID, deliveryID, adminID, nil)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("reverse: %v", err)
	}
	if contra.Litres != -10000 {
		_ = tx.Rollback(ctx)
		t.Fatalf("contra litres = %v, want -10000", contra.Litres)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit reverse: %v", err)
	}

	// Reversing again is rejected.
	tx, err = h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin reverse2: %v", err)
	}
	_, err = repo.ReverseMovement(ctx, tx, h.ids.tenantID, deliveryID, adminID, nil)
	_ = tx.Rollback(ctx)
	if err != inventory.ErrAlreadyReversed {
		t.Fatalf("second reverse err = %v, want ErrAlreadyReversed", err)
	}

	code, body = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/book-balance", admin)
	if code != http.StatusOK {
		t.Fatalf("book-balance after reverse: status %d", code)
	}
	if got := body["book_balance"].(float64); got != 15800 {
		t.Fatalf("book_balance after reverse = %v, want 15800", got)
	}
	code, body = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/ledger", admin)
	if code != http.StatusOK {
		t.Fatalf("ledger after reverse: status %d", code)
	}
	if items := body["items"].([]any); len(items) != 4 {
		t.Fatalf("ledger count after reverse = %d, want 4", len(items))
	}

	// inventory.read is station-scoped: the station1-restricted operator may
	// read the PMS tank (station1) but not the station2 tank.
	code, _ = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/book-balance", op)
	if code != http.StatusOK {
		t.Fatalf("operator read in-scope tank: status %d, want 200", code)
	}
	code, _ = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankMSA.String()+"/book-balance", op)
	if code != http.StatusForbidden {
		t.Fatalf("operator read out-of-scope tank: status %d, want 403", code)
	}
}
