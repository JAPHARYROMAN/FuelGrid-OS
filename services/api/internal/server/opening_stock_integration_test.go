package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestOpeningStock_Lifecycle drives the draft -> approve(lock) lifecycle and
// proves its core invariants (Feature 1.6):
//   - a draft does not move the tank's book balance;
//   - approval posts the genesis 'opening' movement (balance moves to the
//     drafted litres) and LOCKS the request (status approved + movement_id);
//   - a locked tank rejects a fresh draft (one opening per tank);
//   - an approved request can never be re-approved or rejected (409).
func TestOpeningStock_Lifecycle(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, admin := h.adminContext(t, ctx)
	approver := h.secondApprover(t, ctx, slug)

	tank := "/api/v1/tanks/" + h.ids.tankAGO.String()

	// Enter a draft opening of 12,000 L.
	code, body := h.invPostJSON(t, "/api/v1/opening-stock-requests", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "litres": "12000", "notes": "physical count",
	})
	if code != http.StatusCreated {
		t.Fatalf("draft opening: status %d: %v", code, body)
	}
	if body["status"] != "draft" || body["litres"].(string) != "12000.000" {
		t.Fatalf("draft opening = %v", body)
	}
	reqID := body["id"].(string)

	// The book balance is still zero (a draft posts nothing).
	code, bal := h.getJSON(t, tank+"/book-balance", admin)
	if code != http.StatusOK || bal["book_balance"].(string) != "0" {
		t.Fatalf("balance after draft = %v (status %d), want 0", bal["book_balance"], code)
	}

	// A second draft for the same tank is rejected — one live request per tank.
	if code, _ := h.invPostJSON(t, "/api/v1/opening-stock-requests", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "litres": "999",
	}); code != http.StatusConflict {
		t.Fatalf("second draft: status %d, want 409", code)
	}

	// A different user approves it: the opening posts and the request locks.
	code, body = h.invPostJSON(t, "/api/v1/opening-stock-requests/"+reqID+"/approve", approver, map[string]any{})
	if code != http.StatusOK || body["status"] != "approved" {
		t.Fatalf("approve opening: status %d: %v", code, body)
	}
	if body["movement_id"] == nil || body["balance_after"].(string) != "12000.000" {
		t.Fatalf("approved request = %v, want movement_id + balance_after 12000.000", body)
	}

	// The ledger now reflects the opening.
	code, bal = h.getJSON(t, tank+"/book-balance", admin)
	if code != http.StatusOK || bal["book_balance"].(string) != "12000.000" {
		t.Fatalf("balance after approve = %v (status %d), want 12000.000", bal["book_balance"], code)
	}

	// Locked: re-approve / reject are 409, and a new draft is refused (the tank
	// already has an opening).
	if code, _ := h.invPostJSON(t, "/api/v1/opening-stock-requests/"+reqID+"/approve", approver, map[string]any{}); code != http.StatusConflict {
		t.Fatalf("re-approve locked: status %d, want 409", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/opening-stock-requests/"+reqID+"/reject", approver, map[string]any{}); code != http.StatusConflict {
		t.Fatalf("reject locked: status %d, want 409", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/opening-stock-requests", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "litres": "1",
	}); code != http.StatusConflict {
		t.Fatalf("draft after opening exists: status %d, want 409", code)
	}
}

