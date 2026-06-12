package server_test

// DB-backed integration tests for Mobile Attendant App Phase 5 — the
// supervisor review surface's READ endpoints: a shift's reading-verification
// set and its collection receipt. Both are station-scoped reads (station.read)
// added so the desktop review page can render pending vs verified state on
// load, not just from the POST responses.
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the rest of the suite.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestMobileAttendant_Phase5ReviewReads drives the two read endpoints through
// a full review cycle: empty/404 before any supervisor action, then the
// corrected verification row (both values + reason) and the receipt with its
// difference after, and cross-tenant invisibility throughout.
func TestMobileAttendant_Phase5ReviewReads(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	emailA := fmt.Sprintf("att-phase5-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)

	verificationsPath := "/api/v1/shifts/" + shiftID + "/reading-verifications"
	receiptPath := "/api/v1/shifts/" + shiftID + "/collection-receipt"

	// Before any supervisor action: the verification set is empty (the
	// closing reading is pending) and there is no receipt yet.
	code, list := h.getJSON(t, verificationsPath, admin)
	if code != http.StatusOK || countOf(list) != 0 {
		t.Fatalf("verifications before review: %d count=%d %v, want 200/0", code, countOf(list), list)
	}
	if code, _ := h.getJSON(t, receiptPath, admin); code != http.StatusNotFound {
		t.Fatalf("receipt before confirmation: %d, want 404", code)
	}

	// Close, then verify-correct the closing (1500 -> 1490, reason required).
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	var closingID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`SELECT id FROM meter_readings
		 WHERE shift_id = $1 AND nozzle_id = $2 AND reading_type = 'closing' AND status = 'active'`,
		shiftID, nozzleID).Scan(&closingID); err != nil {
		t.Fatalf("lookup closing reading: %v", err)
	}
	if code, b := h.postJSON(t,
		"/api/v1/shifts/"+shiftID+"/readings/"+closingID.String()+"/verify-correct", admin,
		`{"verified_reading":"1490.000","reason":"pump display misread"}`); code != http.StatusCreated {
		t.Fatalf("verify-correct: %d %v", code, b)
	}

	// The GET now returns the corrected row with BOTH values and the reason.
	code, list = h.getJSON(t, verificationsPath, admin)
	if code != http.StatusOK || countOf(list) != 1 {
		t.Fatalf("verifications after correction: %d count=%d %v, want 200/1", code, countOf(list), list)
	}
	row := list["items"].([]any)[0].(map[string]any)
	if row["status"] != "corrected" ||
		row["attendant_submitted_reading"] != "1500.000" ||
		row["supervisor_verified_reading"] != "1490.000" ||
		row["final_approved_reading"] != "1490.000" ||
		row["reason"] != "pump display misread" ||
		row["reading_id"] != closingID.String() {
		t.Fatalf("verification row = %v", row)
	}

	// Cash submitted by the admin, received (short) by the operator (SoD).
	// Expected after the correction: 490 litres x 2950 = 1,445,500.00.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		`{"cash_amount":"1440500"}`); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission/confirm", operator,
		`{"received_total":"1440500","reason":"5,000 short per attendant note"}`); code != http.StatusCreated {
		t.Fatalf("confirm cash: %d %v", code, b)
	}

	// The GET returns the receipt with its SQL-numeric difference.
	code, rec := h.getJSON(t, receiptPath, admin)
	if code != http.StatusOK {
		t.Fatalf("receipt after confirmation: %d %v", code, rec)
	}
	if rec["status"] != "approved_with_difference" ||
		rec["expected_amount"] != "1445500.00" ||
		rec["attendant_submitted_total"] != "1440500.00" ||
		rec["supervisor_received_total"] != "1440500.00" ||
		rec["difference"] != "-5000.00" ||
		rec["reason"] != "5,000 short per attendant note" {
		t.Fatalf("receipt = %v", rec)
	}

	// Cross-tenant: a second tenant's admin sees neither.
	ids2 := seedTenant(t, ctx, h.pool)
	defer cleanupTenant(ctx, h.pool, ids2.tenantID)
	var slug2 string
	_ = h.pool.QueryRow(ctx, `SELECT slug FROM tenants WHERE id = $1`, ids2.tenantID).Scan(&slug2)
	admin2 := h.login(t, slug2, ids2.adminEmail)
	if code, _ := h.getJSON(t, verificationsPath, admin2); code != http.StatusNotFound {
		t.Fatalf("cross-tenant verifications: %d, want 404", code)
	}
	if code, _ := h.getJSON(t, receiptPath, admin2); code != http.StatusNotFound {
		t.Fatalf("cross-tenant receipt: %d, want 404", code)
	}
}
