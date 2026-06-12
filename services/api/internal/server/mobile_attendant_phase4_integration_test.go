package server_test

// DB-backed integration tests for Mobile Attendant Phase 4: the attendant
// collections flow. Covers the variance-reason guard on the attendant
// cash-submission path (422 variance_reason_required, PRD §12.4 — a non-zero
// difference must carry a reason in notes; the supervisor override path is
// exempt) and the workflow snapshot's close_lines calculation basis
// (litres_sold × unit_price = expected_value per nozzle, PRD §7.9/§12.3).
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the rest of the suite.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestMobileAttendant_VarianceReasonRequired: an attendant whose submitted
// total differs from the expected collection must explain the difference —
// without a reason the submission is refused with the machine-readable 422
// and NOTHING is persisted, so the corrected resubmission still works
// (one-per-shift stays intact). A balanced total needs no reason.
func TestMobileAttendant_VarianceReasonRequired(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-p4-var-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)

	// 500 L at 2950.00 -> expected 1,475,000.00, then close.
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d %v", code, b)
	}

	cashPath := "/api/v1/shifts/" + shiftID + "/cash-submission"

	// A short total without a reason is refused with the machine-readable code.
	code, body := h.postJSON(t, cashPath, att, `{"cash_amount":"1470000"}`)
	if code != http.StatusUnprocessableEntity || body["code"] != "variance_reason_required" {
		t.Fatalf("short without reason = %d %v, want 422 variance_reason_required", code, body)
	}
	// Whitespace-only notes are not a reason.
	code, body = h.postJSON(t, cashPath, att, `{"cash_amount":"1470000","notes":"   "}`)
	if code != http.StatusUnprocessableEntity || body["code"] != "variance_reason_required" {
		t.Fatalf("short with blank reason = %d %v, want 422 variance_reason_required", code, body)
	}
	// The refused attempts persisted nothing.
	var count int
	if err := h.pool.QueryRow(ctx,
		`SELECT count(*) FROM cash_submissions WHERE shift_id = $1`, shiftID).Scan(&count); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if count != 0 {
		t.Fatalf("cash_submissions after 422 = %d, want 0 (rolled back)", count)
	}

	// With a reason the same short submission lands, reason in notes, variance
	// computed in SQL numeric (1,470,000 − 1,475,000 = −5,000.00).
	code, sub := h.postJSON(t, cashPath, att,
		`{"cash_amount":"1400000","mobile_money_amount":"70000","notes":"5,000 short - drive-off, incident reported"}`)
	if code != http.StatusCreated {
		t.Fatalf("short with reason: %d %v", code, sub)
	}
	if sub["variance"] != "-5000.00" || sub["submitted_total"] != "1470000.00" ||
		sub["notes"] != "5,000 short - drive-off, incident reported" {
		t.Fatalf("submission = %v, want variance -5000.00 / total 1470000.00 with notes", sub)
	}

	// One-per-shift still holds after the earlier rollbacks.
	if code, _ := h.postJSON(t, cashPath, att, `{"cash_amount":"1475000"}`); code != http.StatusConflict {
		t.Fatalf("resubmission: %d, want 409", code)
	}
}

// TestMobileAttendant_BalancedNeedsNoReason: an attendant whose tender total
// matches the expected collection exactly submits without any notes.
func TestMobileAttendant_BalancedNeedsNoReason(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-p4-bal-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d %v", code, b)
	}

	// Balanced across two tenders, no notes -> 201 with zero variance.
	code, sub := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", att,
		`{"cash_amount":"1400000","mobile_money_amount":"75000"}`)
	if code != http.StatusCreated {
		t.Fatalf("balanced without reason: %d %v, want 201", code, sub)
	}
	if sub["variance"] != "0.00" || sub["submitted_total"] != "1475000.00" {
		t.Fatalf("submission = %v, want variance 0.00 / total 1475000.00", sub)
	}
}

// TestMobileAttendant_VarianceReasonOverrideExempt: the supervisor override
// path (cash.override) is exempt from the attendant variance-reason guard —
// its reason policy lives on the collection receipt (cash.confirm).
func TestMobileAttendant_VarianceReasonOverrideExempt(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-p4-ovr-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d %v", code, b)
	}

	// The admin (cash.override) submits a short total without notes: 201.
	code, sub := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		`{"cash_amount":"1470000"}`)
	if code != http.StatusCreated {
		t.Fatalf("override short without reason: %d %v, want 201", code, sub)
	}
	if sub["variance"] != "-5000.00" {
		t.Fatalf("override variance = %v, want -5000.00", sub["variance"])
	}
}

// TestMobileAttendant_SnapshotCloseLines: once the shift closes, the workflow
// snapshot carries the expected collection's per-nozzle calculation basis —
// the frozen close line with its pump/nozzle/product labels and the exact
// decimal litres × unit price = expected value. Absent while the shift is
// still open (the honest "available after the shift closes" state).
func TestMobileAttendant_SnapshotCloseLines(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-p4-lines-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)

	// Open shift: no expected cash, no close lines yet.
	state := stateOf(t, h, att)
	if state["expected_cash"] != nil || state["close_lines"] != nil {
		t.Fatalf("open-shift snapshot has expected_cash=%v close_lines=%v, want both absent",
			state["expected_cash"], state["close_lines"])
	}

	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d %v", code, b)
	}

	state = stateOf(t, h, att)
	if state["expected_cash"] != "1475000.00" {
		t.Fatalf("expected_cash = %v, want 1475000.00", state["expected_cash"])
	}
	lines, _ := state["close_lines"].([]any)
	if len(lines) != 1 {
		t.Fatalf("close_lines = %v, want exactly 1", state["close_lines"])
	}
	line := lines[0].(map[string]any)
	want := map[string]any{
		"nozzle_id":       nozzleID.String(),
		"pump_number":     float64(1),
		"nozzle_number":   float64(1),
		"product_name":    "Premium",
		"product_color":   "#f97316",
		"opening_reading": "1000.000",
		"closing_reading": "1500.000",
		"litres_sold":     "500.000",
		"unit_price":      "2950.00",
		"expected_value":  "1475000.00",
	}
	for k, v := range want {
		if line[k] != v {
			t.Fatalf("close_lines[0].%s = %v, want %v (line %v)", k, line[k], v, line)
		}
	}
}
