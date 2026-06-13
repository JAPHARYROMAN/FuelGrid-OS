package server_test

// DB-backed integration tests for Mobile Attendant App Phase 7 backend:
//
//   - Attendant issue reporting (POST /incidents/report, incidents.report):
//     SELF-SCOPED to the station of the actor's current shift (derived
//     server-side), cross-station 403, no-shift 409, offline-replay dedupe key
//     returning the existing incident, and the supervisor incidents.manage
//     path staying untouched.
//   - The Phase 7 outbox payload extensions the notification subscriber
//     targets attendants from: recorded_by on ReadingVerificationCorrected,
//     submitted_by on CashCollectionConfirmed, attendant_id on
//     ShiftNozzleUnassigned, checked_in_attendant_ids on ShiftApproved.
//   - The two report datasets (GET /reports/attendance and
//     /reports/corrections-variances): station+date-range envelopes with the
//     late/no-show derivation and exact decimal-string money figures.
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the rest of the suite.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// outboxPayload fetches the newest outbox payload of the given event type for
// the tenant, fatal when none exists.
func outboxPayload(t *testing.T, h *harness, ctx context.Context, eventType string) map[string]any {
	t.Helper()
	var raw []byte
	if err := h.pool.QueryRow(ctx, `
		SELECT payload FROM outbox_events
		WHERE tenant_id = $1 AND event_type = $2
		ORDER BY occurred_at DESC LIMIT 1`,
		h.ids.tenantID, eventType).Scan(&raw); err != nil {
		t.Fatalf("outbox %s: %v", eventType, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("outbox %s payload: %v", eventType, err)
	}
	return m
}

// TestMobileAttendant_ReportIncidentSelfScoped: incidents.report holders
// create incidents only at their current shift's station — derived
// server-side; a client-asserted foreign station is 403, no current shift is
// 409 (code no_active_shift), and the supervisor incidents.manage path is
// unchanged (including the attendant still being refused on it).
func TestMobileAttendant_ReportIncidentSelfScoped(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-p7-inc-%d@it.local", time.Now().UnixNano())
	_, _, attID, _ := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)

	// Self-scoped create: no station in the body — the server derives it from
	// the attendant's current shift.
	code, inc := h.postJSON(t, "/api/v1/incidents/report", att,
		`{"type":"pump","description":"Pump 1 display flickers"}`)
	if code != http.StatusCreated {
		t.Fatalf("report incident: %d %v, want 201", code, inc)
	}
	if inc["station_id"] != h.ids.station1.String() {
		t.Fatalf("station_id = %v, want the shift's station %s", inc["station_id"], h.ids.station1)
	}
	if inc["opened_by"] != attID.String() {
		t.Fatalf("opened_by = %v, want the reporter %s", inc["opened_by"], attID)
	}
	if inc["type"] != "pump" || inc["status"] != "open" {
		t.Fatalf("incident = %v, want open pump issue", inc)
	}

	// The same audit/outbox trail as the manage path (IncidentOpened raises
	// the supervisor-facing notification via the subscriber).
	var nOutbox int
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE tenant_id = $1 AND event_type = 'IncidentOpened' AND aggregate_id = $2`,
		h.ids.tenantID, inc["id"]).Scan(&nOutbox); err != nil || nOutbox != 1 {
		t.Fatalf("IncidentOpened outbox rows = %d (err %v), want 1", nOutbox, err)
	}

	// Cross-station: a client-asserted station that is not the current shift's
	// is refused outright.
	if code, b := h.postJSON(t, "/api/v1/incidents/report", att,
		fmt.Sprintf(`{"type":"safety","description":"x","station_id":%q}`, h.ids.station2)); code != http.StatusForbidden {
		t.Fatalf("cross-station report = %d %v, want 403", code, b)
	}

	// Type vocabulary: the self-service path accepts only the PRD issue types.
	if code, b := h.postJSON(t, "/api/v1/incidents/report", att,
		`{"type":"leak","description":"x"}`); code != http.StatusBadRequest {
		t.Fatalf("non-PRD type = %d %v, want 400", code, b)
	}

	// An attendant with no current shift cannot report (409 no_active_shift).
	emailB := fmt.Sprintf("att-p7-idle-%d@it.local", time.Now().UnixNano())
	seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, emailB)
	idle := h.login(t, tenantSlug, emailB)
	code, body := h.postJSON(t, "/api/v1/incidents/report", idle,
		`{"type":"other","description":"x"}`)
	if code != http.StatusConflict || body["code"] != "no_active_shift" {
		t.Fatalf("no-shift report = %d %v, want 409 no_active_shift", code, body)
	}

	// The supervisor flow is unaffected: incidents.manage still creates with an
	// explicit station, and the attendant is still refused on that path.
	if code, b := h.postJSON(t, "/api/v1/incidents", admin,
		fmt.Sprintf(`{"station_id":%q,"description":"managed incident"}`, h.ids.station1)); code != http.StatusCreated {
		t.Fatalf("manage-path create = %d %v, want 201", code, b)
	}
	if code, _ := h.postJSON(t, "/api/v1/incidents", att,
		fmt.Sprintf(`{"station_id":%q,"description":"x"}`, h.ids.station1)); code != http.StatusForbidden {
		t.Fatalf("attendant on manage path = %d, want 403", code)
	}
}

// TestMobileAttendant_ReportIncidentDedupeReplay: a replayed create carrying
// the same dedupe_key returns the EXISTING incident (200, same id) without a
// second row or a second audit/outbox side effect — the offline queue's
// idempotency contract.
func TestMobileAttendant_ReportIncidentDedupeReplay(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-p7-dedupe-%d@it.local", time.Now().UnixNano())
	h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)

	key := uuid.NewString()
	body := fmt.Sprintf(`{"type":"meter","description":"Meter sticks at 9s","dedupe_key":%q}`, key)

	code, first := h.postJSON(t, "/api/v1/incidents/report", att, body)
	if code != http.StatusCreated {
		t.Fatalf("first report: %d %v, want 201", code, first)
	}
	if first["dedupe_key"] != key {
		t.Fatalf("dedupe_key = %v, want %s", first["dedupe_key"], key)
	}

	// Replay: same key, same logical action — the existing incident comes back.
	code, second := h.postJSON(t, "/api/v1/incidents/report", att, body)
	if code != http.StatusOK {
		t.Fatalf("replayed report: %d %v, want 200", code, second)
	}
	if second["id"] != first["id"] {
		t.Fatalf("replay id = %v, want the original %v", second["id"], first["id"])
	}

	var nRows, nOutbox int
	if err := h.pool.QueryRow(ctx,
		`SELECT count(*) FROM incidents WHERE tenant_id = $1 AND dedupe_key = $2`,
		h.ids.tenantID, key).Scan(&nRows); err != nil || nRows != 1 {
		t.Fatalf("incident rows for key = %d (err %v), want 1", nRows, err)
	}
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE tenant_id = $1 AND event_type = 'IncidentOpened' AND aggregate_id = $2`,
		h.ids.tenantID, first["id"]).Scan(&nOutbox); err != nil || nOutbox != 1 {
		t.Fatalf("IncidentOpened outbox rows = %d (err %v), want 1 (no replay side effects)", nOutbox, err)
	}
}

