package server_test

// DB-backed integration tests for the Mobile Attendant App PRD closeout
// (§7.8, §9.5, §9.6): supervisor reading REJECTION (a hold that blocks shift
// approval and unlocks the attendant's Phase 3 closing-submission lock so they
// can re-capture, after which re-verification proceeds), reading FLAGGING (a
// hold that blocks approval), and collection FLAGGING (a hold that blocks
// approval). They reuse the Phase 0/2/3 harness + helpers.
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

// closingIDForShift looks up the shift's single ACTIVE closing reading id.
func closingIDForShift(t *testing.T, ctx context.Context, h *harness, shiftID string, nozzleID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`SELECT id FROM meter_readings
		 WHERE shift_id = $1 AND nozzle_id = $2 AND reading_type = 'closing' AND status = 'active'`,
		shiftID, nozzleID).Scan(&id); err != nil {
		t.Fatalf("lookup active closing reading: %v", err)
	}
	return id
}

// TestMobileAttendant_RejectReading drives the rejection flow end to end:
// reason is mandatory, SoD blocks self-rejection, a rejection blocks approval
// with readings_rejected_pending, the rejection unlocks the attendant's
// closing-submission lock so they re-capture, and after re-verification the
// shift can be approved.
func TestMobileAttendant_RejectReading(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	emailA := fmt.Sprintf("att-reject-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)

	closingID := closingIDForShift(t, ctx, h, shiftID, nozzleID)
	rejectPath := "/api/v1/shifts/" + shiftID + "/readings/" + closingID.String() + "/reject"

	// Reason is mandatory.
	if code, _ := h.postJSON(t, rejectPath, operator, `{}`); code != http.StatusBadRequest {
		t.Fatalf("reject without reason: %d, want 400", code)
	}

	// Close + submit cash so the only thing standing in approval's way is the
	// verdict on the closing reading.
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		`{"cash_amount":"1475000"}`); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}

	// Reject the attendant's closing (operator holds reading.override and did
	// not record it): a hold with the mandatory reason, final = the unchanged
	// submission.
	code, rej := h.postJSON(t, rejectPath, operator,
		`{"reason":"meter photo is unreadable — re-capture the closing"}`)
	if code != http.StatusCreated {
		t.Fatalf("reject: %d %v", code, rej)
	}
	if rej["status"] != "rejected" ||
		rej["attendant_submitted_reading"] != "1500.000" ||
		rej["final_approved_reading"] != "1500.000" ||
		rej["reason"] != "meter photo is unreadable — re-capture the closing" {
		t.Fatalf("rejection row = %v", rej)
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

	// A rejection is a non-terminal HOLD, so re-deciding it overwrites the hold
	// in place (still exactly one verification row) rather than 409ing — the
	// supervisor can revise the reason or, later, clear it with a terminal
	// verdict. The terminal-immutability rule is proven separately by the
	// flag/collection clear tests' "re-approve terminal -> 409" assertions.
	if code, b := h.postJSON(t, rejectPath, operator, `{"reason":"again"}`); code != http.StatusCreated {
		t.Fatalf("re-reject hold: %d %v, want 201 (hold overwritten in place)", code, b)
	}
	var verifCount int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM reading_verifications WHERE reading_id = $1`, closingID).Scan(&verifCount)
	if verifCount != 1 {
		t.Fatalf("verifications on reading = %d, want 1 (single row, overwritten)", verifCount)
	}

	// Approval is blocked with the rejection-specific machine-readable code.
	// (The reject -> resubmit -> reverify -> approve loop is exercised on an
	// OPEN shift in TestMobileAttendant_RejectUnlocksResubmit.)
	code, blocked := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`)
	if code != http.StatusConflict || blocked["code"] != "readings_rejected_pending" {
		t.Fatalf("approve with rejection pending: %d %v, want 409 readings_rejected_pending", code, blocked)
	}
}

