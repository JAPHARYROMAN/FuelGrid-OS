package server_test

// Guard test for the integration harness teardown. cleanupTenant must leave
// ZERO rows behind for a tenant across EVERY tenant-scoped table, including
// tables added by future migrations. The test seeds a tenant, drives a
// representative slice of domains so the recent 0098–0103 tables
// (employee_roles/setup_steps/shift_attendance/reading_verifications/
// collection_receipts and their siblings) actually get rows, tears the tenant
// down, then asserts no residual rows survive in any tenant-scoped table.
//
// This is the backstop that fails the moment a future tenant-scoped table is
// added but not torn down: cleanupTenant and residualTenantRows both enumerate
// the live schema, so a new table is automatically both purged and checked.
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

// exerciseTenantDomains drives a full operating lifecycle for the harness'
// tenant so a wide spread of tenant-scoped tables — and specifically the
// 0098–0103 mobile-attendant tables — get rows that the teardown must remove:
//
//   - opening-stock lifecycle (operating day open seeds opening_stock for the
//     station; an explicit opening-stock-request row is created too),
//   - a shift with rotation teams/members (shift_teams, shift_team_members,
//     employees, employee_roles via the rotation seed),
//   - attendant check-in (shift_attendance), nozzle assignment + confirm
//     (shift_nozzle_assignments, shift_attendants),
//   - meter readings (meter_readings) + dip readings (tank_dip_readings),
//   - close (shift_close_lines), reading verification (reading_verifications),
//   - cash submission + supervisor confirm (cash_submissions, collection_receipts),
//   - approval (audit_logs / outbox_events along the way),
//   - calibration upload (tank_calibration_charts + entries).
func (h *harness) exerciseTenantDomains(t *testing.T, ctx context.Context) {
	t.Helper()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	emailA := fmt.Sprintf("att-teardown-%d@it.local", time.Now().UnixNano())
	_, shiftID, attID, nozzleID := h.openDayShiftWithAttendant(t, ctx, admin, emailA)
	att := h.login(t, tenantSlug, emailA)

	// opening_stock_requests: the 0093 table guarded against the original leak.
	// Create one explicitly so a row exists independent of the readiness seed.
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO opening_stock_requests (tenant_id, tank_id, litres, requested_by, status)
		SELECT $1, t.id, 5000.000, u.id, 'draft'
		FROM tanks t
		JOIN users u ON u.tenant_id = $1 AND u.email = $2
		WHERE t.tenant_id = $1 AND t.id = $3`,
		h.ids.tenantID, h.ids.adminEmail, h.ids.tankPMS); err != nil {
		t.Fatalf("seed opening_stock_request: %v", err)
	}

	// Attendant check-in -> shift_attendance.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/check-in", att,
		`{"device_info":{"model":"Teardown","app":"1.0.0"}}`); code != http.StatusCreated {
		t.Fatalf("check-in: %d %v", code, b)
	}

	// Confirm the nozzle assignment -> shift_nozzle_assignments.confirmed_at.
	var assignmentID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`SELECT id FROM shift_nozzle_assignments WHERE shift_id = $1 AND attendant_id = $2`,
		shiftID, attID).Scan(&assignmentID); err != nil {
		t.Fatalf("lookup assignment: %v", err)
	}
	if code, b := h.postJSON(t,
		"/api/v1/shifts/"+shiftID+"/nozzle-assignments/"+assignmentID.String()+"/confirm", att, ``); code != http.StatusOK {
		t.Fatalf("confirm assignment: %d %v", code, b)
	}

	// Readings (opening + closing meter, closing dip) and calibration chart.
	h.capturePMSShiftReadings(t, admin, att, shiftID, nozzleID)

	// Close -> shift_close_lines; cash submission -> cash_submissions.
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/close", admin, ``); code != http.StatusOK {
		t.Fatalf("close: %d %v", code, b)
	}
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission", admin,
		`{"cash_amount":"1475000"}`); code != http.StatusCreated {
		t.Fatalf("cash submission: %d %v", code, b)
	}

	// Reading verification -> reading_verifications (a different supervisor than
	// the recorder: the operator holds reading.override and did not record).
	operator := h.login(t, tenantSlug, h.ids.opEmail)
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/readings/verify", operator, ``); code != http.StatusOK {
		t.Fatalf("verify readings: %d %v", code, b)
	}

	// Collection receipt -> collection_receipts (the operator did not submit the
	// cash — admin did — so the SoD confirm check passes).
	if code, b := h.postJSON(t, "/api/v1/shifts/"+shiftID+"/cash-submission/confirm", operator,
		`{"received_total":"1475000"}`); code != http.StatusCreated {
		t.Fatalf("confirm cash (collection receipt): %d %v", code, b)
	}

	// Approve the shift to round out the lifecycle (audit_logs / outbox_events).
	if code, b := h.patchJSON(t, "/api/v1/shifts/"+shiftID+"/status", admin, `{"status":"approved"}`); code != http.StatusOK {
		t.Fatalf("approve: %d %v", code, b)
	}

	// setup_steps (0099) is written by a dedicated setup-review API not exercised
	// here; seed one station-scoped row directly so the teardown is verified
	// against it too.
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO setup_steps (tenant_id, station_id, code, status, completed_by, completed_at)
		VALUES ($1, $2, 'tanks', 'completed',
		        (SELECT id FROM users WHERE tenant_id = $1 AND email = $3), now())`,
		h.ids.tenantID, h.ids.station1, h.ids.adminEmail); err != nil {
		t.Fatalf("seed setup_step: %v", err)
	}
}

