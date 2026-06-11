package server_test

// DB-backed integration tests for Mobile Attendant App Phase 0: attendant
// check-in/out, nozzle-assignment confirmation, and the supervisor
// reading-verification dual-value model + the shift-approval gate. They reuse
// the Phase 2/3 harness and helpers (setupHarness, seedAttendant,
// openDayShiftWithAttendant, capturePMSShiftReadings).
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

// TestMobileAttendant_CheckInFlow: check-in happy path, idempotent repeat,
// non-member 403, supervisor attendance list, check-out + idempotent repeat,
// and cross-tenant invisibility.
func TestMobileAttendant_CheckInFlow(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	suffix := time.Now().UnixNano()
	emailA := fmt.Sprintf("att-checkin-%d@it.local", suffix)
	_, shiftID, attID, _ := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)

	// Happy path: first check-in creates the record.
	code, rec := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/check-in", att,
		`{"device_info":{"model":"TestPhone 1","app":"1.0.0"}}`)
	if code != http.StatusCreated {
		t.Fatalf("check-in: %d %v, want 201", code, rec)
	}
	recID := mustID(t, rec)
	if rec["status"] != "checked_in" || rec["attendant_id"] != attID.String() {
		t.Fatalf("check-in record = %v", rec)
	}

	// Idempotent repeat: 200 returning the SAME record, and exactly one audit row.
	code, again := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/check-in", att, ``)
	if code != http.StatusOK || mustID(t, again) != recID {
		t.Fatalf("repeat check-in: %d %v, want 200 with id %s", code, again, recID)
	}
	var audits int
	_ = h.pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE tenant_id = $1 AND action = 'shift.attendant_checked_in'`,
		h.ids.tenantID).Scan(&audits)
	if audits != 1 {
		t.Fatalf("check-in audit rows = %d, want 1 (idempotent repeat must not re-audit)", audits)
	}

	// A non-member of the shift cannot check in.
	emailB := fmt.Sprintf("att-nonmember-%d@it.local", suffix)
	seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, emailB)
	attB := h.login(t, tenantSlug, emailB)
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/check-in", attB, ``); code != http.StatusForbidden {
		t.Fatalf("non-member check-in: %d, want 403", code)
	}

	// Supervisor attendance list (station.read).
	code, list := h.getJSON(t, "/api/v1/shifts/"+shiftID+"/attendance", admin)
	if code != http.StatusOK || countOf(list) != 1 {
		t.Fatalf("attendance list: %d count=%d %v, want 200/1", code, countOf(list), list)
	}

	// Check-out stamps check_out_at; a repeat is idempotent.
	code, out := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/check-out", att, ``)
	if code != http.StatusOK || out["status"] != "checked_out" || out["check_out_at"] == nil {
		t.Fatalf("check-out: %d %v", code, out)
	}
	firstOut, _ := out["check_out_at"].(string)
	code, outAgain := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/check-out", att, ``)
	if code != http.StatusOK || outAgain["check_out_at"] != firstOut {
		t.Fatalf("repeat check-out: %d %v, want 200 with unchanged check_out_at %s", code, outAgain, firstOut)
	}
	// Checking out of a shift you never checked in to is a conflict.
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/check-out", attB, ``); code != http.StatusForbidden {
		t.Fatalf("non-member check-out: %d, want 403", code)
	}

	// Cross-tenant: a second tenant's admin cannot even see the shift.
	ids2 := seedTenant(t, ctx, h.pool)
	defer cleanupTenant(ctx, h.pool, ids2.tenantID)
	var slug2 string
	_ = h.pool.QueryRow(ctx, `SELECT slug FROM tenants WHERE id = $1`, ids2.tenantID).Scan(&slug2)
	admin2 := h.login(t, slug2, ids2.adminEmail)
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/check-in", admin2, ``); code != http.StatusNotFound {
		t.Fatalf("cross-tenant check-in: %d, want 404", code)
	}
	if code, _ := h.getJSON(t, "/api/v1/shifts/"+shiftID+"/attendance", admin2); code != http.StatusNotFound {
		t.Fatalf("cross-tenant attendance list: %d, want 404", code)
	}
}

// TestMobileAttendant_AssignmentConfirm: only the assigned attendant may
// confirm, the confirm is idempotent, and a reassignment (delete + recreate)
// naturally clears the confirmation.
func TestMobileAttendant_AssignmentConfirm(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	suffix := time.Now().UnixNano()
	emailA := fmt.Sprintf("att-confirm-a-%d@it.local", suffix)
	_, shiftID, attAID, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	attA := h.login(t, tenantSlug, emailA)

	var assignmentID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`SELECT id FROM shift_nozzle_assignments WHERE shift_id = $1 AND attendant_id = $2`,
		shiftID, attAID).Scan(&assignmentID); err != nil {
		t.Fatalf("lookup assignment: %v", err)
	}
	confirmPath := "/api/v1/shifts/" + shiftID + "/nozzle-assignments/" + assignmentID.String() + "/confirm"

	// A different attendant ON the shift still cannot confirm A's assignment.
	emailB := fmt.Sprintf("att-confirm-b-%d@it.local", suffix)
	attBID := seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, emailB)
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/attendants", admin,
		fmt.Sprintf(`{"user_id":%q}`, attBID)); code != http.StatusCreated {
		t.Fatalf("assign B to shift: %d %v", code, b)
	}
	attB := h.login(t, tenantSlug, emailB)
	if code, _ := h.postJSON(t, confirmPath, attB, ``); code != http.StatusForbidden {
		t.Fatalf("other attendant confirm: %d, want 403", code)
	}

	// The assignee confirms; the repeat is idempotent with the same timestamp.
	code, conf := h.postJSON(t, confirmPath, attA, ``)
	if code != http.StatusOK || conf["confirmed_at"] == nil {
		t.Fatalf("confirm: %d %v", code, conf)
	}
	first, _ := conf["confirmed_at"].(string)
	code, confAgain := h.postJSON(t, confirmPath, attA, ``)
	if code != http.StatusOK || confAgain["confirmed_at"] != first {
		t.Fatalf("repeat confirm: %d %v, want 200 with unchanged confirmed_at %s", code, confAgain, first)
	}

	// Reassignment (delete + recreate) clears the confirmation naturally.
	if code, _ := h.do(t, http.MethodDelete,
		"/api/v1/shifts/"+shiftID+"/nozzle-assignments/"+assignmentID.String(), admin, nil, ""); code != http.StatusNoContent {
		t.Fatalf("unassign nozzle: %d, want 204", code)
	}
	code, recreated := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/nozzle-assignments", admin,
		fmt.Sprintf(`{"nozzle_id":%q,"attendant_id":%q}`, nozzleID, attAID))
	if code != http.StatusCreated {
		t.Fatalf("reassign nozzle: %d %v", code, recreated)
	}
	var confirmedAt *time.Time
	if err := h.pool.QueryRow(ctx,
		`SELECT confirmed_at FROM shift_nozzle_assignments WHERE id = $1`,
		mustID(t, recreated)).Scan(&confirmedAt); err != nil {
		t.Fatalf("lookup recreated assignment: %v", err)
	}
	if confirmedAt != nil {
		t.Fatalf("recreated assignment confirmed_at = %v, want NULL", confirmedAt)
	}
}

// TestMobileAttendant_VerificationAndApprovalGate drives the dual-value model
// end to end: approval is blocked (409 readings_unverified) until the
// supervisor batch-verifies the closing readings, after which it succeeds.
func TestMobileAttendant_VerificationAndApprovalGate(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-gate-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)

	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		`{"cash_amount":"1475000"}`); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}

	// Approval is blocked with the machine-readable gate code.
	code, blocked := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`)
	if code != http.StatusConflict || blocked["code"] != "readings_unverified" {
		t.Fatalf("approve before verification: %d %v, want 409 readings_unverified", code, blocked)
	}

	// Batch verify approves every closing reading as-is (final = submitted).
	code, verified := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/readings/verify", admin, ``)
	if code != http.StatusOK || countOf(verified) != 1 {
		t.Fatalf("batch verify: %d %v, want 200 with 1 verification", code, verified)
	}
	item := verified["items"].([]any)[0].(map[string]any)
	if item["status"] != "approved" ||
		item["attendant_submitted_reading"] != "1500.000" ||
		item["final_approved_reading"] != "1500.000" ||
		item["supervisor_verified_reading"] != nil {
		t.Fatalf("batch verification row = %v", item)
	}
	// The batch is idempotent: re-running verifies nothing new.
	code, rerun := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/readings/verify", admin, ``)
	if code != http.StatusOK || rerun["newly_verified"].(float64) != 0 {
		t.Fatalf("batch verify rerun: %d %v, want 200 with newly_verified 0", code, rerun)
	}

	// The gate now passes.
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve after verification: %d %v", code, b)
	}
}

