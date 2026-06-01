package server_test

// DB-backed integration tests for the Phase 3 operating layer. They reuse the
// Phase 2 harness (setupHarness / seedTenant) and drive the real HTTP API
// through the full day workflow plus the failure cases the Phase 3 audit
// called out: attendant self-scope, post-close correction lock, zero-
// assignment close, cross-station writes, and the unassign cascade.
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the rest of the suite.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// --- Phase 3 helpers ---

func (h *harness) postJSON(t *testing.T, path, token, body string) (int, map[string]any) {
	t.Helper()
	code, raw := h.do(t, http.MethodPost, path, token, bytes.NewReader([]byte(body)), "application/json")
	var m map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	return code, m
}

func (h *harness) patchJSON(t *testing.T, path, token, body string) (int, map[string]any) {
	t.Helper()
	code, raw := h.do(t, http.MethodPatch, path, token, bytes.NewReader([]byte(body)), "application/json")
	var m map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	return code, m
}

func mustID(t *testing.T, m map[string]any) string {
	t.Helper()
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatalf("expected id in response, got %v", m)
	}
	return id
}

// seedAttendant creates an active user with the system attendant role and
// access to one station, so it holds reading.edit/cash.submit but not the
// supervisor override permissions.
func seedAttendant(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, stationID uuid.UUID, email string) uuid.UUID {
	t.Helper()
	hasher := password.New(password.DefaultParams, "")
	hash, err := hasher.Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	var id uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'IT Attendant', 'active', $3, now()) RETURNING id`,
		tenantID, email, hash).Scan(&id); err != nil {
		t.Fatalf("seed attendant: %v", err)
	}
	grantRole(t, ctx, pool, tenantID, id, "attendant")
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_station_access (user_id, station_id, tenant_id) VALUES ($1, $2, $3)`,
		id, stationID, tenantID); err != nil {
		t.Fatalf("attendant station access: %v", err)
	}
	return id
}