// TestMobileAttendant_RejectUnlocksResubmit proves the reject -> resubmit ->
// reverify -> approve loop on an OPEN shift: the attendant's closing is locked
// after submission, a supervisor rejection unlocks re-capture, the re-captured
// closing supersedes the rejected one and is unverified again, and once it is
// approved (terminal-good) the shift can be approved.
func TestMobileAttendant_RejectUnlocksResubmit(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	emailA := fmt.Sprintf("att-resubmit-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)

	closingID := closingIDForShift(t, ctx, h, shiftID, nozzleID)

	// While submitted-but-unrejected, the attendant's closing is locked: a
	// re-capture (correct) is refused with closing_already_submitted.
	correctMeterPath := "/api/v1/shifts/" + shiftID + "/meter-readings/" + closingID.String() + "/correct"
	code, locked := h.postJSON(t, correctMeterPath, att, `{"reading":"1490"}`)
	if code != http.StatusConflict || locked["code"] != "closing_already_submitted" {
		t.Fatalf("attendant correct before rejection: %d %v, want 409 closing_already_submitted", code, locked)
	}

	// Supervisor rejects the closing.
	rejectPath := "/api/v1/shifts/" + shiftID + "/readings/" + closingID.String() + "/reject"
	if code, b := h.postJSON(t, rejectPath, operator,
		`{"reason":"closing looks low — please re-read the meter"}`); code != http.StatusCreated {
		t.Fatalf("reject: %d %v", code, b)
	}

	// The lock is now relaxed for this nozzle: the attendant re-captures the
	// closing. The correct path supersedes the rejected reading and inserts a
	// new ACTIVE one.
	code, fresh := h.postJSON(t, correctMeterPath, att, `{"reading":"1495"}`)
	if code != http.StatusOK {
		t.Fatalf("attendant re-capture after rejection: %d %v, want 200", code, fresh)
	}
	if fresh["reading"] != "1495.000" || fresh["status"] != "active" {
		t.Fatalf("re-captured reading = %v, want 1495.000/active", fresh)
	}
	newClosingID := closingIDForShift(t, ctx, h, shiftID, nozzleID)
	if newClosingID == closingID {
		t.Fatalf("re-capture did not supersede: still %s", closingID)
	}

	// The rejection stays on the now-superseded original (history preserved).
	var supersededStatus string
	if err := h.pool.QueryRow(ctx, `SELECT status FROM meter_readings WHERE id = $1`, closingID).
		Scan(&supersededStatus); err != nil {
		t.Fatalf("re-read original: %v", err)
	}
	if supersededStatus != "superseded" {
		t.Fatalf("original status = %s, want superseded", supersededStatus)
	}
	var rejectionsOnOriginal int
	_ = h.pool.QueryRow(ctx,
		`SELECT count(*) FROM reading_verifications WHERE reading_id = $1 AND status = 'rejected'`,
		closingID).Scan(&rejectionsOnOriginal)
	if rejectionsOnOriginal != 1 {
		t.Fatalf("rejection on original = %d, want 1 (history preserved)", rejectionsOnOriginal)
	}

	// Close + cash so only verification stands in the way.
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		fmt.Sprintf(`{"cash_amount":"%s"}`, expectedCashFor(t, h, admin, shiftID))); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}

	// The new ACTIVE closing is unverified — approval reports readings_unverified,
	// NOT readings_rejected_pending (the rejection moved to the superseded row).
	code, blocked := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`)
	if code != http.StatusConflict || blocked["code"] != "readings_unverified" {
		t.Fatalf("approve after re-capture: %d %v, want 409 readings_unverified", code, blocked)
	}

	// Re-verify the re-captured closing (batch approve), then approval passes.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/readings/verify", operator, ``); code != http.StatusOK {
		t.Fatalf("re-verify: %d %v", code, b)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission/confirm", operator,
		fmt.Sprintf(`{"received_total":"%s"}`, expectedCashFor(t, h, admin, shiftID))); code != http.StatusCreated {
		t.Fatalf("confirm cash: %d %v", code, b)
	}
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve after reverify: %d %v, want 200", code, b)
	}

	// Exactly one rejected notification was produced for the recorder (the
	// attendant). The outbox->subscriber runs in the API process; assert the
	// audit/outbox carried the event so the frontend wiring is provable here.
	var rejectEvents int
	_ = h.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox_events WHERE tenant_id = $1 AND event_type = 'ReadingVerificationRejected'`,
		h.ids.tenantID).Scan(&rejectEvents)
	if rejectEvents != 1 {
		t.Fatalf("ReadingVerificationRejected outbox events = %d, want 1", rejectEvents)
	}
}

