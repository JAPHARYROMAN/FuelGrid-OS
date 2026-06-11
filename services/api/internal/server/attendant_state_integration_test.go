package server_test

// DB-backed integration tests for the Mobile Attendant App Phase 1 workflow
// snapshot: GET /api/v1/attendant/current-shift. They reuse the Phase 2/3
// harness and drive the next_action state machine with the REAL Phase 0
// endpoints (check-in, assignment confirm, meter capture, close, batch
// verify, cash submission, collection receipt, approval).
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

const attendantStatePath = "/api/v1/attendant/current-shift"

// stateOf fetches the snapshot and fails the test on a non-200.
func stateOf(t *testing.T, h *harness, token string) map[string]any {
	t.Helper()
	code, body := h.getJSON(t, attendantStatePath, token)
	if code != http.StatusOK {
		t.Fatalf("attendant current-shift: %d %v, want 200", code, body)
	}
	return body
}

func wantAction(t *testing.T, state map[string]any, action string) {
	t.Helper()
	if state["next_action"] != action {
		t.Fatalf("next_action = %v (status=%v message=%v blocking=%v), want %s",
			state["next_action"], state["status"], state["user_message"], state["blocking_code"], action)
	}
	if msg, _ := state["user_message"].(string); msg == "" {
		t.Fatalf("user_message missing for next_action %s", action)
	}
}

// TestAttendantState_OffDutyAndRotation: a user with no employee record is
// off duty; a rostered user whose team covers a slot today is expected_today
// with that slot; a user on the resting team is off duty.
func TestAttendantState_OffDutyAndRotation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	suffix := time.Now().UnixNano()

	// No employee record at all -> off duty, empty snapshot.
	emailA := fmt.Sprintf("att-state-offduty-%d@it.local", suffix)
	attAID := seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, emailA)
	attA := h.login(t, tenantSlug, emailA)
	state := stateOf(t, h, attA)
	if state["status"] != "off_duty" {
		t.Fatalf("status = %v, want off_duty", state["status"])
	}
	wantAction(t, state, "off_duty")
	if state["shift"] != nil || state["station"] != nil || state["expected_today"] != nil {
		t.Fatalf("off-duty snapshot leaked shift/station data: %v", state)
	}
	if att, _ := state["attendance"].(map[string]any); att["status"] != "not_checked_in" {
		t.Fatalf("attendance = %v, want not_checked_in", state["attendance"])
	}

	// Rostered on the morning team (cycle day 0) -> expected today, morning.
	seedShiftRotation(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, "morning", &attAID)
	state = stateOf(t, h, attA)
	if state["status"] != "expected_today" {
		t.Fatalf("status = %v, want expected_today", state["status"])
	}
	wantAction(t, state, "await_shift_open")
	expected, _ := state["expected_today"].(map[string]any)
	if expected["slot"] != "morning" {
		t.Fatalf("expected_today = %v, want morning slot", state["expected_today"])
	}
	station, _ := state["station"].(map[string]any)
	if station["id"] != h.ids.station1.String() || station["name"] != "Mikocheni" {
		t.Fatalf("station = %v, want station1/Mikocheni", state["station"])
	}

	// Rostered on the RESTING team (rotation_order 2 on cycle day 0) -> off duty.
	emailB := fmt.Sprintf("att-state-rest-%d@it.local", suffix)
	attBID := seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, emailB)
	var restTeamID, empBID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`SELECT id FROM shift_teams WHERE tenant_id = $1 AND station_id = $2 AND rotation_order = 2`,
		h.ids.tenantID, h.ids.station1).Scan(&restTeamID); err != nil {
		t.Fatalf("lookup rest team: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO employees (tenant_id, station_id, user_id, full_name, role)
		VALUES ($1, $2, $3, 'Resting Member', 'pump_attendant') RETURNING id`,
		h.ids.tenantID, h.ids.station1, attBID).Scan(&empBID); err != nil {
		t.Fatalf("seed resting employee: %v", err)
	}
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO shift_team_members (tenant_id, team_id, employee_id) VALUES ($1, $2, $3)`,
		h.ids.tenantID, restTeamID, empBID); err != nil {
		t.Fatalf("seed resting member: %v", err)
	}
	attB := h.login(t, tenantSlug, emailB)
	state = stateOf(t, h, attB)
	if state["status"] != "off_duty" {
		t.Fatalf("resting-team status = %v, want off_duty", state["status"])
	}
	wantAction(t, state, "off_duty")
}

