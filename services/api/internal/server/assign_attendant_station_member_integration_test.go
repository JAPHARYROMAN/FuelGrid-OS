package server_test

// Integration tests for the station-membership guard on the supervisor
// "assign attendant to a shift" path (POST /api/v1/shifts/{id}/attendants).
//
// The gap (mobile-attendant PRD closeout, PR #174): the assign path
// roster-added ANY same-tenant user with no server-side check that the user is
// actually part of the shift's station workforce. It is tenant- and
// station-bounded, but a supervisor could still add a user who is not a station
// employee at all (e.g. a head-office account). handleAssignAttendant now
// rejects such users with 422 {"code":"attendant_not_station_member"}, while
// preserving the legitimate ad-hoc SUBSTITUTE case (an active station employee
// who is NOT on today's rotation team).
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the rest of the suite.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// seedUserWithStationAccess creates an active login user with the attendant
// role and station access, but deliberately NO employees row — i.e. a user who
// is in the tenant but is not part of any station workforce.
func seedUserWithStationAccess(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, stationID uuid.UUID, email string) uuid.UUID {
	t.Helper()
	hasher := password.New(password.DefaultParams, "")
	hash, err := hasher.Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	var id uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'Outsider', 'active', $3, now()) RETURNING id`,
		tenantID, email, hash).Scan(&id); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	grantRole(t, ctx, pool, tenantID, id, "attendant")
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_station_access (user_id, station_id, tenant_id) VALUES ($1, $2, $3)`,
		id, stationID, tenantID); err != nil {
		t.Fatalf("station access: %v", err)
	}
	return id
}

// seedStationEmployee links userID to an ACTIVE employees row at stationID
// (without putting them on any rotation team) — i.e. a station-workforce member
// who is available as an ad-hoc substitute.
func seedStationEmployee(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, stationID, userID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var empID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO employees (tenant_id, station_id, user_id, full_name, role, status)
		VALUES ($1, $2, $3, $4, 'pump_attendant', 'active')
		ON CONFLICT (tenant_id, user_id) WHERE user_id IS NOT NULL
		DO UPDATE SET station_id = EXCLUDED.station_id, status = 'active'
		RETURNING id`,
		tenantID, stationID, userID, name).Scan(&empID); err != nil {
		t.Fatalf("seed station employee: %v", err)
	}
	return empID
}

// TestAssignAttendant_StationMembershipGuard exercises the four cases the
// finding calls out, all on one open shift:
//
//	(a) a non-station-employee user            -> rejected, attendant_not_station_member
//	(b) an active station employee, not rostered (substitute) -> allowed
//	(c) an active station employee on the rostered team        -> allowed
//	(d) an employee of a DIFFERENT station     -> rejected (station-bounded)
func TestAssignAttendant_StationMembershipGuard(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	// Open a day + morning shift on station1. openDayShiftWithAttendant seeds the
	// rotation team (with one linked attendant) and auto-populates that attendant.
	_, shiftID, _, _ := h.openDayShiftWithAttendant(t, ctx,
		admin, fmt.Sprintf("att-guard-%d@it.local", time.Now().UnixNano()))

	assign := func(userID uuid.UUID) (int, map[string]any) {
		return h.postJSON(t, "/api/v1/shifts/"+shiftID+"/attendants", admin,
			fmt.Sprintf(`{"user_id":%q}`, userID))
	}

	// (a) A same-tenant user with NO employees row at the station — e.g. a
	// head-office account. Must be rejected with the machine-readable code.
	outsider := seedUserWithStationAccess(t, ctx, h.pool, h.ids.tenantID, h.ids.station1,
		fmt.Sprintf("outsider-%d@it.local", time.Now().UnixNano()))
	if code, body := assign(outsider); code != http.StatusUnprocessableEntity ||
		body["code"] != "attendant_not_station_member" {
		t.Fatalf("(a) non-station-employee assign: code=%d body=%v, want 422 attendant_not_station_member", code, body)
	}

	// (b) An ACTIVE station employee who is NOT on today's rotation team — the
	// legitimate ad-hoc SUBSTITUTE. Must be allowed (201).
	sub := seedUserWithStationAccess(t, ctx, h.pool, h.ids.tenantID, h.ids.station1,
		fmt.Sprintf("sub-%d@it.local", time.Now().UnixNano()))
	seedStationEmployee(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, sub, "Substitute Sam")
	if code, body := assign(sub); code != http.StatusCreated {
		t.Fatalf("(b) substitute (station employee, off-team) assign: code=%d body=%v, want 201", code, body)
	}

	// (c) An ACTIVE station employee who IS on the rostered team. Must be allowed.
	teamMember := seedUserWithStationAccess(t, ctx, h.pool, h.ids.tenantID, h.ids.station1,
		fmt.Sprintf("teammate-%d@it.local", time.Now().UnixNano()))
	empID := seedStationEmployee(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, teamMember, "Team Tina")
	// Put this employee on the shift's rostered team.
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO shift_team_members (tenant_id, team_id, employee_id)
		SELECT s.tenant_id, s.team_id, $3
		FROM shifts s WHERE s.tenant_id = $1 AND s.id = $2
		ON CONFLICT (team_id, employee_id) DO NOTHING`,
		h.ids.tenantID, shiftID, empID); err != nil {
		t.Fatalf("add to team: %v", err)
	}
	if code, body := assign(teamMember); code != http.StatusCreated {
		t.Fatalf("(c) rostered team member assign: code=%d body=%v, want 201", code, body)
	}

	// (d) An employee of a DIFFERENT station (station2). The guard is
	// station-bounded: even though they are in the tenant and on a station
	// workforce, they are not part of THIS shift's station. Must be rejected.
	other := seedUserWithStationAccess(t, ctx, h.pool, h.ids.tenantID, h.ids.station2,
		fmt.Sprintf("station2-%d@it.local", time.Now().UnixNano()))
	seedStationEmployee(t, ctx, h.pool, h.ids.tenantID, h.ids.station2, other, "Other Olive")
	if code, body := assign(other); code != http.StatusUnprocessableEntity ||
		body["code"] != "attendant_not_station_member" {
		t.Fatalf("(d) cross-station employee assign: code=%d body=%v, want 422 attendant_not_station_member", code, body)
	}
}
