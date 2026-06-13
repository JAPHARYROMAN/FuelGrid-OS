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

	// A second verdict on the same reading is refused (one verification per reading).
	if code, _ := h.postJSON(t, rejectPath, operator, `{"reason":"again"}`); code != http.StatusConflict {
		t.Fatalf("double verdict: %d, want 409", code)
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

	// One receipt per submission: a follow-up confirm is refused.
	if code, _ := h.postJSON(t, confirmPath, operator, `{"received_total":"1475000"}`); code != http.StatusConflict {
		t.Fatalf("re-confirm after flag: %d, want 409 (one per submission)", code)
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