// TestAttendantState_BlockedAwaitingAssignment: an attendant checked in to an
// open shift with NO nozzle assignment is blocked with the machine-readable
// awaiting_nozzle_assignment code.
func TestAttendantState_BlockedAwaitingAssignment(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-state-blocked-%d@it.local", time.Now().UnixNano())
	_, shiftID, attID, _ := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)

	// Remove the helper's nozzle assignment so the attendant has none.
	var assignmentID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`SELECT id FROM shift_nozzle_assignments WHERE shift_id = $1 AND attendant_id = $2`,
		shiftID, attID).Scan(&assignmentID); err != nil {
		t.Fatalf("lookup assignment: %v", err)
	}
	if code, _ := h.do(t, http.MethodDelete,
		"/api/v1/shifts/"+shiftID+"/nozzle-assignments/"+assignmentID.String(), admin, nil, ""); code != http.StatusNoContent {
		t.Fatalf("unassign nozzle: %d, want 204", code)
	}

	// Before check-in the next action is check_in even without an assignment.
	wantAction(t, stateOf(t, h, att), "check_in")

	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/check-in", att, ``); code != http.StatusCreated {
		t.Fatalf("check-in: %d %v", code, b)
	}
	state := stateOf(t, h, att)
	wantAction(t, state, "blocked")
	if state["blocking_code"] != "awaiting_nozzle_assignment" {
		t.Fatalf("blocking_code = %v, want awaiting_nozzle_assignment", state["blocking_code"])
	}
}

