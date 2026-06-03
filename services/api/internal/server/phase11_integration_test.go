package server_test

// DB-backed integration tests for the Phase 11 workforce surface: the
// deterministic 3-team rotation and the shift-open enforcement ("no shift
// without its expected employees"). Reuses the Phase 2 harness; gated on
// TEST_DATABASE_URL + TEST_REDIS_URL.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5433/fuelgrid?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6379/0 \
//	go test ./services/api/internal/server -run Phase11 -v

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// configureRotation seeds three teams (orders 0,1,2) at station1 and anchors
// the rotation at today (cycle day 0). The demo operator (op) is linked to an
// employee placed on team order 0, so opening today's MORNING shift (order 0
// works morning on cycle day 0) auto-populates that attendant.
func configureRotation(t *testing.T, h *harness) (teamIDs [3]uuid.UUID, opEmployeeID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	for order := 0; order < 3; order++ {
		var id uuid.UUID
		if err := h.pool.QueryRow(ctx, `
			INSERT INTO shift_teams (tenant_id, station_id, name, rotation_order)
			VALUES ($1, $2, $3, $4) RETURNING id`,
			h.ids.tenantID, h.ids.station1, []string{"Team A", "Team B", "Team C"}[order], order,
		).Scan(&id); err != nil {
			t.Fatalf("seed team %d: %v", order, err)
		}
		teamIDs[order] = id
	}
	// Link the operator to an employee on team order 0.
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO employees (tenant_id, station_id, user_id, full_name, role)
		VALUES ($1, $2, $3, 'Rotation Operator', 'pump_attendant') RETURNING id`,
		h.ids.tenantID, h.ids.station1, h.ids.opID,
	).Scan(&opEmployeeID); err != nil {
		t.Fatalf("seed op employee: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO shift_team_members (tenant_id, team_id, employee_id) VALUES ($1, $2, $3)`,
		h.ids.tenantID, teamIDs[0], opEmployeeID); err != nil {
		t.Fatalf("seed team member: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		UPDATE stations SET rotation_anchor_date = CURRENT_DATE WHERE tenant_id = $1 AND id = $2`,
		h.ids.tenantID, h.ids.station1); err != nil {
		t.Fatalf("seed anchor: %v", err)
	}
	// Opening stock is a per-station operational prerequisite of opening a shift.
	seedOpeningStock(t, context.Background(), h.pool, h.ids.tenantID, h.ids.station1)
	return teamIDs, opEmployeeID
}

// TestPhase11_ScheduledTeamRotates proves the rotation cycles correctly over
// three days: with the anchor at cycle day 0, the team working the MORNING slot
// is order 0 on day 0, order 2 on day 1, and order 1 on day 2.
func TestPhase11_ScheduledTeamRotates(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	teamIDs, _ := configureRotation(t, h)
	st := h.ids.station1.String()
	today := time.Now().UTC()

	// Morning slot across the 3-day cycle: orders 0, 2, 1.
	wantMorningOrder := []int{0, 2, 1}
	for i, wantOrder := range wantMorningOrder {
		date := today.AddDate(0, 0, i).Format("2006-01-02")
		path := "/api/v1/stations/" + st + "/scheduled-team?slot=morning&date=" + date
		code, m := h.getJSON(t, path, admin)
		if code != http.StatusOK {
			t.Fatalf("day %d scheduled-team: code=%d body=%v", i, code, m)
		}
		team, _ := m["team"].(map[string]any)
		if team == nil {
			t.Fatalf("day %d: nil team", i)
		}
		if team["id"] != teamIDs[wantOrder].String() {
			t.Fatalf("day %d morning: team=%v want order %d (%s)", i, team["id"], wantOrder, teamIDs[wantOrder])
		}
	}

	// On day 0, evening is order 1 and rest is order 2 — confirm evening differs
	// from morning and is the expected team.
	code, m := h.getJSON(t, "/api/v1/stations/"+st+"/scheduled-team?slot=evening&date="+today.Format("2006-01-02"), admin)
	if code != http.StatusOK {
		t.Fatalf("day 0 evening: code=%d", code)
	}
	team, _ := m["team"].(map[string]any)
	if team == nil || team["id"] != teamIDs[1].String() {
		t.Fatalf("day 0 evening: team=%v want order 1 (%s)", team, teamIDs[1])
	}
}