// TestMobileAttendant_FlagReadingBlocksApproval: a flagged closing reading is a
// hold that blocks approval with readings_flagged_pending; SoD and reason rules
// match the reject path; the meter reading is never mutated.
func TestMobileAttendant_FlagReadingBlocksApproval(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	emailA := fmt.Sprintf("att-flag-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)

	closingID := closingIDForShift(t, ctx, h, shiftID, nozzleID)
	flagPath := "/api/v1/shifts/" + shiftID + "/readings/" + closingID.String() + "/flag"

	// Reason mandatory.
	if code, _ := h.postJSON(t, flagPath, operator, `{}`); code != http.StatusBadRequest {
		t.Fatalf("flag without reason: %d, want 400", code)
	}

	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		`{"cash_amount":"1475000"}`); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}

	code, flag := h.postJSON(t, flagPath, operator, `{"reason":"reading inconsistent with dip — investigating"}`)
	if code != http.StatusCreated {
		t.Fatalf("flag: %d %v", code, flag)
	}
	if flag["status"] != "flagged" || flag["reason"] != "reading inconsistent with dip — investigating" {
		t.Fatalf("flag row = %v", flag)
	}

	// The meter reading is untouched.
	var rawReading string
	_ = h.pool.QueryRow(ctx, `SELECT reading::text FROM meter_readings WHERE id = $1`, closingID).Scan(&rawReading)
	if rawReading != "1500.000" {
		t.Fatalf("meter reading mutated: %s, want 1500.000", rawReading)
	}

	// Approval blocked with the flag-specific code.
	code, blocked := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`)
	if code != http.StatusConflict || blocked["code"] != "readings_flagged_pending" {
		t.Fatalf("approve with flag pending: %d %v, want 409 readings_flagged_pending", code, blocked)
	}

	// Exactly one flag event was produced (frontend notification wiring).
	var flagEvents int
	_ = h.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox_events WHERE tenant_id = $1 AND event_type = 'ReadingVerificationFlagged'`,
		h.ids.tenantID).Scan(&flagEvents)
	if flagEvents != 1 {
		t.Fatalf("ReadingVerificationFlagged outbox events = %d, want 1", flagEvents)
	}
}

// TestMobileAttendant_FlagReadingSoD: a supervisor who RECORDED a closing
// reading cannot reject or flag it (separation of duties).
func TestMobileAttendant_RejectFlagSoD(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-sod-verdict-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)

	// ADMIN records the closing via override, so admin is the recorder.
	h.capturePMSShiftReadings(t, admin, admin, shiftID, nozzleID)
	closingID := closingIDForShift(t, ctx, h, shiftID, nozzleID)

	for _, verb := range []string{"reject", "flag"} {
		path := "/api/v1/shifts/" + shiftID + "/readings/" + closingID.String() + "/" + verb
		if code, _ := h.postJSON(t, path, admin, `{"reason":"self"}`); code != http.StatusForbidden {
			t.Fatalf("self %s: %d, want 403", verb, code)
		}
	}
}