// seedShiftRotation makes station1 openable for the given slot: it ensures the
// station has its three rotation teams + a today anchor, and links memberUserID
// (when non-nil) to an employee on the team that works `slot` on cycle day 0.
// Opening that slot's shift then resolves a non-empty team and auto-populates
// the linked attendant — the Phase 11 enforcement ("no shift without its
// expected employees"). Idempotent within a test's fresh tenant.
//
// On cycle day 0: morning = order 0, evening = order 1.
func seedShiftRotation(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, stationID uuid.UUID, slot string, memberUserID *uuid.UUID) {
	t.Helper()
	order := 0
	if slot == "evening" {
		order = 1
	}
	teamIDs := make([]uuid.UUID, 3)
	for o := 0; o < 3; o++ {
		// Idempotent: re-running yields the existing team id.
		if err := pool.QueryRow(ctx, `
			INSERT INTO shift_teams (tenant_id, station_id, name, rotation_order)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (tenant_id, station_id, rotation_order) DO UPDATE SET name = EXCLUDED.name
			RETURNING id`,
			tenantID, stationID, []string{"Team A", "Team B", "Team C"}[o], o).Scan(&teamIDs[o]); err != nil {
			t.Fatalf("seed team %d: %v", o, err)
		}
	}
	if memberUserID != nil {
		var empID uuid.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO employees (tenant_id, station_id, user_id, full_name, role)
			VALUES ($1, $2, $3, 'Shift Member', 'pump_attendant')
			ON CONFLICT (tenant_id, user_id) WHERE user_id IS NOT NULL DO UPDATE SET full_name = EXCLUDED.full_name
			RETURNING id`,
			tenantID, stationID, *memberUserID).Scan(&empID); err != nil {
			t.Fatalf("seed member employee: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO shift_team_members (tenant_id, team_id, employee_id) VALUES ($1, $2, $3)
			ON CONFLICT (team_id, employee_id) DO NOTHING`,
			tenantID, teamIDs[order], empID); err != nil {
			t.Fatalf("seed member: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `
		UPDATE stations SET rotation_anchor_date = CURRENT_DATE WHERE tenant_id = $1 AND id = $2`,
		tenantID, stationID); err != nil {
		t.Fatalf("seed anchor: %v", err)
	}
}

// userID resolves a seeded user's id by email.
func (h *harness) userID(t *testing.T, ctx context.Context, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM users WHERE tenant_id = $1 AND email = $2`,
		h.ids.tenantID, email).Scan(&id); err != nil {
		t.Fatalf("lookup user %s: %v", email, err)
	}
	return id
}

func nozzleFor(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, tankID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM nozzles WHERE tenant_id = $1 AND tank_id = $2 LIMIT 1`,
		tenantID, tankID).Scan(&id); err != nil {
		t.Fatalf("lookup nozzle: %v", err)
	}
	return id
}

// openDayShiftWithAttendant opens a day + shift (as admin), creates an
// attendant, assigns them to the shift and the PMS nozzle, and returns the
// day id, shift id, attendant id, and nozzle id.
func (h *harness) openDayShiftWithAttendant(t *testing.T, ctx context.Context, admin, attEmail string) (dayID, shiftID string, attID, nozzleID uuid.UUID) {
	t.Helper()
	st := h.ids.station1.String()
	code, day := h.postJSON(t, "/api/v1/stations/"+st+"/operating-days", admin, `{}`)
	if code != http.StatusCreated {
		t.Fatalf("open day: %d %v", code, day)
	}
	dayID = mustID(t, day)

	// The attendant must exist before the shift opens: rotation links them to the
	// morning team so opening auto-populates them as the shift's attendant.
	attID = seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, attEmail)
	nozzleID = nozzleFor(t, ctx, h.pool, h.ids.tenantID, h.ids.tankPMS)
	seedShiftRotation(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, "morning", &attID)

	code, shift := h.postJSON(t, "/api/v1/stations/"+st+"/shifts", admin,
		fmt.Sprintf(`{"operating_day_id":%q,"name":"Morning","slot":"morning"}`, dayID))
	if code != http.StatusCreated {
		t.Fatalf("open shift: %d %v", code, shift)
	}
	shiftID = mustID(t, shift)

	// The attendant is auto-assigned by the rotation; only the nozzle is manual.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/nozzle-assignments", admin,
		fmt.Sprintf(`{"nozzle_id":%q,"attendant_id":%q}`, nozzleID, attID)); code != http.StatusCreated {
		t.Fatalf("assign nozzle: %d %v", code, b)
	}
	return dayID, shiftID, attID, nozzleID
}

// --- Tests ---

// TestPhase3_DayWorkflow drives the whole chain end to end with the assigned
// attendant capturing readings and a supervisor closing, approving, and
// locking the day.
func TestPhase3_DayWorkflow(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	dayID, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, fmt.Sprintf("att-flow-%d@it.local", time.Now().UnixNano()))
	att := h.login(t, tenantSlug, attEmailFromDB(t, ctx, h.pool, h.ids.tenantID))

	// Active chart so the closing dip resolves.
	if code, _ := h.uploadCSV(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/calibration-charts", admin,
		"Initial", "dip_mm,volume_litres\n0,0\n3000,30000\n"); code != http.StatusCreated {
		t.Fatalf("upload chart: %d", code)
	}

	// Attendant captures opening + closing meter (500 L) and a closing dip.
	noz := nozzleID.String()
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", att,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"opening","reading":1000}`, noz)); code != http.StatusCreated {
		t.Fatalf("opening meter: %d %v", code, b)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", att,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"closing","reading":1500}`, noz)); code != http.StatusCreated {
		t.Fatalf("closing meter: %d %v", code, b)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/dip-readings", att,
		fmt.Sprintf(`{"tank_id":%q,"reading_type":"closing","dip_mm":1240}`, h.ids.tankPMS.String())); code != http.StatusCreated {
		t.Fatalf("closing dip: %d %v", code, b)
	}

	// Supervisor closes: expected cash = 500 * 2950 = 1,475,000.
	code, closeBody := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``)
	if code != http.StatusOK {
		t.Fatalf("close: %d %v", code, closeBody)
	}
	// expected_cash is the exact SQL-numeric decimal string (numeric(14,2)):
	// 500.000 L * 2950.00 = 1,475,000.00.
	if exp := closeBody["expected_cash"].(string); exp != "1475000.00" {
		t.Fatalf("expected_cash = %v, want 1475000.00", exp)
	}

	// Cash submitted to match -> zero variance -> no blocking exception.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		`{"cash_amount":"1475000"}`); code != http.StatusCreated {
		t.Fatalf("cash: %d %v", code, b)
	}
	// Separation of duties (OPS-002): the closer (admin, who closed above) may
	// not approve their own shift; a distinct approver can. Then close + lock.
	if code, _ := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`); code != http.StatusForbidden {
		t.Fatalf("self-approve should be 403, got %d", code)
	}
	approver := h.secondApprover(t, ctx, tenantSlug)
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", approver, `{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve: %d %v", code, b)
	}
	if code, b := h.patchJSON(t, "/api/v1/operating-days/"+dayID+"/status", admin, `{"status":"closed"}`); code != http.StatusOK {
		t.Fatalf("close day: %d %v", code, b)
	}
	if code, b := h.patchJSON(t, "/api/v1/operating-days/"+dayID+"/lock", admin, `{}`); code != http.StatusOK {
		t.Fatalf("lock day: %d %v", code, b)
	}
}

