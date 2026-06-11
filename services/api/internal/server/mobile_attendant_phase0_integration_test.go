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

	// Readings are verified, but the cash handover is still unconfirmed — the
	// second gate of the handover chain blocks with its own code.
	code, blocked = h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`)
	if code != http.StatusConflict || blocked["code"] != "collection_unconfirmed" {
		t.Fatalf("approve before receipt: %d %v, want 409 collection_unconfirmed", code, blocked)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission/confirm", admin,
		`{"received_total":"1475000"}`); code != http.StatusCreated {
		t.Fatalf("confirm cash: %d %v", code, b)
	}

	// Both gates now pass.
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
	// receipt to confirm it, then approve.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		`{"cash_amount":"1445500"}`); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission/confirm", admin,
		`{"received_total":"1445500"}`); code != http.StatusCreated {
		t.Fatalf("confirm cash: %d %v", code, b)
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

// TestMobileAttendant_CollectionReceipt: a supervisor confirms the cash
// handover; the receipt snapshots expected + submitted, computes the
// difference in SQL numeric, demands a reason whenever the difference is
// non-zero, refuses the submitter (SoD), and is one-per-submission.
func TestMobileAttendant_CollectionReceipt(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	emailA := fmt.Sprintf("att-receipt-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)

	confirmPath := "/api/v1/shifts/" + shiftID + "/cash-submission/confirm"

	// No receipt before the shift closes / before cash is submitted.
	if code, _ := h.postJSON(t, confirmPath, operator, `{"received_total":"1475000"}`); code != http.StatusConflict {
		t.Fatalf("confirm before close: %d, want 409", code)
	}
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	if code, _ := h.postJSON(t, confirmPath, operator, `{"received_total":"1475000"}`); code != http.StatusConflict {
		t.Fatalf("confirm before cash submission: %d, want 409", code)
	}

	// The OPERATOR submits the cash (cash.override), so the operator is the
	// submitter for the SoD check below.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", operator,
		`{"cash_amount":"1475000"}`); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}

	// SoD: the submitter cannot confirm receiving their own submission.
	if code, _ := h.postJSON(t, confirmPath, operator, `{"received_total":"1475000"}`); code != http.StatusForbidden {
		t.Fatalf("submitter self-confirm: %d, want 403", code)
	}
	// The attendant lacks cash.confirm entirely.
	if code, _ := h.postJSON(t, confirmPath, att, `{"received_total":"1475000"}`); code != http.StatusForbidden {
		t.Fatalf("attendant confirm: %d, want 403", code)
	}

	// A received total that differs from expected demands a reason.
	if code, _ := h.postJSON(t, confirmPath, admin, `{"received_total":"1470000"}`); code != http.StatusBadRequest {
		t.Fatalf("difference without reason: %d, want 400", code)
	}

	// Confirmed with a reason: both snapshots stored, difference = received −
	// expected computed in SQL numeric, status upgraded server-side.
	code, rec := h.postJSON(t, confirmPath, admin,
		`{"received_total":"1470000","reason":"5,000 short — attendant to repay","supervisor_comment":"counted twice"}`)
	if code != http.StatusCreated {
		t.Fatalf("confirm: %d %v", code, rec)
	}
	if rec["status"] != "approved_with_difference" ||
		rec["expected_amount"] != "1475000.00" ||
		rec["attendant_submitted_total"] != "1475000.00" ||
		rec["supervisor_received_total"] != "1470000.00" ||
		rec["difference"] != "-5000.00" ||
		rec["reason"] != "5,000 short — attendant to repay" {
		t.Fatalf("receipt = %v", rec)
	}

	// One receipt per cash submission.
	if code, _ := h.postJSON(t, confirmPath, admin,
		`{"received_total":"1475000"}`); code != http.StatusConflict {
		t.Fatalf("double confirm: %d, want 409", code)
	}

	// Exactly one audit row for the confirmation.
	var audits int
	_ = h.pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE tenant_id = $1 AND action = 'cash.collection_confirmed'`,
		h.ids.tenantID).Scan(&audits)
	if audits != 1 {
		t.Fatalf("confirm audit rows = %d, want 1", audits)
	}
}