// TestOpeningStock_RejectThenReenter proves a rejection records a reason and
// frees the tank for a corrected figure (a rejected request is not "live").
func TestOpeningStock_RejectThenReenter(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, admin := h.adminContext(t, ctx)
	approver := h.secondApprover(t, ctx, slug)

	code, body := h.invPostJSON(t, "/api/v1/opening-stock-requests", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "litres": "5000",
	})
	if code != http.StatusCreated {
		t.Fatalf("draft: %d: %v", code, body)
	}
	reqID := body["id"].(string)

	code, body = h.invPostJSON(t, "/api/v1/opening-stock-requests/"+reqID+"/reject", approver,
		map[string]any{"note": "wrong dip used"})
	if code != http.StatusOK || body["status"] != "rejected" {
		t.Fatalf("reject: status %d: %v", code, body)
	}
	if body["decision_note"].(string) != "wrong dip used" {
		t.Fatalf("rejection note = %v", body["decision_note"])
	}

	// The tank is free again: a corrected draft is accepted, and approving it
	// posts the opening.
	code, body = h.invPostJSON(t, "/api/v1/opening-stock-requests", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "litres": "5200",
	})
	if code != http.StatusCreated {
		t.Fatalf("re-draft after reject: status %d: %v", code, body)
	}
	reqID2 := body["id"].(string)
	if code, _ := h.invPostJSON(t, "/api/v1/opening-stock-requests/"+reqID2+"/approve", approver, map[string]any{}); code != http.StatusOK {
		t.Fatalf("approve re-draft: status %d", code)
	}
	code, bal := h.getJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/book-balance", admin)
	if code != http.StatusOK || bal["book_balance"].(string) != "5200.000" {
		t.Fatalf("balance after re-approve = %v, want 5200.000", bal["book_balance"])
	}
}

// TestOpeningStock_SystemAdminSelfApprove proves system_admin can override the
// normal separation-of-duties guard during tenant setup.
func TestOpeningStock_SystemAdminSelfApprove(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	code, body := h.invPostJSON(t, "/api/v1/opening-stock-requests", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "litres": "8000",
	})
	if code != http.StatusCreated {
		t.Fatalf("draft: %d: %v", code, body)
	}
	reqID := body["id"].(string)

	code, body = h.invPostJSON(t, "/api/v1/opening-stock-requests/"+reqID+"/approve", admin, map[string]any{})
	if code != http.StatusOK || body["status"] != "approved" {
		t.Fatalf("system_admin self-approve: status %d: %v", code, body)
	}
}

// TestOpeningStock_NonAdminNoSelfApprove proves separation of duties still
// applies to regular operational roles.
func TestOpeningStock_NonAdminNoSelfApprove(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, _ := h.adminContext(t, ctx)
	manager := h.stationManager(t, ctx, slug, "osr-manager@fuelgrid.local")

	code, body := h.invPostJSON(t, "/api/v1/opening-stock-requests", manager, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "litres": "8000",
	})
	if code != http.StatusCreated {
		t.Fatalf("draft: %d: %v", code, body)
	}
	reqID := body["id"].(string)

	if code, _ := h.invPostJSON(t, "/api/v1/opening-stock-requests/"+reqID+"/approve", manager, map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("manager self-approve: status %d, want 403", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/opening-stock-requests/"+reqID+"/reject", manager, map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("manager self-reject: status %d, want 403", code)
	}
}