// TestMobileAttendant_VerifyCorrect: a supervisor correction stores BOTH
// values, never mutates the meter reading, demands a reason, and recomputes
// the frozen close line (litres + expected value) when the shift is closed.
func TestMobileAttendant_VerifyCorrect(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-correct-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)

	// Close first so the correction exercises the close-line recompute.
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
	correctPath := "/api/v1/shifts/" + shiftID + "/readings/" + closingID.String() + "/verify-correct"

	// Reason is mandatory.
	if code, _ := h.postJSON(t, correctPath, admin, `{"verified_reading":"1490.000"}`); code != http.StatusBadRequest {
		t.Fatalf("verify-correct without reason: %d, want 400", code)
	}
	// A correction below the opening reading is rejected (would drive litres negative).
	if code, _ := h.postJSON(t, correctPath, admin,
		`{"verified_reading":"900","reason":"typo"}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("verify-correct below opening: %d, want 422", code)
	}

	// The correction stores both values.
	code, v := h.postJSON(t, correctPath, admin,
		`{"verified_reading":"1490.000","reason":"pump display misread by attendant"}`)
	if code != http.StatusCreated {
		t.Fatalf("verify-correct: %d %v", code, v)
	}
	if v["status"] != "corrected" ||
		v["attendant_submitted_reading"] != "1500.000" ||
		v["supervisor_verified_reading"] != "1490.000" ||
		v["final_approved_reading"] != "1490.000" ||
		v["reason"] != "pump display misread by attendant" {
		t.Fatalf("verification row = %v", v)
	}

	// The original meter reading is untouched (dual-value model).
	var rawReading, rawStatus string
	if err := h.pool.QueryRow(ctx,
		`SELECT reading::text, status FROM meter_readings WHERE id = $1`, closingID).
		Scan(&rawReading, &rawStatus); err != nil {
		t.Fatalf("re-read meter reading: %v", err)
	}
	if rawReading != "1500.000" || rawStatus != "active" {
		t.Fatalf("meter reading mutated: reading=%s status=%s, want 1500.000/active", rawReading, rawStatus)
	}

	// The frozen close line was recomputed from the final approved figure:
	// litres 490, expected 490 * 2950 = 1,445,500.00.
	code, summary := h.getJSON(t, "/api/v1/shifts/"+shiftID+"/close-summary", admin)
	if code != http.StatusOK {
		t.Fatalf("close summary: %d", code)
	}
	line := summary["lines"].([]any)[0].(map[string]any)
	if line["closing_reading"] != "1490.000" || line["litres_sold"] != "490.000" ||
		line["expected_value"] != "1445500.00" {
		t.Fatalf("recomputed close line = %v", line)
	}
	if summary["expected_cash"] != "1445500.00" {
		t.Fatalf("expected_cash = %v, want 1445500.00", summary["expected_cash"])
	}

	// A second verification of the same reading is refused.
	if code, _ := h.postJSON(t, correctPath, admin,
		`{"verified_reading":"1480","reason":"again"}`); code != http.StatusConflict {
		t.Fatalf("double verification: %d, want 409", code)
	}

	// The corrected verification satisfies the approval gate: cash to match,
	// then approve.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		`{"cash_amount":"1445500"}`); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve: %d %v", code, b)
	}
}

// TestMobileAttendant_SoDSelfVerify: whoever recorded a closing reading cannot
// verify it — batch or single — while a different supervisor can.
func TestMobileAttendant_SoDSelfVerify(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	emailA := fmt.Sprintf("att-sod-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)

	// ADMIN records the closing reading via override.
	h.capturePMSShiftReadings(t, admin, admin, shiftID, nozzleID)

	// The recorder cannot batch-verify their own reading...
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/readings/verify", admin, ``); code != http.StatusForbidden {
		t.Fatalf("self batch verify: %d, want 403", code)
	}
	// ...nor verify-correct it.
	var closingID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`SELECT id FROM meter_readings
		 WHERE shift_id = $1 AND nozzle_id = $2 AND reading_type = 'closing' AND status = 'active'`,
		shiftID, nozzleID).Scan(&closingID); err != nil {
		t.Fatalf("lookup closing reading: %v", err)
	}
	if code, _ := h.postJSON(t,
		"/api/v1/shifts/"+shiftID+"/readings/"+closingID.String()+"/verify-correct", admin,
		`{"verified_reading":"1490","reason":"self"}`); code != http.StatusForbidden {
		t.Fatalf("self verify-correct: %d, want 403", code)
	}

	// A different supervisor (station_manager holds reading.override) can.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/readings/verify", operator, ``); code != http.StatusOK {
		t.Fatalf("other-supervisor batch verify: %d %v, want 200", code, b)
	}
}