// TestMobileAttendant_CollectionFlagBlocksApproval: a flagged collection
// receipt is a hold that blocks shift approval (collection_unconfirmed), the
// existing received/approved_with_difference/rejected behaviour stays intact,
// and a flag requires a reason.
func TestMobileAttendant_CollectionFlagBlocksApproval(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	emailA := fmt.Sprintf("att-colflag-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)

	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	// Verify readings so the only blocker is the collection verdict.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/readings/verify", operator, ``); code != http.StatusOK {
		t.Fatalf("verify readings: %d %v", code, b)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		`{"cash_amount":"1475000"}`); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}

	confirmPath := "/api/v1/shifts/" + shiftID + "/cash-submission/confirm"

	// Flag requires a reason.
	if code, _ := h.postJSON(t, confirmPath, operator,
		`{"received_total":"1475000","status":"flagged"}`); code != http.StatusBadRequest {
		t.Fatalf("flag collection without reason: %d, want 400", code)
	}

	// Flag the handover (operator did not submit — admin did): status flagged
	// regardless of the matching figure.
	code, rec := h.postJSON(t, confirmPath, operator,
		`{"received_total":"1475000","status":"flagged","reason":"cash count disputed — escalating"}`)
	if code != http.StatusCreated {
		t.Fatalf("flag collection: %d %v", code, rec)
	}
	if rec["status"] != "flagged" || rec["reason"] != "cash count disputed — escalating" {
		t.Fatalf("flagged receipt = %v", rec)
	}

	// A flagged receipt leaves the gate closed.
	code, blocked := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`)
	if code != http.StatusConflict || blocked["code"] != "collection_unconfirmed" {
		t.Fatalf("approve with flagged collection: %d %v, want 409 collection_unconfirmed", code, blocked)
	}

	// A flag is a HOLD, not a final receipt: re-confirming after the dispute is
	// settled overwrites it in place (the clear path is exercised end to end in
	// TestMobileAttendant_CollectionFlagClearedByReconfirm).
	if code, b := h.postJSON(t, confirmPath, operator, `{"received_total":"1475000"}`); code != http.StatusCreated {
		t.Fatalf("re-confirm after flag: %d %v, want 201 (hold is re-confirmable)", code, b)
	}
}

// TestMobileAttendant_FlagClearedByApprove proves a FLAGGED reading is
// resolvable: the supervisor flags it, then — investigation done, figure fine —
// approves it as-submitted via the per-reading approve path, which overwrites
// the flag with a terminal 'approved'. The shift then approves cleanly. This is
// the hold -> clear -> approve cycle the reviewers flagged as untested.
func TestMobileAttendant_FlagClearedByApprove(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	emailA := fmt.Sprintf("att-flagclear-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)

	closingID := closingIDForShift(t, ctx, h, shiftID, nozzleID)
	flagPath := "/api/v1/shifts/" + shiftID + "/readings/" + closingID.String() + "/flag"
	approvePath := "/api/v1/shifts/" + shiftID + "/readings/" + closingID.String() + "/approve"

	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		fmt.Sprintf(`{"cash_amount":"%s"}`, expectedCashFor(t, h, admin, shiftID))); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission/confirm", operator,
		fmt.Sprintf(`{"received_total":"%s"}`, expectedCashFor(t, h, admin, shiftID))); code != http.StatusCreated {
		t.Fatalf("confirm cash: %d %v", code, b)
	}

	// Flag the reading: approval is blocked.
	if code, b := h.postJSON(t, flagPath, operator, `{"reason":"checking the dip"}`); code != http.StatusCreated {
		t.Fatalf("flag: %d %v", code, b)
	}
	code, blocked := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`)
	if code != http.StatusConflict || blocked["code"] != "readings_flagged_pending" {
		t.Fatalf("approve with flag: %d %v, want 409 readings_flagged_pending", code, blocked)
	}

	// Clear the flag by approving the reading as-submitted: the flag row is
	// overwritten with a terminal 'approved'.
	code, appr := h.postJSON(t, approvePath, operator, ``)
	if code != http.StatusCreated || appr["status"] != "approved" || appr["final_approved_reading"] != "1500.000" {
		t.Fatalf("approve reading to clear flag: %d %v", code, appr)
	}
	var verifCount int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM reading_verifications WHERE reading_id = $1`, closingID).Scan(&verifCount)
	if verifCount != 1 {
		t.Fatalf("verifications on reading = %d, want 1 (hold overwritten in place)", verifCount)
	}

	// Re-approving an already-terminal reading is refused.
	if code, _ := h.postJSON(t, approvePath, operator, ``); code != http.StatusConflict {
		t.Fatalf("re-approve terminal: %d, want 409", code)
	}

	// The shift now approves.
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve after clearing flag: %d %v, want 200", code, b)
	}
}

// TestMobileAttendant_PostCloseRejectClearedByCorrect proves a rejection issued
// on a CLOSED shift (the realistic post-close review path) is recoverable: the
// attendant can no longer re-capture (the shift is closed), so the SUPERVISOR
// clears the hold by correcting the reading, after which the shift approves.
func TestMobileAttendant_PostCloseRejectClearedByCorrect(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	emailA := fmt.Sprintf("att-postclose-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)

	closingID := closingIDForShift(t, ctx, h, shiftID, nozzleID)
	rejectPath := "/api/v1/shifts/" + shiftID + "/readings/" + closingID.String() + "/reject"
	correctPath := "/api/v1/shifts/" + shiftID + "/readings/" + closingID.String() + "/verify-correct"

	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		fmt.Sprintf(`{"cash_amount":"%s"}`, expectedCashFor(t, h, admin, shiftID))); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}

	// Reject after close.
	if code, b := h.postJSON(t, rejectPath, operator, `{"reason":"meter photo unreadable"}`); code != http.StatusCreated {
		t.Fatalf("reject post-close: %d %v", code, b)
	}
	// The attendant cannot re-capture on a closed shift.
	closingCorrectPath := "/api/v1/shifts/" + shiftID + "/meter-readings/" + closingID.String() + "/correct"
	if code, _ := h.postJSON(t, closingCorrectPath, att, `{"reading":"1495"}`); code != http.StatusConflict {
		t.Fatalf("attendant re-capture on closed shift: %d, want 409", code)
	}

	// The supervisor clears the rejection by correcting the reading (works
	// post-close — it recomputes the close line). The hold is overwritten.
	code, corr := h.postJSON(t, correctPath, operator, `{"verified_reading":"1500","reason":"confirmed against dip"}`)
	if code != http.StatusCreated || corr["status"] != "corrected" {
		t.Fatalf("supervisor correct to clear rejection: %d %v", code, corr)
	}
	var verifCount int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM reading_verifications WHERE reading_id = $1`, closingID).Scan(&verifCount)
	if verifCount != 1 {
		t.Fatalf("verifications on reading = %d, want 1 (rejection overwritten)", verifCount)
	}

	// Cash + approval now succeed.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission/confirm", operator,
		fmt.Sprintf(`{"received_total":"%s"}`, expectedCashFor(t, h, admin, shiftID))); code != http.StatusCreated {
		t.Fatalf("confirm cash: %d %v", code, b)
	}
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve after supervisor cleared rejection: %d %v, want 200", code, b)
	}
}