// TestAttendantState_NextActionProgression drives the full state machine with
// the real Phase 0 endpoints across two assigned nozzles:
//
//	check_in → confirm_assignment → verify_opening_readings → working →
//	submit_closing_readings → await_reading_verification →
//	submit_collections → await_collection_receipt → complete
//
// and then re-checks complete after the final shift approval (the
// approved-today fallback) plus expected-openings availability on the next
// shift (the handover chain figure).
func TestAttendantState_NextActionProgression(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-state-flow-%d@it.local", time.Now().UnixNano())
	dayID, shiftID, attID, nozzlePMS := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)

	// A second nozzle (AGO) on the same pump, assigned to the same attendant,
	// so the partial-closing state (submit_closing_readings) is observable.
	var nozzleAGO uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO nozzles (tenant_id, station_id, pump_id, tank_id, product_id, number, default_price)
		VALUES ($1, $2, $3, $4, $5, 2, 2820.00) RETURNING id`,
		h.ids.tenantID, h.ids.station1, h.ids.pump1, h.ids.tankAGO, h.ids.agoProduct).Scan(&nozzleAGO); err != nil {
		t.Fatalf("seed AGO nozzle: %v", err)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/nozzle-assignments", admin,
		fmt.Sprintf(`{"nozzle_id":%q,"attendant_id":%q}`, nozzleAGO, attID)); code != http.StatusCreated {
		t.Fatalf("assign AGO nozzle: %d %v", code, b)
	}
	// Calibration charts so closing dips convert to volume at close time.
	for _, tankID := range []uuid.UUID{h.ids.tankPMS, h.ids.tankAGO} {
		if code, _ := h.uploadCSV(t, "/api/v1/tanks/"+tankID.String()+"/calibration-charts", admin,
			"Initial", "dip_mm,volume_litres\n0,0\n3000,30000\n"); code != http.StatusCreated {
			t.Fatalf("upload chart for %s: %d", tankID, code)
		}
	}

	// 1) Open shift, not checked in -> check_in. First shift ever: no
	// expected openings yet.
	state := stateOf(t, h, att)
	if state["status"] != "on_shift" {
		t.Fatalf("status = %v, want on_shift", state["status"])
	}
	wantAction(t, state, "check_in")
	if state["expected_openings_available"] != false {
		t.Fatalf("expected_openings_available = %v, want false on the first shift", state["expected_openings_available"])
	}
	assignments, _ := state["assignments"].([]any)
	if len(assignments) != 2 {
		t.Fatalf("assignments = %d, want 2", len(assignments))
	}

	// 2) Checked in, assignments unconfirmed -> confirm_assignment.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/check-in", att,
		`{"device_info":{"model":"TestPhone"}}`); code != http.StatusCreated {
		t.Fatalf("check-in: %d %v", code, b)
	}
	state = stateOf(t, h, att)
	wantAction(t, state, "confirm_assignment")
	if att, _ := state["attendance"].(map[string]any); att["status"] != "checked_in" {
		t.Fatalf("attendance = %v, want checked_in", state["attendance"])
	}

	// Confirm both assignments through the real endpoint.
	for _, raw := range state["assignments"].([]any) {
		a := raw.(map[string]any)
		if code, b := h.postJSON(t,
			"/api/v1/shifts/"+shiftID+"/nozzle-assignments/"+a["assignment_id"].(string)+"/confirm",
			att, ``); code != http.StatusOK {
			t.Fatalf("confirm assignment %v: %d %v", a["assignment_id"], code, b)
		}
	}

	// 3) Confirmed, openings missing -> verify_opening_readings.
	wantAction(t, stateOf(t, h, att), "verify_opening_readings")

	// 4) Openings captured on both nozzles -> working.
	for noz, opening := range map[uuid.UUID]string{nozzlePMS: "1000", nozzleAGO: "2000"} {
		if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", att,
			fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"opening","reading":%q}`, noz, opening)); code != http.StatusCreated {
			t.Fatalf("opening for %s: %d %v", noz, code, b)
		}
	}
	wantAction(t, stateOf(t, h, att), "working")

	// 5) One of two closings captured -> submit_closing_readings.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", att,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"closing","reading":"1500"}`, nozzlePMS)); code != http.StatusCreated {
		t.Fatalf("PMS closing: %d %v", code, b)
	}
	wantAction(t, stateOf(t, h, att), "submit_closing_readings")

	// 6) All closings captured (shift still open) -> await_reading_verification,
	// with each closing pending verification.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", att,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"closing","reading":"2300"}`, nozzleAGO)); code != http.StatusCreated {
		t.Fatalf("AGO closing: %d %v", code, b)
	}
	state = stateOf(t, h, att)
	wantAction(t, state, "await_reading_verification")
	for _, raw := range state["readings"].([]any) {
		rd := raw.(map[string]any)
		if rd["verification_status"] != "pending" {
			t.Fatalf("verification_status = %v, want pending: %v", rd["verification_status"], rd)
		}
	}

	// Close needs the closing dips; the snapshot stays await_reading_verification
	// after the close while readings are unverified.
	for _, tankID := range []uuid.UUID{h.ids.tankPMS, h.ids.tankAGO} {
		if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/dip-readings", att,
			fmt.Sprintf(`{"tank_id":%q,"reading_type":"closing","dip_mm":1240}`, tankID)); code != http.StatusCreated {
			t.Fatalf("closing dip for %s: %d %v", tankID, code, b)
		}
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d %v", code, b)
	}
	wantAction(t, stateOf(t, h, att), "await_reading_verification")

	// 7) Supervisor verifies -> submit_collections, with the SQL-numeric
	// expected cash: 500 L * 2950 + 300 L * 2820 = 2,321,000.00.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/readings/verify", admin, ``); code != http.StatusOK {
		t.Fatalf("batch verify: %d %v", code, b)
	}
	state = stateOf(t, h, att)
	wantAction(t, state, "submit_collections")
	if state["expected_cash"] != "2321000.00" {
		t.Fatalf("expected_cash = %v, want 2321000.00", state["expected_cash"])
	}
	for _, raw := range state["readings"].([]any) {
		rd := raw.(map[string]any)
		if rd["verification_status"] != "approved" {
			t.Fatalf("verification_status = %v, want approved: %v", rd["verification_status"], rd)
		}
	}

	// 8) The attendant submits the collections (cash.submit, own shift) ->
	// await_collection_receipt.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", att,
		`{"cash_amount":"2321000"}`); code != http.StatusCreated {
		t.Fatalf("cash submission: %d %v", code, b)
	}
	state = stateOf(t, h, att)
	wantAction(t, state, "await_collection_receipt")
	if state["cash_submission"] == nil {
		t.Fatalf("cash_submission missing from snapshot: %v", state)
	}

	// 9) Supervisor confirms receipt -> complete.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission/confirm", admin,
		`{"received_total":"2321000"}`); code != http.StatusCreated {
		t.Fatalf("confirm cash: %d %v", code, b)
	}
	state = stateOf(t, h, att)
	wantAction(t, state, "complete")
	if state["status"] != "complete" || state["collection_receipt"] == nil {
		t.Fatalf("status = %v receipt = %v, want complete with a receipt", state["status"], state["collection_receipt"])
	}

	// 10) Approval keeps the day on complete (the approved-today fallback).
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve: %d %v", code, b)
	}
	state = stateOf(t, h, att)
	wantAction(t, state, "complete")
	shiftBody, _ := state["shift"].(map[string]any)
	if state["status"] != "complete" || shiftBody["status"] != "approved" {
		t.Fatalf("post-approval snapshot = status %v shift %v, want complete/approved", state["status"], state["shift"])
	}

	// 11) The next shift derives expected openings from the approved closings.
	code, shift2Body := h.postJSON(t, "/api/v1/stations/"+h.ids.station1.String()+"/shifts", admin,
		fmt.Sprintf(`{"operating_day_id":%q,"name":"Evening","slot":"morning"}`, dayID))
	if code != http.StatusCreated {
		t.Fatalf("open shift2: %d %v", code, shift2Body)
	}
	shift2 := mustID(t, shift2Body)
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shift2+"/nozzle-assignments", admin,
		fmt.Sprintf(`{"nozzle_id":%q,"attendant_id":%q}`, nozzlePMS, attID)); code != http.StatusCreated {
		t.Fatalf("assign nozzle on shift2: %d %v", code, b)
	}
	state = stateOf(t, h, att)
	wantAction(t, state, "check_in")
	if state["expected_openings_available"] != true {
		t.Fatalf("expected_openings_available = %v, want true on the handover shift", state["expected_openings_available"])
	}
}

