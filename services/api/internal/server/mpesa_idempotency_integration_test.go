package server_test

// DB-backed idempotency proof for the M-Pesa (Safaricom Daraja) collection
// write path (C.2). This is the one duplicate-protected write path that lacked
// a test: the sales-posting, stock-adjustment, sale-void, and tank-opening
// paths are already covered (phase4 / stock_adjustments / sale_voids suites).
//
// Daraja delivers its result callback at-least-once and retries until it gets
// the success ack, so the settle path MUST be idempotent: a duplicated callback
// for the same checkout id must NOT re-apply (no second row, no flip of an
// already-terminal transaction, no clobbered receipt). The guards under test:
//
//   - InitiateMpesa: a repeated STK-push ack for the same (tenant,
//     checkout_request_id) is refused by the unique constraint
//     uq_mpesa_tenant_checkout (one pending row per checkout id);
//   - SettleMpesaByCheckoutID: a second callback for an already-terminal row is
//     a clean no-op that returns the existing row unchanged — never a re-settle.
//
// The mpesa HTTP handlers depend on a live Daraja client (s.mpesa) that the
// harness does not wire, so this drives the repo write path directly on the
// harness pool — the same direct-repo style the Phase 4 ledger suite uses.
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL; skips when either is unset.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/payments"
)

func TestMpesaCallback_Idempotent(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()

	repo := payments.New(h.pool)
	// The Daraja callback is unauthenticated, so the settle/lookup path keys on
	// checkout_request_id ALONE (no tenant scope). Use a per-run unique id so a
	// row left by a prior run cannot be matched by this run's lookup, and delete
	// the rows we create (cleanupTenant does not cover mpesa_transactions).
	checkoutID := fmt.Sprintf("ws_CO_idem_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(),
			`DELETE FROM mpesa_transactions WHERE checkout_request_id = $1`, checkoutID)
	})

	// A freshly-initiated STK push: one pending row keyed by checkout id.
	pending, err := repo.InitiateMpesa(ctx, h.pool, h.ids.tenantID, payments.InitiateMpesaInput{
		StationID:         h.ids.station1,
		CheckoutRequestID: checkoutID,
		MerchantRequestID: "mr-1",
		Amount:            "15000.00",
		Phone:             "254700000000",
		AccountReference:  "INV-1",
		Description:       "Fuel",
	})
	if err != nil {
		t.Fatalf("initiate mpesa: %v", err)
	}
	if pending.Status != "pending" {
		t.Fatalf("initial status = %q, want pending", pending.Status)
	}

	// A duplicate STK-push ack for the same checkout id is refused by the
	// (tenant_id, checkout_request_id) unique constraint — it never creates a
	// second pending row.
	if _, err := repo.InitiateMpesa(ctx, h.pool, h.ids.tenantID, payments.InitiateMpesaInput{
		StationID:         h.ids.station1,
		CheckoutRequestID: checkoutID,
		Amount:            "15000.00",
		Phone:             "254700000000",
	}); err == nil {
		t.Fatal("duplicate initiate for same checkout id: got nil error, want unique violation")
	}

	settle := payments.SettleMpesaInput{
		CheckoutRequestID: checkoutID,
		Status:            "paid",
		ResultCode:        0,
		MpesaReceipt:      "NLJ7RT61SV",
		RawPayload:        json.RawMessage(`{"Body":{"stkCallback":{"ResultCode":0}}}`),
	}

	// First callback settles the row to paid with the receipt.
	first, err := repo.SettleMpesaByCheckoutID(ctx, h.pool, settle)
	if err != nil {
		t.Fatalf("first settle: %v", err)
	}
	if first.Status != "paid" {
		t.Fatalf("status after first callback = %q, want paid", first.Status)
	}
	if first.MpesaReceipt == nil || *first.MpesaReceipt != "NLJ7RT61SV" {
		t.Fatalf("receipt after first callback = %v, want NLJ7RT61SV", first.MpesaReceipt)
	}

	// Second (duplicate) callback for the same checkout id is a no-op: it
	// returns the existing terminal row unchanged rather than re-settling.
	// A different result code/receipt in the retry must NOT overwrite the
	// already-applied terminal state — that is the whole point of the guard.
	second, err := repo.SettleMpesaByCheckoutID(ctx, h.pool, payments.SettleMpesaInput{
		CheckoutRequestID: checkoutID,
		Status:            "failed",
		ResultCode:        1032,
		MpesaReceipt:      "SHOULD-NOT-APPLY",
		RawPayload:        json.RawMessage(`{"Body":{"stkCallback":{"ResultCode":1032}}}`),
	})
	if err != nil {
		t.Fatalf("second (duplicate) settle: %v", err)
	}
	if second.Status != "paid" {
		t.Fatalf("status after duplicate callback = %q, want still paid (no re-settle)", second.Status)
	}
	if second.MpesaReceipt == nil || *second.MpesaReceipt != "NLJ7RT61SV" {
		t.Fatalf("receipt after duplicate callback = %v, want unchanged NLJ7RT61SV", second.MpesaReceipt)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate callback returned a different row id: %s vs %s", second.ID, first.ID)
	}

	// Exactly one mpesa row exists for this checkout id — the duplicate callback
	// (and the duplicate initiate) did not double-apply.
	var rows int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM mpesa_transactions
		WHERE tenant_id = $1 AND checkout_request_id = $2`,
		h.ids.tenantID, checkoutID).Scan(&rows); err != nil {
		t.Fatalf("count mpesa rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("mpesa rows for checkout id = %d, want exactly 1", rows)
	}
}

// TestMpesaCallback_UnknownCheckoutID proves a callback for a checkout id the
// system never issued is reported as not-found (so the webhook can log + ack it
// without persisting a phantom row) rather than silently creating one.
func TestMpesaCallback_UnknownCheckoutID(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()

	repo := payments.New(h.pool)
	// A per-run-unique id the system never issued (the settle path keys on
	// checkout id alone, so a fixed literal could collide with a leftover row).
	_, err := repo.SettleMpesaByCheckoutID(ctx, h.pool, payments.SettleMpesaInput{
		CheckoutRequestID: fmt.Sprintf("ws_CO_never_issued_%d", time.Now().UnixNano()),
		Status:            "paid",
		ResultCode:        0,
	})
	if !errors.Is(err, payments.ErrMpesaNotFound) {
		t.Fatalf("settle unknown checkout id: err = %v, want ErrMpesaNotFound", err)
	}

	var rows int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM mpesa_transactions WHERE tenant_id = $1`,
		h.ids.tenantID).Scan(&rows); err != nil {
		t.Fatalf("count mpesa rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("mpesa rows after unknown callback = %d, want 0 (no phantom row)", rows)
	}
}

// compile-time guard: the harness pool satisfies database.Querier, the type the
// repo write methods accept. Keeps the test honest if that interface changes.
var _ database.Querier = (*database.Pool)(nil)