// TestPhase11_OpenShiftEnforcement proves a shift cannot open without its
// configured team, and that a configured one auto-populates attendants from the
// team's login-linked members.
func TestPhase11_OpenShiftEnforcement(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	st := h.ids.station1.String()

	// Open today's operating day.
	code, day := h.postJSON(t, "/api/v1/stations/"+st+"/operating-days", admin, `{}`)
	if code != http.StatusCreated {
		t.Fatalf("open day: code=%d body=%v", code, day)
	}
	dayID, _ := day["id"].(string)

	// Before the station is operationally ready (no opening stock, no active
	// employee), the open-shift readiness guard rejects with 409 Conflict and
	// lists ONLY the scoped per-station operational blockers — never tenant-wide
	// checklist items (regions, users, teams, rotation_anchor).
	code, body := h.postJSON(t, "/api/v1/stations/"+st+"/shifts", admin,
		`{"operating_day_id":"`+dayID+`","name":"Morning","slot":"morning"}`)
	if code != http.StatusConflict {
		t.Fatalf("open shift before setup: code=%d body=%v (want 409)", code, body)
	}
	blockers, _ := body["blockers"].([]any)
	if len(blockers) == 0 {
		t.Fatalf("open shift before setup: no blockers listed, body=%v", body)
	}
	operational := map[string]bool{
		"stations": true, "tanks": true, "pumps": true,
		"nozzles": true, "opening_stock": true, "employees": true,
	}
	sawOperational := false
	for _, b := range blockers {
		bm, _ := b.(map[string]any)
		bcode, _ := bm["code"].(string)
		if !operational[bcode] {
			t.Fatalf("open shift before setup: unexpected non-operational blocker %q (body=%v)", bcode, body)
		}
		if bcode == "opening_stock" || bcode == "employees" {
			sawOperational = true
		}
	}
	if !sawOperational {
		t.Fatalf("open shift before setup: expected opening_stock/employees blocker, got %v", blockers)
	}

	// A missing/invalid slot is a 400 — slot validation runs before the guard.
	if code, _ := h.postJSON(t, "/api/v1/stations/"+st+"/shifts", admin,
		`{"operating_day_id":"`+dayID+`","name":"Morning"}`); code != http.StatusBadRequest {
		t.Fatalf("open shift w/o slot: code=%d (want 400)", code)
	}

	// Configure rotation: the operator's team (order 0) works morning on day 0.
	// This also seeds opening stock + the operator's employee, satisfying the
	// operational prerequisites.
	configureRotation(t, h)

	// Now opening the morning shift succeeds and auto-populates the operator as
	// an attendant (the only login-linked team member).
	code, shift := h.postJSON(t, "/api/v1/stations/"+st+"/shifts", admin,
		`{"operating_day_id":"`+dayID+`","name":"Morning","slot":"morning"}`)
	if code != http.StatusCreated {
		t.Fatalf("open configured shift: code=%d body=%v (want 201)", code, shift)
	}
	if shift["slot"] != "morning" {
		t.Fatalf("opened shift slot=%v want morning", shift["slot"])
	}
	if shift["team_id"] == nil {
		t.Fatalf("opened shift has no team_id")
	}
	shiftID, _ := shift["id"].(string)

	// The shift detail must list the operator as an auto-assigned attendant.
	code, detail := h.getJSON(t, "/api/v1/shifts/"+shiftID, admin)
	if code != http.StatusOK {
		t.Fatalf("get shift: code=%d", code)
	}
	attendants, _ := detail["attendants"].([]any)
	if len(attendants) != 1 {
		t.Fatalf("attendants=%d want 1 (auto-populated from team)", len(attendants))
	}
	first, _ := attendants[0].(map[string]any)
	if first["user_id"] != h.ids.opID.String() {
		t.Fatalf("auto-assigned attendant=%v want operator %s", first["user_id"], h.ids.opID)
	}

	// The evening slot (order 1) has no members, so it is rejected even though
	// rotation is configured — "no shift without its expected employees".
	code, body = h.postJSON(t, "/api/v1/stations/"+st+"/shifts", admin,
		`{"operating_day_id":"`+dayID+`","name":"Evening","slot":"evening"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("open empty-team shift: code=%d body=%v (want 400)", code, body)
	}
}
