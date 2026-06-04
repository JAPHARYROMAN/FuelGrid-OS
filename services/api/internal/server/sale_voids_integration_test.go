package server_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// recognizeOneSale seeds a costed PMS delivery, a selling price, a closed shift
// that sold litresSold, approves the shift to recognize one sale, and returns
// the operating-day id and the recognized sale's id.
func recognizeOneSale(t *testing.T, ctx context.Context, h *harness, adminID uuid.UUID, admin, businessDate string, litresSold float64) (dayID uuid.UUID, saleID string) {
	t.Helper()

	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`,
		h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("nozzle: %v", err)
	}

	pms := "/api/v1/tanks/" + h.ids.tankPMS.String()
	if code, _ := h.invPostJSON(t, pms+"/opening-balance", admin, map[string]any{"litres": 30000}); code != http.StatusCreated {
		t.Fatalf("open: %d", code)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO stock_movements
		    (tenant_id, tank_id, movement_type, source_ref_type, litres, balance_after, recorded_by, landed_cost_per_litre, landed_cost_total)
		VALUES ($1, $2, 'delivery', 'delivery', 10000, 40000, $3, 2400, 24000000)
	`, h.ids.tenantID, h.ids.tankPMS, adminID); err != nil {
		t.Fatalf("seed costed delivery: %v", err)
	}

	station := "/api/v1/stations/" + h.ids.station1.String()
	if code, _ := h.invPostJSON(t, station+"/prices", admin,
		map[string]any{"product_id": h.ids.pmsProduct.String(), "unit_price": "2950"}); code != http.StatusCreated {
		t.Fatalf("set price: %d", code)
	}

	day, shift := seedClosedDayShift(t, ctx, h, adminID, nozzleID, businessDate, litresSold)
	if code, raw := h.do(t, http.MethodPatch, "/api/v1/shifts/"+shift.String()+"/status", admin,
		bytes.NewReader([]byte(`{"status":"approved"}`)), "application/json"); code != http.StatusOK {
		t.Fatalf("approve shift: %d %s", code, raw)
	}

	code, body := h.getJSON(t, "/api/v1/shifts/"+shift.String()+"/sales", admin)
	items, _ := body["items"].([]any)
	if code != http.StatusOK || len(items) != 1 {
		t.Fatalf("recognized sales: status %d count %d", code, len(items))
	}
	return day, items[0].(map[string]any)["id"].(string)
}

// TestSaleVoid_Lifecycle drives the full request -> approve lifecycle and proves
// the Feature 4.3 invariants:
//   - the original sale is PRESERVED (never deleted/mutated);
//   - approving records the reversal (sale amounts negated) and the day rollup
//     nets to zero, while the sale row itself is unchanged;
//   - approving is idempotent: a second approve is a 409 and the rollup is not
//     double-reversed;
//   - the sale list surfaces void_status so a reversed sale is distinguishable.
func TestSaleVoid_Lifecycle(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, slug, admin := h.adminContext(t, ctx)
	approver := h.secondApprover(t, ctx, slug)

	day, saleID := recognizeOneSale(t, ctx, h, adminID, admin, "2026-07-01", 4200)
	station := "/api/v1/stations/" + h.ids.station1.String()

	// Snapshot the original sale's gross — it must never change.
	var origGross string
	if err := h.pool.QueryRow(ctx, `SELECT gross_amount::text FROM sales WHERE tenant_id=$1 AND id=$2`,
		h.ids.tenantID, saleID).Scan(&origGross); err != nil {
		t.Fatalf("orig sale gross: %v", err)
	}
	if origGross != "12390000.00" {
		t.Fatalf("orig gross = %s, want 12390000.00", origGross)
	}

	// Request the void (reason required).
	code, body := h.invPostJSON(t, "/api/v1/sales/"+saleID+"/void-requests", admin, map[string]any{
		"reason": "metering fault — sale recognized in error",
	})
	if code != http.StatusCreated || body["status"] != "requested" {
		t.Fatalf("request void: status %d: %v", code, body)
	}
	voidID := body["id"].(string)

	// The day rollup still shows the full revenue while the void is only requested.
	code, rd := h.invPostJSON(t, station+"/revenue-days", admin, map[string]any{"operating_day_id": day.String()})
	if code != http.StatusOK || rd["gross_revenue"] != "12390000.00" {
		t.Fatalf("rollup before approve = %v (status %d), want gross 12390000.00", rd["gross_revenue"], code)
	}

	// The sale list surfaces void_status = requested.
	code, list := h.getJSON(t, station+"/sales?operating_day_id="+day.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("list sales: %d", code)
	}
	if vs := list["items"].([]any)[0].(map[string]any)["void_status"]; vs != "requested" {
		t.Fatalf("void_status before approve = %v, want requested", vs)
	}

	// A different user approves → the void becomes the reversal record.
	code, body = h.invPostJSON(t, "/api/v1/sale-voids/"+voidID+"/approve", approver, map[string]any{})
	if code != http.StatusOK || body["status"] != "approved" {
		t.Fatalf("approve void: status %d: %v", code, body)
	}
	// Reversal = sale amounts negated, exact to the cent.
	if body["reversal_gross"] != "-12390000.00" || body["reversal_net"] != "-12390000.00" {
		t.Fatalf("reversal gross/net = %v / %v, want -12390000.00", body["reversal_gross"], body["reversal_net"])
	}
	if body["reversal_cogs"] != "-10080000.00" || body["reversal_margin"] != "-2310000.00" {
		t.Fatalf("reversal cogs/margin = %v / %v", body["reversal_cogs"], body["reversal_margin"])
	}

	// The original sale row is UNCHANGED (append-only).
	var afterGross string
	if err := h.pool.QueryRow(ctx, `SELECT gross_amount::text FROM sales WHERE tenant_id=$1 AND id=$2`,
		h.ids.tenantID, saleID).Scan(&afterGross); err != nil {
		t.Fatalf("sale after void: %v", err)
	}
	if afterGross != origGross {
		t.Fatalf("sale gross mutated by void: %s -> %s", origGross, afterGross)
	}

	// The day rollup now nets to zero — the reversal cancels the sale.
	code, rd = h.invPostJSON(t, station+"/revenue-days", admin, map[string]any{"operating_day_id": day.String()})
	if code != http.StatusOK {
		t.Fatalf("recompute rollup: %d %v", code, rd)
	}
	if rd["gross_revenue"] != "0.00" || rd["margin_total"] != "0.00" {
		t.Fatalf("rollup after approve = gross %v margin %v, want 0.00/0.00", rd["gross_revenue"], rd["margin_total"])
	}

	// The sale list now surfaces void_status = approved.
	code, list = h.getJSON(t, station+"/sales?operating_day_id="+day.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("list sales after approve: %d", code)
	}
	if vs := list["items"].([]any)[0].(map[string]any)["void_status"]; vs != "approved" {
		t.Fatalf("void_status after approve = %v, want approved", vs)
	}

	// Idempotent: a second approve is a 409 and the rollup is not double-reversed.
	if code, _ := h.invPostJSON(t, "/api/v1/sale-voids/"+voidID+"/approve", approver, map[string]any{}); code != http.StatusConflict {
		t.Fatalf("re-approve approved void: status %d, want 409", code)
	}
	// Rejecting an approved void is also a 409 (terminal state).
	if code, _ := h.invPostJSON(t, "/api/v1/sale-voids/"+voidID+"/reject", approver, map[string]any{}); code != http.StatusConflict {
		t.Fatalf("reject approved void: status %d, want 409", code)
	}
	code, rd = h.invPostJSON(t, station+"/revenue-days", admin, map[string]any{"operating_day_id": day.String()})
	if code != http.StatusOK || rd["gross_revenue"] != "0.00" {
		t.Fatalf("rollup after re-approve attempt = %v, want still 0.00", rd["gross_revenue"])
	}

	// A second void request for the same sale is refused while the first is
	// approved (no double-void).
	if code, _ := h.invPostJSON(t, "/api/v1/sales/"+saleID+"/void-requests", admin, map[string]any{"reason": "again"}); code != http.StatusConflict {
		t.Fatalf("double-void request: status %d, want 409", code)
	}
}