// TestPhase3_AttendantSelfScope is the P1 release-blocker check: an attendant
// may only write against shifts and nozzles assigned to them; a supervisor
// with the override permission may write across the station.
func TestPhase3_AttendantSelfScope(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	suffix := time.Now().UnixNano()
	emailA := fmt.Sprintf("att-a-%d@it.local", suffix)
	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	attA := h.login(t, tenantSlug, emailA)

	// A second attendant, not assigned to the shift.
	emailB := fmt.Sprintf("att-b-%d@it.local", suffix)
	attBID := seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, emailB)
	attB := h.login(t, tenantSlug, emailB)

	noz := nozzleID.String()
	openBody := fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"opening","reading":1000}`, noz)

	// B is not on the shift at all -> 403.
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", attB, openBody); code != http.StatusForbidden {
		t.Fatalf("non-member write: %d, want 403", code)
	}
	// Put B on the shift but assign no nozzle: writing A's nozzle -> 403.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/attendants", admin,
		fmt.Sprintf(`{"user_id":%q}`, attBID)); code != http.StatusCreated {
		t.Fatalf("assign B: %d %v", code, b)
	}
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", attB, openBody); code != http.StatusForbidden {
		t.Fatalf("unassigned-nozzle write: %d, want 403", code)
	}
	// A owns the nozzle -> 201.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", attA, openBody); code != http.StatusCreated {
		t.Fatalf("assigned write: %d %v, want 201", code, b)
	}
	// Supervisor override may write the same nozzle (closing) across the station.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", admin,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"closing","reading":1500}`, noz)); code != http.StatusCreated {
		t.Fatalf("override write: %d %v, want 201", code, b)
	}
}

// TestPhase3_PostCloseCorrectionLocked is the second P1 check: once a shift is
// closed, readings can no longer be corrected (the close snapshot is frozen).
func TestPhase3_PostCloseCorrectionLocked(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	_, shiftID, _, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, fmt.Sprintf("att-close-%d@it.local", time.Now().UnixNano()))
	noz := nozzleID.String()

	if code, _ := h.uploadCSV(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/calibration-charts", admin,
		"Initial", "dip_mm,volume_litres\n0,0\n3000,30000\n"); code != http.StatusCreated {
		t.Fatalf("upload chart: %d", code)
	}

	code, opening := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", admin,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"opening","reading":1000}`, noz))
	if code != http.StatusCreated {
		t.Fatalf("opening: %d %v", code, opening)
	}
	openingID := mustID(t, opening)
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", admin,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"closing","reading":1500}`, noz)); code != http.StatusCreated {
		t.Fatalf("closing: %d", code)
	}
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/dip-readings", admin,
		fmt.Sprintf(`{"tank_id":%q,"reading_type":"closing","dip_mm":1240}`, h.ids.tankPMS.String())); code != http.StatusCreated {
		t.Fatalf("dip: %d", code)
	}

	// Correction allowed while open.
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings/"+openingID+"/correct", admin, `{"reading":1010}`); code != http.StatusOK {
		t.Fatalf("correct while open: %d, want 200", code)
	}
	// Close, then correction must be refused (409).
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	// Re-fetch the active opening id (it was superseded by the correction).
	var activeOpening uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`SELECT id FROM meter_readings WHERE shift_id = $1 AND nozzle_id = $2 AND reading_type = 'opening' AND status = 'active'`,
		shiftID, nozzleID).Scan(&activeOpening); err != nil {
		t.Fatalf("lookup active opening: %v", err)
	}
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings/"+activeOpening.String()+"/correct", admin, `{"reading":1020}`); code != http.StatusConflict {
		t.Fatalf("correct after close: %d, want 409", code)
	}
}