// TestMobileAttendant_HandoverChain: a new shift cannot open while a prior
// shift at the station is closed-but-not-approved (machine-readable 409); a
// shift.approve holder may override with a mandatory, audited reason; once
// the prior shift is approved, opening is allowed again. The overridden-open
// shift's expected openings fall back to the prior shift's RAW closing while
// it is still unverified.
func TestMobileAttendant_HandoverChain(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-handover-%d@it.local", time.Now().UnixNano())
	dayID, shift1, attID, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shift1, nozzleID)
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shift1+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close shift1: %d", code)
	}

	st := h.ids.station1.String()
	openBody := fmt.Sprintf(`{"operating_day_id":%q,"name":"Evening","slot":"morning"}`, dayID)

	// shift1 is closed-but-not-approved: opening the next shift is blocked
	// with the machine-readable handover code.
	code, blocked := h.postJSON(t, "/api/v1/stations/"+st+"/shifts", admin, openBody)
	if code != http.StatusConflict || blocked["code"] != "prior_shift_unapproved" {
		t.Fatalf("open during handover: %d %v, want 409 prior_shift_unapproved", code, blocked)
	}
	ids, _ := blocked["unapproved_shift_ids"].([]any)
	if len(ids) != 1 || ids[0] != shift1 {
		t.Fatalf("unapproved_shift_ids = %v, want [%s]", ids, shift1)
	}

	// An attendant holds shift.open but NOT shift.approve: their override
	// attempt is refused.
	attOverrideBody := fmt.Sprintf(
		`{"operating_day_id":%q,"name":"Evening","slot":"morning","handover_override_reason":"urgent"}`, dayID)
	if code, _ := h.postJSON(t, "/api/v1/stations/"+st+"/shifts", att, attOverrideBody); code != http.StatusForbidden {
		t.Fatalf("override without shift.approve: %d, want 403", code)
	}

	// A shift.approve holder overrides with a reason; the override is audited.
	code, shift2Body := h.postJSON(t, "/api/v1/stations/"+st+"/shifts", admin,
		fmt.Sprintf(`{"operating_day_id":%q,"name":"Evening","slot":"morning","handover_override_reason":"outgoing supervisor unreachable"}`, dayID))
	if code != http.StatusCreated {
		t.Fatalf("override open: %d %v", code, shift2Body)
	}
	shift2 := mustID(t, shift2Body)
	var overrideAudits int
	_ = h.pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE tenant_id = $1 AND action = 'shift.handover_overridden' AND entity_id = $2`,
		h.ids.tenantID, shift2).Scan(&overrideAudits)
	if overrideAudits != 1 {
		t.Fatalf("override audit rows = %d, want 1", overrideAudits)
	}

	// shift1's closing is still unverified, so shift2's expected opening for
	// the nozzle falls back to the RAW closing (1500.000).
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shift2+"/nozzle-assignments", admin,
		fmt.Sprintf(`{"nozzle_id":%q,"attendant_id":%q}`, nozzleID, attID)); code != http.StatusCreated {
		t.Fatalf("assign nozzle on shift2: %d %v", code, b)
	}
	code, exp := h.getJSON(t, "/api/v1/shifts/"+shift2+"/expected-opening-readings", att)
	if code != http.StatusOK || countOf(exp) != 1 {
		t.Fatalf("expected openings: %d %v", code, exp)
	}
	item := exp["items"].([]any)[0].(map[string]any)
	if item["expected_opening_reading"] != "1500.000" || item["source"] != "raw" || item["source_shift_id"] != shift1 {
		t.Fatalf("expected opening (raw fallback) = %v", item)
	}

	// Approve shift1 (verify its readings first; no cash was submitted, so
	// the receipt gate does not apply).
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shift1+"/readings/verify", admin, ``); code != http.StatusOK {
		t.Fatalf("verify shift1: %d %v", code, b)
	}
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shift1+"/status", admin, `{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve shift1: %d %v", code, b)
	}

	// With shift1 approved (and shift2 merely open, not closed), a plain open
	// goes through again.
	if code, b := h.postJSON(t, "/api/v1/stations/"+st+"/shifts", admin,
		fmt.Sprintf(`{"operating_day_id":%q,"name":"Night","slot":"morning"}`, dayID)); code != http.StatusCreated {
		t.Fatalf("open after approve: %d %v", code, b)
	}
}