// TestMobileAttendant_Phase7EventPayloadsAndReports drives a full shift
// (check-in -> readings -> unassign/reassign -> close -> verify-correct ->
// cash -> receipt -> approve) and asserts (a) every Phase 7 outbox payload
// extension the notification subscriber targets attendants from, and (b) the
// two report datasets render the flow with exact decimal strings.
func TestMobileAttendant_Phase7EventPayloadsAndReports(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	emailA := fmt.Sprintf("att-p7-flow-%d@it.local", time.Now().UnixNano())
	_, shiftID, attID, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)

	// ShiftNozzleAssigned (from openDayShiftWithAttendant) already carries the
	// attendant in its payload.
	if p := outboxPayload(t, h, ctx, "ShiftNozzleAssigned"); p["attendant_id"] != attID.String() {
		t.Fatalf("ShiftNozzleAssigned payload attendant_id = %v, want %s", p["attendant_id"], attID)
	}

	// Unassign + reassign: the unassignment payload must resolve the attendant
	// (Phase 7 additive payload on a delete-style event).
	code, detail := h.getJSON(t, "/api/v1/shifts/"+shiftID, admin)
	if code != http.StatusOK {
		t.Fatalf("get shift: %d", code)
	}
	assignments := detail["nozzle_assignments"].([]any)
	if len(assignments) != 1 {
		t.Fatalf("assignments = %d, want 1", len(assignments))
	}
	assignmentID := assignments[0].(map[string]any)["id"].(string)
	if code, _ := h.do(t, http.MethodDelete,
		"/api/v1/shifts/"+shiftID+"/nozzle-assignments/"+assignmentID, admin, nil, ""); code != http.StatusNoContent {
		t.Fatalf("unassign: %d", code)
	}
	p := outboxPayload(t, h, ctx, "ShiftNozzleUnassigned")
	if p["attendant_id"] != attID.String() || p["nozzle_id"] != nozzleID.String() {
		t.Fatalf("ShiftNozzleUnassigned payload = %v, want attendant %s nozzle %s", p, attID, nozzleID)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/nozzle-assignments", admin,
		fmt.Sprintf(`{"nozzle_id":%q,"attendant_id":%q}`, nozzleID, attID)); code != http.StatusCreated {
		t.Fatalf("reassign: %d %v", code, b)
	}

	// Check in (drives the attendance dataset AND the ShiftApproved fan-out).
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/check-in", att, `{}`); code != http.StatusCreated {
		t.Fatalf("check-in: %d %v", code, b)
	}

	// Readings, close, supervisor correction (1500 -> 1490, reason mandatory).
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	var closingID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`SELECT id FROM meter_readings
		 WHERE shift_id = $1 AND nozzle_id = $2 AND reading_type = 'closing' AND status = 'active'`,
		shiftID, nozzleID).Scan(&closingID); err != nil {
		t.Fatalf("lookup closing: %v", err)
	}
	if code, b := h.postJSON(t,
		"/api/v1/shifts/"+shiftID+"/readings/"+closingID.String()+"/verify-correct", admin,
		`{"verified_reading":"1490.000","reason":"pump display misread"}`); code != http.StatusCreated {
		t.Fatalf("verify-correct: %d %v", code, b)
	}
	// The corrected event targets the RECORDER via the additive recorded_by.
	p = outboxPayload(t, h, ctx, "ReadingVerificationCorrected")
	if p["recorded_by"] != attID.String() {
		t.Fatalf("ReadingVerificationCorrected payload recorded_by = %v, want %s", p["recorded_by"], attID)
	}

	// Cash: expected after correction = 490 x 2950 = 1,445,500.00; submitted
	// and received 1,440,500.00 — a 5,000.00 shortage.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		`{"cash_amount":"1440500"}`); code != http.StatusCreated {
		t.Fatalf("cash submit: %d %v", code, b)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission/confirm", operator,
		`{"received_total":"1440500","reason":"5,000 short per attendant note"}`); code != http.StatusCreated {
		t.Fatalf("confirm cash: %d %v", code, b)
	}
	// The receipt event targets the SUBMITTER via the additive submitted_by,
	// with the received-vs-expected figures already in the DTO.
	p = outboxPayload(t, h, ctx, "CashCollectionConfirmed")
	adminID := h.userID(t, ctx, h.ids.adminEmail)
	if p["submitted_by"] != adminID.String() {
		t.Fatalf("CashCollectionConfirmed payload submitted_by = %v, want %s", p["submitted_by"], adminID)
	}
	if p["expected_amount"] != "1445500.00" || p["difference"] != "-5000.00" {
		t.Fatalf("CashCollectionConfirmed payload figures = %v", p)
	}

	// The 5,000 shortage exceeds the cash-variance threshold and auto-raised a
	// blocking exception — resolve it (shift.approve) before approval.
	code, excList := h.getJSON(t, "/api/v1/shifts/"+shiftID+"/exceptions", operator)
	if code != http.StatusOK {
		t.Fatalf("list exceptions: %d %v", code, excList)
	}
	for _, raw := range excList["items"].([]any) {
		exc := raw.(map[string]any)
		if exc["status"] != "open" {
			continue
		}
		if code, b := h.patchJSON(t, "/api/v1/shift-exceptions/"+exc["id"].(string)+"/status", operator,
			`{"status":"resolved"}`); code != http.StatusOK {
			t.Fatalf("resolve exception: %d %v", code, b)
		}
	}

	// Approve (operator — SoD: admin closed) and assert the fan-out list.
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", operator,
		`{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve: %d %v", code, b)
	}
	p = outboxPayload(t, h, ctx, "ShiftApproved")
	idsAny, _ := p["checked_in_attendant_ids"].([]any)
	found := false
	for _, v := range idsAny {
		if v == attID.String() {
			found = true
		}
	}
	if !found {
		t.Fatalf("ShiftApproved checked_in_attendant_ids = %v, want to include %s", p["checked_in_attendant_ids"], attID)
	}

	// --- Report datasets ---

	// Attendance: the rostered attendant checked in right after opening, so
	// they derive "present" (late threshold is 15 minutes after opening).
	code, rep := h.getJSON(t, "/api/v1/reports/attendance?station_id="+h.ids.station1.String(), operator)
	if code != http.StatusOK {
		t.Fatalf("attendance report: %d %v", code, rep)
	}
	table := rep["table"].(map[string]any)
	rows := table["rows"].([]any)
	foundPresent := false
	for _, raw := range rows {
		r := raw.([]any)
		// columns: shift, slot, shift_status, attendant, email, attendance_status, check_in_at, check_out_at
		if r[4] == emailA && r[5] == "present" && r[6] != "" {
			foundPresent = true
		}
	}
	if !foundPresent {
		t.Fatalf("attendance rows = %v, want a present row for %s", rows, emailA)
	}

	// Corrections & variances: the corrected reading (both values + reason)
	// and the shortage receipt (expected vs received + difference + reason),
	// all exact decimal strings; shortage total summed in SQL numeric.
	code, rep = h.getJSON(t, "/api/v1/reports/corrections-variances?station_id="+h.ids.station1.String(), operator)
	if code != http.StatusOK {
		t.Fatalf("corrections report: %d %v", code, rep)
	}
	chart := rep["chart_data"].(map[string]any)
	corrections := chart["corrections"].([]any)
	if len(corrections) != 1 {
		t.Fatalf("corrections = %v, want 1 row", corrections)
	}
	c := corrections[0].(map[string]any)
	if c["submitted_reading"] != "1500.000" || c["final_reading"] != "1490.000" ||
		c["delta_litres"] != "-10.000" || c["reason"] != "pump display misread" ||
		c["attendant_id"] != attID.String() {
		t.Fatalf("correction row = %v", c)
	}
	collections := chart["collections"].([]any)
	if len(collections) != 1 {
		t.Fatalf("collections = %v, want 1 row", collections)
	}
	col := collections[0].(map[string]any)
	if col["expected_amount"] != "1445500.00" || col["received_total"] != "1440500.00" ||
		col["difference"] != "-5000.00" || col["status"] != "approved_with_difference" {
		t.Fatalf("collection row = %v", col)
	}
	for _, m := range rep["summary"].([]any) {
		sm := m.(map[string]any)
		if sm["label"] == "Total shortage" && sm["value"] != "5000.00" {
			t.Fatalf("Total shortage = %v, want 5000.00", sm["value"])
		}
	}

	// Station-scope authz like sibling reports: the station-restricted
	// operator cannot read another station's datasets.
	if code, _ := h.getJSON(t, "/api/v1/reports/attendance?station_id="+h.ids.station2.String(), operator); code != http.StatusForbidden {
		t.Fatalf("out-of-scope attendance report = %d, want 403", code)
	}
	if code, _ := h.getJSON(t, "/api/v1/reports/corrections-variances?station_id="+h.ids.station2.String(), operator); code != http.StatusForbidden {
		t.Fatalf("out-of-scope corrections report = %d, want 403", code)
	}
}