// representativeTeardownTables is a sanity subset: tables the lifecycle above is
// expected to populate. The test fails loudly if the harness ever stops writing
// to one of them (so the "zero residual" assertion can't pass vacuously because
// the lifecycle quietly stopped exercising a domain).
var representativeTeardownTables = []string{
	"opening_stock_requests",
	"shift_attendance",
	"shift_nozzle_assignments",
	"shift_attendants",
	"meter_readings",
	"reading_verifications",
	"cash_submissions",
	"collection_receipts",
	"shift_close_lines",
	"setup_steps",
	"employees",
	"shift_teams",
}

// TestCleanupTenant_LeavesNoResidual is the teardown guard. It seeds a tenant,
// exercises a representative slice of domains so the 0098–0103 tables get rows,
// tears the tenant down with cleanupTenant, and asserts ZERO residual rows
// across EVERY tenant-scoped table for that tenant.
//
// Before the runtime-topological-order fix, cleanupTenant omitted ~22
// tenant-scoped tables (e.g. shift_attendance, reading_verifications,
// collection_receipts, meter_readings, shift_nozzle_assignments, setup_steps,
// employee_roles); their RESTRICT FKs then blocked the parent deletes, so the
// whole tenant tree (tenant + tanks + movements + users) leaked. This test would
// have reported a large non-zero residual; with the fix it is zero.
func TestCleanupTenant_LeavesNoResidual(t *testing.T) {
	h, cleanup := setupHarness(t)
	ctx := context.Background()

	// The harness cleanup() also calls cleanupTenant; that is idempotent (a
	// second purge of an already-empty tenant is a no-op), so deferring it is
	// safe even though this test tears the tenant down itself.
	defer cleanup()

	h.exerciseTenantDomains(t, ctx)

	// Confirm the lifecycle actually populated the representative tables, so the
	// zero-residual assertion below is meaningful and not vacuous.
	for _, tbl := range representativeTeardownTables {
		var n int
		if err := h.pool.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id = $1`, tbl), h.ids.tenantID).Scan(&n); err != nil {
			t.Fatalf("precheck count %s: %v", tbl, err)
		}
		if n == 0 {
			t.Fatalf("precheck: %s has no rows for the tenant — the lifecycle no longer exercises it, "+
				"so the zero-residual assertion would be vacuous", tbl)
		}
	}

	// Tear the tenant down and assert nothing survives in ANY tenant-scoped table.
	cleanupTenant(ctx, h.pool, h.ids.tenantID)

	residual, err := residualTenantRows(ctx, h.pool, h.ids.tenantID)
	if err != nil {
		t.Fatalf("residual scan: %v", err)
	}
	if len(residual) > 0 {
		total := 0
		for _, n := range residual {
			total += n
		}
		t.Fatalf("cleanupTenant left %d residual rows across %d tables: %v "+
			"(a tenant-scoped table is not being torn down)", total, len(residual), residual)
	}

	// The tenant row itself must be gone too.
	var tenantRows int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM tenants WHERE id = $1`, h.ids.tenantID).Scan(&tenantRows); err != nil {
		t.Fatalf("count tenant row: %v", err)
	}
	if tenantRows != 0 {
		t.Fatalf("tenant row survived cleanupTenant: %d rows", tenantRows)
	}
}