// TestMobileAttendant_CollectionFlagClearedByReconfirm proves a FLAGGED
// collection receipt is resolvable: the supervisor re-confirms after settling
// the dispute, overwriting the held receipt in place, and the shift approves. A
// terminal-good receipt, by contrast, stays immutable.
func TestMobileAttendant_CollectionFlagClearedByReconfirm(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	emailA := fmt.Sprintf("att-colclear-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)

	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/readings/verify", operator, ``); code != http.StatusOK {
		t.Fatalf("verify readings: %d %v", code, b)
	}
	expected := expectedCashFor(t, h, admin, shiftID)
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		fmt.Sprintf(`{"cash_amount":"%s"}`, expected)); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}
	confirmPath := "/api/v1/shifts/" + shiftID + "/cash-submission/confirm"

	// Flag the handover, then approval is blocked.
	if code, b := h.postJSON(t, confirmPath, operator,
		fmt.Sprintf(`{"received_total":"%s","status":"flagged","reason":"count disputed"}`, expected)); code != http.StatusCreated {
		t.Fatalf("flag collection: %d %v", code, b)
	}
	code, blocked := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`)
	if code != http.StatusConflict || blocked["code"] != "collection_unconfirmed" {
		t.Fatalf("approve with flagged collection: %d %v, want 409 collection_unconfirmed", code, blocked)
	}

	// Re-confirm after settling: the held receipt is overwritten in place
	// (still exactly one receipt for the submission).
	code, rec := h.postJSON(t, confirmPath, operator, fmt.Sprintf(`{"received_total":"%s"}`, expected))
	if code != http.StatusCreated || rec["status"] != "received" {
		t.Fatalf("re-confirm flagged collection: %d %v, want 201 received", code, rec)
	}
	var receiptCount int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM collection_receipts WHERE shift_id = $1`, shiftID).Scan(&receiptCount)
	if receiptCount != 1 {
		t.Fatalf("receipts for shift = %d, want 1 (held receipt overwritten in place)", receiptCount)
	}

	// A terminal-good receipt is now immutable: a further confirm is refused.
	if code, _ := h.postJSON(t, confirmPath, operator, fmt.Sprintf(`{"received_total":"%s"}`, expected)); code != http.StatusConflict {
		t.Fatalf("re-confirm terminal receipt: %d, want 409", code)
	}

	// The shift now approves.
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve after clearing collection flag: %d %v, want 200", code, b)
	}
}

