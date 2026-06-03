package server_test

import (
	"context"
	"net/http"
	"testing"
)

// TestStockAdjustment_Lifecycle drives the full request -> approve -> post
// lifecycle and proves its core invariants (Feature 5.4):
//   - the book balance moves by exactly the signed delta only at post;
//   - the ledger records before/after via the posted movement's balance_after;
//   - posting is idempotent: a posted adjustment cannot re-post (409) and the
//     ledger is not double-applied.
func TestStockAdjustment_Lifecycle(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, admin := h.adminContext(t, ctx)
	approver := h.secondApprover(t, ctx, slug)

	tank := "/api/v1/tanks/" + h.ids.tankAGO.String()

	// Seed an opening balance so the tank has a ledger to correct.
	if code, _ := h.invPostJSON(t, tank+"/opening-balance", admin, map[string]any{"litres": 10000}); code != http.StatusCreated {
		t.Fatalf("set opening: %d", code)
	}

	// Request a -250L correction (e.g. evaporation shrinkage).
	code, body := h.invPostJSON(t, "/api/v1/stock-adjustments", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "delta_litres": "-250", "reason": "evaporation loss",
		"classification": "evaporation",
	})
	if code != http.StatusCreated {
		t.Fatalf("request adjustment: status %d: %v", code, body)
	}
	if body["status"] != "requested" || body["delta_litres"].(string) != "-250.000" {
		t.Fatalf("requested adjustment = %v", body)
	}
	adjID := body["id"].(string)

	// Book balance is unchanged while the adjustment is only requested.
	code, bal := h.getJSON(t, tank+"/book-balance", admin)
	if code != http.StatusOK || bal["book_balance"].(string) != "10000.000" {
		t.Fatalf("balance after request = %v (status %d), want unchanged", bal["book_balance"], code)
	}

	// A different user approves it (separation of duties satisfied).
	code, body = h.invPostJSON(t, "/api/v1/stock-adjustments/"+adjID+"/approve", approver, map[string]any{})
	if code != http.StatusOK || body["status"] != "approved" {
		t.Fatalf("approve adjustment: status %d: %v", code, body)
	}

	// Still unchanged until posted.
	code, bal = h.getJSON(t, tank+"/book-balance", admin)
	if code != http.StatusOK || bal["book_balance"].(string) != "10000.000" {
		t.Fatalf("balance after approve = %v (status %d), want unchanged", bal["book_balance"], code)
	}

	// Post it: the ledger moves by exactly -250 and the snapshots are recorded.
	code, body = h.invPostJSON(t, "/api/v1/stock-adjustments/"+adjID+"/post", approver, map[string]any{})
	if code != http.StatusOK || body["status"] != "posted" {
		t.Fatalf("post adjustment: status %d: %v", code, body)
	}
	if body["balance_before"].(string) != "10000.000" || body["balance_after"].(string) != "9750.000" {
		t.Fatalf("posted snapshots = before %v after %v, want 10000/9750", body["balance_before"], body["balance_after"])
	}
	if body["movement_id"] == nil {
		t.Fatalf("posted adjustment has no movement_id: %v", body)
	}

	code, bal = h.getJSON(t, tank+"/book-balance", admin)
	if code != http.StatusOK || bal["book_balance"].(string) != "9750.000" {
		t.Fatalf("balance after post = %v (status %d), want 9750.000", bal["book_balance"], code)
	}

	// Idempotent: a posted adjustment cannot be posted again (409) and the
	// ledger is not double-applied.
	code, _ = h.invPostJSON(t, "/api/v1/stock-adjustments/"+adjID+"/post", approver, map[string]any{})
	if code != http.StatusConflict {
		t.Fatalf("re-post posted adjustment: status %d, want 409", code)
	}
	code, bal = h.getJSON(t, tank+"/book-balance", admin)
	if code != http.StatusOK || bal["book_balance"].(string) != "9750.000" {
		t.Fatalf("balance after re-post attempt = %v (status %d), want still 9750.000", bal["book_balance"], code)
	}

	// A posted adjustment can no longer be approved/rejected either.
	if code, _ := h.invPostJSON(t, "/api/v1/stock-adjustments/"+adjID+"/approve", approver, map[string]any{}); code != http.StatusConflict {
		t.Fatalf("approve posted adjustment: status %d, want 409", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/stock-adjustments/"+adjID+"/reject", approver, map[string]any{}); code != http.StatusConflict {
		t.Fatalf("reject posted adjustment: status %d, want 409", code)
	}
}

// TestStockAdjustment_NoSelfApprove proves separation of duties: the requester
// cannot approve or reject their own adjustment, and an unapproved adjustment
// cannot be posted.
func TestStockAdjustment_NoSelfApprove(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	if code, _ := h.invPostJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/opening-balance", admin, map[string]any{"litres": 5000}); code != http.StatusCreated {
		t.Fatalf("set opening: %d", code)
	}

	code, body := h.invPostJSON(t, "/api/v1/stock-adjustments", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "delta_litres": "100", "reason": "found extra",
		"classification": "measurement_error",
	})
	if code != http.StatusCreated {
		t.Fatalf("request adjustment: %d: %v", code, body)
	}
	adjID := body["id"].(string)

	// The requester (admin) cannot approve their own adjustment.
	if code, _ := h.invPostJSON(t, "/api/v1/stock-adjustments/"+adjID+"/approve", admin, map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("self-approve: status %d, want 403", code)
	}
	// Nor reject it.
	if code, _ := h.invPostJSON(t, "/api/v1/stock-adjustments/"+adjID+"/reject", admin, map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("self-reject: status %d, want 403", code)
	}
	// A requested (un-approved) adjustment cannot be posted.
	if code, _ := h.invPostJSON(t, "/api/v1/stock-adjustments/"+adjID+"/post", admin, map[string]any{}); code != http.StatusConflict {
		t.Fatalf("post un-approved adjustment: status %d, want 409", code)
	}
}

// TestStockAdjustment_Validation rejects a zero delta, an unknown
// classification, and a missing reason.
func TestStockAdjustment_Validation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	base := map[string]any{
		"tank_id": h.ids.tankAGO.String(), "delta_litres": "100",
		"reason": "ok", "classification": "other",
	}
	clone := func(over map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range over {
			m[k] = v
		}
		return m
	}

	if code, _ := h.invPostJSON(t, "/api/v1/stock-adjustments", admin, clone(map[string]any{"delta_litres": "0"})); code != http.StatusBadRequest {
		t.Fatalf("zero delta: status %d, want 400", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/stock-adjustments", admin, clone(map[string]any{"classification": "bogus"})); code != http.StatusBadRequest {
		t.Fatalf("bad classification: status %d, want 400", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/stock-adjustments", admin, clone(map[string]any{"reason": "   "})); code != http.StatusBadRequest {
		t.Fatalf("blank reason: status %d, want 400", code)
	}
}

// TestStockAdjustment_PermissionScope proves the station-scoped permission gate:
// a station_manager scoped to station1 cannot request an adjustment against a
// station2 tank.
func TestStockAdjustment_PermissionScope(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, _ := h.adminContext(t, ctx)
	op := h.login(t, slug, h.ids.opEmail)

	// op is scoped to station1; tankMSA is on station2.
	code, _ := h.invPostJSON(t, "/api/v1/stock-adjustments", op, map[string]any{
		"tank_id": h.ids.tankMSA.String(), "delta_litres": "10", "reason": "x", "classification": "other",
	})
	if code != http.StatusForbidden {
		t.Fatalf("out-of-scope request: status %d, want 403", code)
	}
}