// TestMobileAttendant_ExpectedOpening: the expected opening derives from the
// previous shift's FINAL APPROVED closing (the corrected verification figure,
// not the raw submission), and opening capture rejects a reading below it
// with a machine-readable 422.
func TestMobileAttendant_ExpectedOpening(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-expected-%d@it.local", time.Now().UnixNano())
	dayID, shift1, attID, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)

	// The very first shift has no prior closing: the expected opening is empty.
	code, exp := h.getJSON(t, "/api/v1/shifts/"+shift1+"/expected-opening-readings", att)
	if code != http.StatusOK || countOf(exp) != 1 {
		t.Fatalf("expected openings (first shift): %d %v", code, exp)
	}
	if v := exp["items"].([]any)[0].(map[string]any)["expected_opening_reading"]; v != nil {
		t.Fatalf("first-shift expected opening = %v, want empty", v)
	}

	// Run shift1: raw closing 1500, corrected by the supervisor to 1490.
	h.capturePMSShiftReadings(t, admin, att, shift1, nozzleID)
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shift1+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close shift1: %d", code)
	}
	var closingID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`SELECT id FROM meter_readings
		 WHERE shift_id = $1 AND nozzle_id = $2 AND reading_type = 'closing' AND status = 'active'`,
		shift1, nozzleID).Scan(&closingID); err != nil {
		t.Fatalf("lookup closing reading: %v", err)
	}
	if code, b := h.postJSON(t,
		"/api/v1/shifts/"+shift1+"/readings/"+closingID.String()+"/verify-correct", admin,
		`{"verified_reading":"1490.000","reason":"meter parallax"}`); code != http.StatusCreated {
		t.Fatalf("verify-correct: %d %v", code, b)
	}
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shift1+"/status", admin, `{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve shift1: %d %v", code, b)
	}

	// Open shift2 and assign the same nozzle to the same attendant.
	code, shift2Body := h.postJSON(t, "/api/v1/stations/"+h.ids.station1.String()+"/shifts", admin,
		fmt.Sprintf(`{"operating_day_id":%q,"name":"Evening","slot":"morning"}`, dayID))
	if code != http.StatusCreated {
		t.Fatalf("open shift2: %d %v", code, shift2Body)
	}
	shift2 := mustID(t, shift2Body)
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shift2+"/nozzle-assignments", admin,
		fmt.Sprintf(`{"nozzle_id":%q,"attendant_id":%q}`, nozzleID, attID)); code != http.StatusCreated {
		t.Fatalf("assign nozzle on shift2: %d %v", code, b)
	}

	// The expected opening is the CORRECTED final (1490.000), not the raw 1500.
	code, exp = h.getJSON(t, "/api/v1/shifts/"+shift2+"/expected-opening-readings", att)
	if code != http.StatusOK || countOf(exp) != 1 {
		t.Fatalf("expected openings (shift2): %d %v", code, exp)
	}
	item := exp["items"].([]any)[0].(map[string]any)
	if item["expected_opening_reading"] != "1490.000" || item["source"] != "verified" ||
		item["source_shift_id"] != shift1 || item["nozzle_id"] != nozzleID.String() {
		t.Fatalf("expected opening (corrected case) = %v", item)
	}

	// Capturing an opening BELOW the expected figure is rejected with the
	// machine-readable code and the expected value.
	code, rejected := h.postJSON(t, "/api/v1/shifts/"+shift2+"/meter-readings", att,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"opening","reading":"1480"}`, nozzleID))
	if code != http.StatusUnprocessableEntity || rejected["code"] != "opening_below_expected" ||
		rejected["expected_opening_reading"] != "1490.000" {
		t.Fatalf("opening below expected: %d %v, want 422 opening_below_expected", code, rejected)
	}
	// At (or above) the expected figure it goes through.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shift2+"/meter-readings", att,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"opening","reading":"1490"}`, nozzleID)); code != http.StatusCreated {
		t.Fatalf("opening at expected: %d %v", code, b)
	}
}