// TestMobileAttendant_VerdictRejectedOnApprovedShift proves reject/flag/approve
// are refused once the shift is approved (its facts are frozen), matching the
// verify-correct guard.
func TestMobileAttendant_VerdictRejectedOnApprovedShift(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	emailA := fmt.Sprintf("att-approvedguard-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)
	closingID := closingIDForShift(t, ctx, h, shiftID, nozzleID)

	// Drive the shift all the way to approved.
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/readings/verify", operator, ``); code != http.StatusOK {
		t.Fatalf("verify: %d %v", code, b)
	}
	expected := expectedCashFor(t, h, admin, shiftID)
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		fmt.Sprintf(`{"cash_amount":"%s"}`, expected)); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission/confirm", operator,
		fmt.Sprintf(`{"received_total":"%s"}`, expected)); code != http.StatusCreated {
		t.Fatalf("confirm: %d %v", code, b)
	}
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve: %d %v", code, b)
	}

	// Every verdict is now refused with 409 (shift approved).
	for _, verb := range []string{"reject", "flag", "approve"} {
		path := "/api/v1/shifts/" + shiftID + "/readings/" + closingID.String() + "/" + verb
		body := `{"reason":"too late"}`
		if verb == "approve" {
			body = ``
		}
		if code, _ := h.postJSON(t, path, operator, body); code != http.StatusConflict {
			t.Fatalf("%s on approved shift: %d, want 409", verb, code)
		}
	}
}

// expectedCashFor reads the shift's expected cash from the close-summary API so
// the resubmit test can submit a matching figure regardless of the re-captured
// closing value.
func expectedCashFor(t *testing.T, h *harness, admin, shiftID string) string {
	t.Helper()
	code, summary := h.getJSON(t, "/api/v1/shifts/"+shiftID+"/close-summary", admin)
	if code != http.StatusOK {
		t.Fatalf("close summary: %d", code)
	}
	expected, _ := summary["expected_cash"].(string)
	if expected == "" {
		t.Fatalf("close summary missing expected_cash: %v", summary)
	}
	return expected
}