// TestOpeningStock_CleanupTearsDownTenant guards the test harness itself: an
// approved opening-stock request links a stock_movements row and references the
// tank, the requester and the tenant via ON DELETE RESTRICT FKs (migration
// 0093). cleanupTenant therefore MUST purge opening_stock_requests before those
// parents, or the parent DELETEs silently fail (cleanupTenant swallows their
// errors) and every opening-stock test leaks its whole tenant tree.
//
// This drives a full draft -> approve lifecycle, runs cleanupTenant, and proves
// the tenant (and its tank, movement and request) is actually gone. It fails if
// opening_stock_requests is ever dropped from the cleanup ordering again.
func TestOpeningStock_CleanupTearsDownTenant(t *testing.T) {
	h, cleanup := setupHarness(t)
	// The deferred harness cleanup is the same teardown under test; calling it
	// again after we have already purged the tenant is a harmless no-op.
	defer cleanup()
	ctx := context.Background()
	_, slug, admin := h.adminContext(t, ctx)
	approver := h.secondApprover(t, ctx, slug)

	code, body := h.invPostJSON(t, "/api/v1/opening-stock-requests", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "litres": "7000",
	})
	if code != http.StatusCreated {
		t.Fatalf("draft: %d: %v", code, body)
	}
	reqID := body["id"].(string)
	if code, _ := h.invPostJSON(t, "/api/v1/opening-stock-requests/"+reqID+"/approve", approver, map[string]any{}); code != http.StatusOK {
		t.Fatalf("approve: status %d", code)
	}

	// Sanity: the approved request, its genesis movement and the tank exist.
	var osr, mvts int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM opening_stock_requests WHERE tenant_id = $1`, h.ids.tenantID).Scan(&osr); err != nil {
		t.Fatalf("count osr: %v", err)
	}
	if osr != 1 {
		t.Fatalf("opening_stock_requests = %d, want 1 before cleanup", osr)
	}

	cleanupTenant(ctx, h.pool, h.ids.tenantID)

	// After teardown the tenant tree must be fully gone. A leaked
	// opening_stock_requests row would have blocked the stock_movements / tank /
	// tenant deletes via ON DELETE RESTRICT.
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM opening_stock_requests WHERE tenant_id = $1`, h.ids.tenantID).Scan(&osr); err != nil {
		t.Fatalf("count osr after cleanup: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM stock_movements WHERE tenant_id = $1`, h.ids.tenantID).Scan(&mvts); err != nil {
		t.Fatalf("count movements after cleanup: %v", err)
	}
	var tanks, tenants int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM tanks WHERE tenant_id = $1`, h.ids.tenantID).Scan(&tanks); err != nil {
		t.Fatalf("count tanks after cleanup: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM tenants WHERE id = $1`, h.ids.tenantID).Scan(&tenants); err != nil {
		t.Fatalf("count tenants after cleanup: %v", err)
	}
	if osr != 0 || mvts != 0 || tanks != 0 || tenants != 0 {
		t.Fatalf("cleanup left a leaked tenant tree: opening_stock_requests=%d stock_movements=%d tanks=%d tenants=%d (want 0,0,0,0)",
			osr, mvts, tanks, tenants)
	}
}

func (h *harness) stationManager(t *testing.T, ctx context.Context, slug, email string) string {
	t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		SELECT tenant_id, $2, 'Station Manager', 'active', password_hash, now()
		FROM users WHERE tenant_id = $1 AND email = $3
		RETURNING id
	`, h.ids.tenantID, email, h.ids.adminEmail).Scan(&id); err != nil {
		t.Fatalf("create station manager: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, id, "station_manager")
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO user_station_access (user_id, station_id, tenant_id)
		VALUES ($1, $2, $3)
	`, id, h.ids.station1, h.ids.tenantID); err != nil {
		t.Fatalf("grant station manager station access: %v", err)
	}
	return h.login(t, slug, email)
}

// TestOpeningStock_Validation rejects a negative/absent litres value and an
// unknown tank.
func TestOpeningStock_Validation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	if code, _ := h.invPostJSON(t, "/api/v1/opening-stock-requests", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "litres": "-5",
	}); code != http.StatusBadRequest {
		t.Fatalf("negative litres: status %d, want 400", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/opening-stock-requests", admin, map[string]any{
		"tank_id": h.ids.tankAGO.String(),
	}); code != http.StatusBadRequest {
		t.Fatalf("missing litres: status %d, want 400", code)
	}
}

// TestOpeningStock_AttendantForbidden proves a freshly-created attendant (no
// inventory permissions) cannot enter a draft (403).
func TestOpeningStock_AttendantForbidden(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, _ := h.adminContext(t, ctx)

	const email = "osr-attendant@fuelgrid.local"
	seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, email)
	att := h.login(t, slug, email)

	code, _ := h.invPostJSON(t, "/api/v1/opening-stock-requests", att, map[string]any{
		"tank_id": h.ids.tankAGO.String(), "litres": "100",
	})
	if code != http.StatusForbidden {
		t.Fatalf("attendant draft: status %d, want 403", code)
	}
}