// TestSaleVoid_NoSelfApprove proves separation of duties: the requester cannot
// approve or reject their own void (403), and a missing reason is rejected (400).
func TestSaleVoid_NoSelfApprove(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	_, saleID := recognizeOneSale(t, ctx, h, adminID, admin, "2026-07-02", 1000)

	// A blank reason is rejected.
	if code, _ := h.invPostJSON(t, "/api/v1/sales/"+saleID+"/void-requests", admin, map[string]any{"reason": "   "}); code != http.StatusBadRequest {
		t.Fatalf("blank reason: status %d, want 400", code)
	}

	code, body := h.invPostJSON(t, "/api/v1/sales/"+saleID+"/void-requests", admin, map[string]any{"reason": "wrong shift"})
	if code != http.StatusCreated {
		t.Fatalf("request void: %d %v", code, body)
	}
	voidID := body["id"].(string)

	// The requester (admin) cannot approve their own void.
	if code, _ := h.invPostJSON(t, "/api/v1/sale-voids/"+voidID+"/approve", admin, map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("self-approve: status %d, want 403", code)
	}
	// Nor reject it.
	if code, _ := h.invPostJSON(t, "/api/v1/sale-voids/"+voidID+"/reject", admin, map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("self-reject: status %d, want 403", code)
	}
}

// TestSaleVoid_AttendantCannotApprove proves the approve gate: a freshly-created
// attendant (which holds neither sale.void.request nor sale.void.approve) is
// forbidden from both requesting and approving a void.
func TestSaleVoid_AttendantCannotApprove(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, slug, admin := h.adminContext(t, ctx)

	_, saleID := recognizeOneSale(t, ctx, h, adminID, admin, "2026-07-03", 1000)

	// Admin opens a void; a different approver is needed to test approve gating.
	code, body := h.invPostJSON(t, "/api/v1/sales/"+saleID+"/void-requests", admin, map[string]any{"reason": "test"})
	if code != http.StatusCreated {
		t.Fatalf("request void: %d %v", code, body)
	}
	voidID := body["id"].(string)

	// A fresh attendant scoped to station1 — it lacks both void permissions.
	attEmail := "void-att@fuelgrid.local"
	seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, attEmail)
	attendant := h.login(t, slug, attEmail)

	// Cannot request a void (lacks sale.void.request).
	if code, _ := h.invPostJSON(t, "/api/v1/sales/"+saleID+"/void-requests", attendant, map[string]any{"reason": "x"}); code != http.StatusForbidden {
		t.Fatalf("attendant request void: status %d, want 403", code)
	}
	// Cannot approve a void (lacks sale.void.approve).
	if code, _ := h.invPostJSON(t, "/api/v1/sale-voids/"+voidID+"/approve", attendant, map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("attendant approve void: status %d, want 403", code)
	}
}