// TestPhase3_ZeroAssignmentClose: a shift with no nozzle assignments can't be
// closed (audit P2).
func TestPhase3_ZeroAssignmentClose(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	st := h.ids.station1.String()

	code, day := h.postJSON(t, "/api/v1/stations/"+st+"/operating-days", admin, `{}`)
	if code != http.StatusCreated {
		t.Fatalf("open day: %d %v", code, day)
	}
	// Rotation linking a login-linked member, so the shift opens (then close
	// fails on the zero *nozzle* assignments guard, not the open guard).
	adminID := h.userID(t, ctx, h.ids.adminEmail)
	seedShiftRotation(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, "morning", &adminID)
	code, shift := h.postJSON(t, "/api/v1/stations/"+st+"/shifts", admin,
		fmt.Sprintf(`{"operating_day_id":%q,"name":"Empty","slot":"morning"}`, mustID(t, day)))
	if code != http.StatusCreated {
		t.Fatalf("open shift: %d %v", code, shift)
	}
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+mustID(t, shift)+"/close", admin, ``); code != http.StatusUnprocessableEntity {
		t.Fatalf("zero-assignment close: %d, want 422", code)
	}
}

// TestPhase3_CrossStationReading: a reading for a nozzle at another station is
// rejected on a station-1 shift.
func TestPhase3_CrossStationReading(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	_, shiftID, _, _ := h.openDayShiftWithAttendant(t, ctx, admin, fmt.Sprintf("att-xs-%d@it.local", time.Now().UnixNano()))

	// A nozzle at station2 (different station than the shift).
	var pump2, nozzle2 uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO pumps (tenant_id, station_id, number, name) VALUES ($1, $2, 9, 'P9') RETURNING id`,
		h.ids.tenantID, h.ids.station2).Scan(&pump2); err != nil {
		t.Fatalf("seed pump2: %v", err)
	}
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO nozzles (tenant_id, station_id, pump_id, tank_id, product_id, number, default_price)
		 VALUES ($1, $2, $3, $4, $5, 1, 2950.00) RETURNING id`,
		h.ids.tenantID, h.ids.station2, pump2, h.ids.tankMSA, h.ids.pmsProduct).Scan(&nozzle2); err != nil {
		t.Fatalf("seed nozzle2: %v", err)
	}
	if code, _ := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/meter-readings", admin,
		fmt.Sprintf(`{"nozzle_id":%q,"reading_type":"opening","reading":1000}`, nozzle2.String())); code != http.StatusBadRequest {
		t.Fatalf("cross-station reading: %d, want 400", code)
	}
}

// TestPhase3_UnassignCascade: unassigning an attendant removes their nozzle
// assignments via the DB cascade (audit P2).
func TestPhase3_UnassignCascade(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	_, shiftID, attID, _ := h.openDayShiftWithAttendant(t, ctx, admin, fmt.Sprintf("att-cascade-%d@it.local", time.Now().UnixNano()))

	var before int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM shift_nozzle_assignments WHERE shift_id = $1 AND attendant_id = $2`, shiftID, attID).Scan(&before)
	if before != 1 {
		t.Fatalf("nozzle assignments before unassign = %d, want 1", before)
	}
	if code, _ := h.do(t, http.MethodDelete, "/api/v1/shifts/"+shiftID+"/attendants/"+attID.String(), admin, nil, ""); code != http.StatusNoContent {
		t.Fatalf("unassign attendant: %d, want 204", code)
	}
	var after int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM shift_nozzle_assignments WHERE shift_id = $1 AND attendant_id = $2`, shiftID, attID).Scan(&after)
	if after != 0 {
		t.Fatalf("nozzle assignments after unassign = %d, want 0 (cascade)", after)
	}
}

// attEmailFromDB returns the most recently seeded attendant email for the
// tenant, so DayWorkflow can log in as the attendant it just created.
func attEmailFromDB(t *testing.T, ctx context.Context, pool *database.Pool, tenantID uuid.UUID) string {
	t.Helper()
	var email string
	if err := pool.QueryRow(ctx,
		`SELECT u.email FROM users u
		 JOIN user_roles ur ON ur.user_id = u.id
		 JOIN roles r ON r.id = ur.role_id
		 WHERE u.tenant_id = $1 AND r.code = 'attendant'
		 ORDER BY u.created_at DESC LIMIT 1`, tenantID).Scan(&email); err != nil {
		t.Fatalf("lookup attendant email: %v", err)
	}
	return email
}