// TestAttendantState_IsolationAndAuth: the endpoint requires a session; a
// non-attendant admin gets a sensible off-duty snapshot; another tenant's
// users never see the first tenant's shift.
func TestAttendantState_IsolationAndAuth(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	// A real shift exists for an attendant in tenant 1.
	emailA := fmt.Sprintf("att-state-iso-%d@it.local", time.Now().UnixNano())
	_, shiftID, _, _ := h.openDayShiftWithAttendant(t, ctx, admin, emailA)

	// Unauthenticated -> 401.
	if code, _ := h.getJSON(t, attendantStatePath, ""); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d, want 401", code)
	}

	// The admin is not a rostered attendant: a sensible off-duty answer, not
	// an error and not another user's shift.
	state := stateOf(t, h, admin)
	if state["status"] != "off_duty" || state["shift"] != nil {
		t.Fatalf("admin snapshot = %v, want off_duty with no shift", state)
	}

	// A second tenant's admin sees only their own (empty) world.
	ids2 := seedTenant(t, ctx, h.pool)
	defer cleanupTenant(ctx, h.pool, ids2.tenantID)
	var slug2 string
	_ = h.pool.QueryRow(ctx, `SELECT slug FROM tenants WHERE id = $1`, ids2.tenantID).Scan(&slug2)
	admin2 := h.login(t, slug2, ids2.adminEmail)
	state = stateOf(t, h, admin2)
	if state["status"] != "off_duty" || state["shift"] != nil {
		t.Fatalf("cross-tenant snapshot = %v, want off_duty with no shift", state)
	}
	if state["station"] != nil {
		t.Fatalf("cross-tenant snapshot leaked a station: %v", state["station"])
	}
	_ = shiftID
}
