package server_test

// DB-backed integration tests for Mobile Attendant Phase 3: the closing
// submission lock (PRD §7.7 — an attendant's closing reading is immutable
// once submitted; corrections are the supervisor's) and the workflow
// snapshot's verification fields (final_reading + verification_reason, the
// dual-value model surfaced to the attendant review-status screen).
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the rest of the suite.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestMobileAttendant_ClosingSubmissionLock: once an attendant submits a
// closing reading for a nozzle, they can neither re-capture nor correct it —
// both return 409 with the machine-readable code closing_already_submitted.
// The supervisor's reading.override correction path stays open, and the
// attendant's ability to correct their own OPENING while the shift is open is
// unchanged (Phase 2 behavior).
func TestMobileAttendant_ClosingSubmissionLock(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-p3-lock-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	noz := nozzleID.String()

	code, opening := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", att,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"opening","reading":"1000"}`, noz))
	if code != http.StatusCreated {
		t.Fatalf("opening: %d %v", code, opening)
	}
	openingID := mustID(t, opening)
	code, closing := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", att,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"closing","reading":"1500"}`, noz))
	if code != http.StatusCreated {
		t.Fatalf("closing: %d %v", code, closing)
	}
	closingID := mustID(t, closing)

	// A second closing capture for the same nozzle is refused with the
	// machine-readable lock code.
	code, body := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", att,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"closing","reading":"1600"}`, noz))
	if code != http.StatusConflict || body["code"] != "closing_already_submitted" {
		t.Fatalf("re-capture closing = %d %v, want 409 closing_already_submitted", code, body)
	}

	// The attendant cannot correct (supersede) their own submitted closing.
	code, body = h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings/"+closingID+"/correct", att,
		`{"reading":"1550"}`)
	if code != http.StatusConflict || body["code"] != "closing_already_submitted" {
		t.Fatalf("attendant closing correct = %d %v, want 409 closing_already_submitted", code, body)
	}

	// Openings stay attendant-correctable while the shift is open (Phase 2).
	if code, body := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings/"+openingID+"/correct", att,
		`{"reading":"1010"}`); code != http.StatusOK {
		t.Fatalf("attendant opening correct = %d %v, want 200", code, body)
	}

	// The supervisor's reading.override correction path is untouched.
	if code, body := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings/"+closingID+"/correct", admin,
		`{"reading":"1520"}`); code != http.StatusOK {
		t.Fatalf("override closing correct = %d %v, want 200", code, body)
	}
}

// TestMobileAttendant_SnapshotVerificationFields: the attendant workflow
// snapshot carries the dual-value verification outcome per closing reading —
// pending without final_reading first, then (after a supervisor
// verify-correct) status corrected with the final approved figure and the
// supervisor's reason. The verify-correct flow itself must still work with
// the Phase 3 submission lock in place, and the lock must keep holding for
// the attendant after verification.
func TestMobileAttendant_SnapshotVerificationFields(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-p3-snap-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)
	noz := nozzleID.String()

	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", att,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"opening","reading":"1000"}`, noz)); code != http.StatusCreated {
		t.Fatalf("opening: %d %v", code, b)
	}
	code, closing := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", att,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"closing","reading":"1500"}`, noz))
	if code != http.StatusCreated {
		t.Fatalf("closing: %d %v", code, closing)
	}
	closingID := mustID(t, closing)

	// Pending: status present, no final figure or reason yet.
	state := stateOf(t, h, att)
	rd := snapshotReading(t, state)
	if rd["verification_status"] != "pending" || rd["final_reading"] != nil || rd["verification_reason"] != nil {
		t.Fatalf("pre-verification reading = %v, want pending without final/reason", rd)
	}

	// Supervisor verify-corrects to 1490 with a reason (still works under the
	// Phase 3 lock — the lock only binds the attendant's own correct path).
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/readings/"+closingID+"/verify-correct", admin,
		`{"verified_reading":"1490","reason":"Meter glass misread"}`); code != http.StatusCreated {
		t.Fatalf("verify-correct: %d %v", code, b)
	}

	state = stateOf(t, h, att)
	rd = snapshotReading(t, state)
	if rd["verification_status"] != "corrected" {
		t.Fatalf("verification_status = %v, want corrected", rd["verification_status"])
	}
	if rd["closing_reading"] != "1500.000" || rd["final_reading"] != "1490.000" {
		t.Fatalf("dual values = submitted %v / final %v, want 1500.000 / 1490.000",
			rd["closing_reading"], rd["final_reading"])
	}
	if rd["verification_reason"] != "Meter glass misread" {
		t.Fatalf("verification_reason = %v, want the supervisor's reason", rd["verification_reason"])
	}

	// The lock keeps holding after verification: the attendant still cannot
	// supersede the verified closing.
	code, body := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings/"+closingID+"/correct", att,
		`{"reading":"1495"}`)
	if code != http.StatusConflict || body["code"] != "closing_already_submitted" {
		t.Fatalf("post-verification correct = %d %v, want 409 closing_already_submitted", code, body)
	}
}

// snapshotReading pulls the single per-nozzle reading entry out of the
// attendant workflow snapshot.
func snapshotReading(t *testing.T, state map[string]any) map[string]any {
	t.Helper()
	readings, _ := state["readings"].([]any)
	if len(readings) != 1 {
		t.Fatalf("snapshot readings = %v, want exactly 1", state["readings"])
	}
	rd, _ := readings[0].(map[string]any)
	return rd
}
