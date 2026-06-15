package server_test

// DB-backed integration tests for per-tenant Scheduled Reports (Reports Center
// Phase 12 — blueprint §8). Reuses the Phase 2 harness. Gated on
// TEST_DATABASE_URL + TEST_REDIS_URL.
//
// Asserts:
//
//	(a) CRUD happy path + permission gating: create/list/get/update/enable/delete a
//	    schedule via reports.schedule; a station-scoped report requires the actor's
//	    own run permission;
//	(b) run-now delivers in_app to a permitted user recipient and records a run;
//	(c) PERMISSION AT DELIVERY: a recipient whose report permission was revoked is
//	    SKIPPED — no notification row created;
//	(d) IDEMPOTENCY: ClaimDue advances next_run_at so a second claim in the same
//	    period returns nothing, and the run-ledger UNIQUE collapses a duplicate
//	    period to one row;
//	(e) cross-tenant isolation / no IDOR: another tenant's schedule id is a 404;
//	(f) webhook SSRF guard: a private/loopback webhook_url is rejected at create.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/scheduledreports"
)

// stationCloseSchedule builds a create body for a station-scoped daily station-close
// schedule delivering in_app to the given user recipient.
func stationCloseSchedule(name, stationID, userID string) string {
	return fmt.Sprintf(`{
		"report_key": "station-close",
		"name": %q,
		"filters": {"station_id": %q},
		"schedule": {"frequency": "daily", "hour": 6, "minute": 0},
		"recipients": [{"type": "user", "value": %q}],
		"delivery_channel": "in_app",
		"format": "csv"
	}`, name, stationID, userID)
}

func TestScheduledReports_CRUDAndPermissionGating(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	station := h.ids.station1.String()

	// Create (admin holds reports.schedule + revenue.read tenant-wide).
	code, body := h.postJSON(t, "/api/v1/reports/scheduled", admin,
		stationCloseSchedule("Daily close MIK-01", station, h.adminID(t).String()))
	if code != http.StatusCreated {
		t.Fatalf("create schedule = %d, want 201 (%v)", code, body)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("create: missing id (%v)", body)
	}
	if body["status"] != "active" || body["enabled"] != true {
		t.Fatalf("create: unexpected status/enabled (%v)", body)
	}
	if body["next_run_at"] == nil {
		t.Fatalf("create: next_run_at not set")
	}

	// List shows it.
	code, list := h.getJSON(t, "/api/v1/reports/scheduled", admin)
	if code != http.StatusOK {
		t.Fatalf("list = %d (%v)", code, list)
	}
	if countOf(list) < 1 {
		t.Fatalf("list count = %d, want >=1", countOf(list))
	}

	// Get by id.
	code, got := h.getJSON(t, "/api/v1/reports/scheduled/"+id, admin)
	if code != http.StatusOK || got["id"] != id {
		t.Fatalf("get = %d (%v)", code, got)
	}

	// Update the name + schedule.
	upd := fmt.Sprintf(`{
		"name": "Renamed close",
		"filters": {"station_id": %q},
		"schedule": {"frequency": "weekly", "hour": 7, "minute": 30, "day_of_week": 1},
		"recipients": [{"type": "email", "value": "ops@example.com"}],
		"delivery_channel": "email",
		"format": "pdf"
	}`, station)
	code, updated := h.do2JSON(t, http.MethodPut, "/api/v1/reports/scheduled/"+id, admin, upd)
	if code != http.StatusOK {
		t.Fatalf("update = %d (%v)", code, updated)
	}
	if updated["name"] != "Renamed close" || updated["delivery_channel"] != "email" {
		t.Fatalf("update did not apply (%v)", updated)
	}

	// Disable then re-enable.
	code, _ = h.postJSON(t, "/api/v1/reports/scheduled/"+id+"/enabled", admin, `{"enabled": false}`)
	if code != http.StatusOK {
		t.Fatalf("disable = %d", code)
	}
	code, dis := h.getJSON(t, "/api/v1/reports/scheduled/"+id, admin)
	if code != http.StatusOK || dis["enabled"] != false || dis["status"] != "paused" {
		t.Fatalf("after disable: %v", dis)
	}
	code, _ = h.postJSON(t, "/api/v1/reports/scheduled/"+id+"/enabled", admin, `{"enabled": true}`)
	if code != http.StatusOK {
		t.Fatalf("enable = %d", code)
	}

	// An unknown report_key is a 400.
	code, _ = h.postJSON(t, "/api/v1/reports/scheduled", admin,
		`{"report_key":"does-not-exist","name":"x","schedule":{"frequency":"daily","hour":1},"recipients":[{"type":"email","value":"a@b.c"}],"delivery_channel":"email","format":"csv"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("unknown report_key = %d, want 400", code)
	}

	// Delete.
	code, _ = h.do2JSON(t, http.MethodDelete, "/api/v1/reports/scheduled/"+id, admin, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", code)
	}
	code, _ = h.getJSON(t, "/api/v1/reports/scheduled/"+id, admin)
	if code != http.StatusNotFound {
		t.Fatalf("get after delete = %d, want 404", code)
	}
}

// TestScheduledReports_PermissionGatingStationScope: an operator scoped to station1
// can schedule a station1 report but NOT a station2 report (403 on the report's own
// permission re-check).
func TestScheduledReports_PermissionGatingStationScope(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	op := h.login(t, tenantSlug, h.ids.opEmail)

	// station1 — in scope: allowed (operator holds reports.schedule + revenue.read at station1).
	code, body := h.postJSON(t, "/api/v1/reports/scheduled", op,
		stationCloseSchedule("op station1", h.ids.station1.String(), h.ids.opID.String()))
	if code != http.StatusCreated {
		t.Fatalf("operator station1 create = %d, want 201 (%v)", code, body)
	}

	// station2 — out of scope: the report's own permission re-check 403s.
	code, body = h.postJSON(t, "/api/v1/reports/scheduled", op,
		stationCloseSchedule("op station2", h.ids.station2.String(), h.ids.opID.String()))
	if code != http.StatusForbidden {
		t.Fatalf("operator station2 create = %d, want 403 (%v)", code, body)
	}
}

// TestScheduledReports_RunNowDeliversInApp: run-now on an in_app schedule creates a
// notification for the permitted user recipient and records a successful run.
func TestScheduledReports_RunNowDeliversInApp(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	ctx := context.Background()

	adminID := h.adminID(t)
	code, body := h.postJSON(t, "/api/v1/reports/scheduled", admin,
		stationCloseSchedule("run-now close", h.ids.station1.String(), adminID.String()))
	if code != http.StatusCreated {
		t.Fatalf("create = %d (%v)", code, body)
	}
	id, _ := body["id"].(string)

	before := h.countNotifications(t, ctx, adminID)

	code, run := h.postJSON(t, "/api/v1/reports/scheduled/"+id+"/run-now", admin, "")
	if code != http.StatusOK {
		t.Fatalf("run-now = %d (%v)", code, run)
	}
	if run["status"] != "success" {
		t.Fatalf("run-now status = %v, want success (%v)", run["status"], run)
	}
	if dc, _ := run["delivered_count"].(float64); dc < 1 {
		t.Fatalf("run-now delivered_count = %v, want >=1", run["delivered_count"])
	}

	after := h.countNotifications(t, ctx, adminID)
	if after != before+1 {
		t.Fatalf("expected exactly one new notification, before=%d after=%d", before, after)
	}

	// The run appears in the schedule's run history.
	code, runs := h.getJSON(t, "/api/v1/reports/scheduled/"+id+"/runs", admin)
	if code != http.StatusOK || countOf(runs) < 1 {
		t.Fatalf("runs list = %d count=%d (%v)", code, countOf(runs), runs)
	}
}

// TestScheduledReports_PermissionRevokedRecipientSkipped: a user recipient whose
// report permission is revoked is SKIPPED at delivery — no notification, recorded as
// skipped.
func TestScheduledReports_PermissionRevokedRecipientSkipped(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	ctx := context.Background()

	// The schedule (owned by admin) targets the OPERATOR as its only in_app recipient.
	code, body := h.postJSON(t, "/api/v1/reports/scheduled", admin,
		stationCloseSchedule("revoke test", h.ids.station1.String(), h.ids.opID.String()))
	if code != http.StatusCreated {
		t.Fatalf("create = %d (%v)", code, body)
	}
	id, _ := body["id"].(string)

	// Revoke the operator's station1 access so they can no longer run the
	// station-scoped report (revenue.read AtStation(station1) now fails for them).
	if _, err := h.pool.Exec(ctx,
		`DELETE FROM user_station_access WHERE user_id = $1 AND station_id = $2 AND tenant_id = $3`,
		h.ids.opID, h.ids.station1, h.ids.tenantID); err != nil {
		t.Fatalf("revoke station access: %v", err)
	}

	before := h.countNotifications(t, ctx, h.ids.opID)
	code, run := h.postJSON(t, "/api/v1/reports/scheduled/"+id+"/run-now", admin, "")
	if code != http.StatusOK {
		t.Fatalf("run-now = %d (%v)", code, run)
	}
	// The owner (admin) still permitted, so the report renders, but the only
	// recipient (operator) is revoked -> delivered 0, skipped >=1.
	if dc, _ := run["delivered_count"].(float64); dc != 0 {
		t.Fatalf("delivered_count = %v, want 0 (revoked recipient must get nothing)", run["delivered_count"])
	}
	if sc, _ := run["skipped_count"].(float64); sc < 1 {
		t.Fatalf("skipped_count = %v, want >=1", run["skipped_count"])
	}
	after := h.countNotifications(t, ctx, h.ids.opID)
	if after != before {
		t.Fatalf("revoked recipient must NOT receive a notification: before=%d after=%d", before, after)
	}
}

// TestScheduledReports_ClaimDueIdempotency: ClaimDue advances next_run_at as it
// selects, so a second claim within the same period returns nothing; and the run
// ledger's UNIQUE (schedule, period_key) collapses a duplicate-period record.
func TestScheduledReports_ClaimDueIdempotency(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	repo := scheduledreports.New(h.pool)

	// Insert a due daily schedule directly (next_run_at in the past) for this tenant.
	sched := scheduledreports.Schedule{Frequency: scheduledreports.FrequencyDaily, Hour: 6, Minute: 0}
	created, err := repo.Create(ctx, h.ids.tenantID, scheduledreports.CreateInput{
		ReportKey:       "station-close",
		Name:            "idem",
		Filters:         map[string]string{"station_id": h.ids.station1.String()},
		Schedule:        sched,
		Recipients:      []scheduledreports.Recipient{{Type: "user", Value: h.adminID(t).String()}},
		DeliveryChannel: "in_app",
		Format:          "csv",
		CreatedBy:       h.adminID(t),
		NextRunAt:       time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	now := time.Now()
	// First claim returns the due row and advances its next_run_at to the future.
	claimed, err := repo.ClaimDue(ctx, now, 25)
	if err != nil {
		t.Fatalf("first ClaimDue: %v", err)
	}
	foundFirst := false
	for _, c := range claimed {
		if c.ID == created.ID {
			foundFirst = true
			if !c.NextRunAt.After(now) {
				t.Fatalf("ClaimDue did not advance next_run_at past now (got %s)", c.NextRunAt)
			}
		}
	}
	if !foundFirst {
		t.Fatalf("first ClaimDue did not return the due schedule")
	}

	// Second claim at the same instant must NOT re-return it (idempotent within the
	// period — next_run_at was advanced).
	claimed2, err := repo.ClaimDue(ctx, now, 25)
	if err != nil {
		t.Fatalf("second ClaimDue: %v", err)
	}
	for _, c := range claimed2 {
		if c.ID == created.ID {
			t.Fatalf("second ClaimDue re-returned the same schedule in the same period (not idempotent)")
		}
	}

	// The run-ledger UNIQUE collapses a duplicate period to one row.
	periodKey := sched.PeriodKey(now)
	_, err = repo.RecordRun(ctx, h.ids.tenantID, scheduledreports.RecordRunInput{
		ScheduledReportID: created.ID, PeriodKey: periodKey, Status: scheduledreports.RunSuccess,
	})
	if err != nil {
		t.Fatalf("first RecordRun: %v", err)
	}
	_, err = repo.RecordRun(ctx, h.ids.tenantID, scheduledreports.RecordRunInput{
		ScheduledReportID: created.ID, PeriodKey: periodKey, Status: scheduledreports.RunSuccess,
	})
	if err == nil {
		t.Fatalf("second RecordRun for the same period should fail (idempotency guard)")
	}

	// Cleanup the directly-inserted schedule.
	_ = repo.Delete(ctx, h.ids.tenantID, created.ID)
}

// TestScheduledReports_CrossTenantIsolation: a schedule created in tenant A is a 404
// (not an IDOR) for tenant B, even though B is a valid authenticated actor.
func TestScheduledReports_CrossTenantIsolation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	repo := scheduledreports.New(h.pool)

	// Create a schedule in a SECOND tenant directly (owned by that tenant's owner).
	otherTenant, otherUser := h.seedSecondTenant(t, ctx)
	created, err := repo.Create(ctx, otherTenant, scheduledreports.CreateInput{
		ReportKey:       "financials",
		Name:            "other tenant",
		Schedule:        scheduledreports.Schedule{Frequency: scheduledreports.FrequencyDaily, Hour: 6},
		Recipients:      []scheduledreports.Recipient{{Type: "email", Value: "x@y.z"}},
		DeliveryChannel: "email",
		Format:          "csv",
		CreatedBy:       otherUser,
		NextRunAt:       time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create other-tenant schedule: %v", err)
	}

	// Tenant A's admin must NOT be able to GET / PUT / DELETE / run-now that id.
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	otherID := created.ID.String()

	code, _ := h.getJSON(t, "/api/v1/reports/scheduled/"+otherID, admin)
	if code != http.StatusNotFound {
		t.Fatalf("cross-tenant GET = %d, want 404 (no IDOR)", code)
	}
	code, _ = h.do2JSON(t, http.MethodDelete, "/api/v1/reports/scheduled/"+otherID, admin, "")
	if code != http.StatusNotFound {
		t.Fatalf("cross-tenant DELETE = %d, want 404", code)
	}
	code, _ = h.postJSON(t, "/api/v1/reports/scheduled/"+otherID+"/run-now", admin, "")
	if code != http.StatusNotFound {
		t.Fatalf("cross-tenant run-now = %d, want 404", code)
	}

	_ = repo.Delete(ctx, otherTenant, created.ID)
}

// TestScheduledReports_WebhookSSRFRejected: a webhook schedule pointing at a
// loopback / private host is rejected at create.
func TestScheduledReports_WebhookSSRFRejected(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	station := h.ids.station1.String()

	for _, bad := range []string{
		"http://hooks.example.com/x", // not https
		"https://127.0.0.1/x",        // loopback
		"https://169.254.169.254/x",  // cloud metadata
		"https://10.0.0.1/x",         // private
		"https://192.168.1.1/x",      // private
	} {
		body := fmt.Sprintf(`{
			"report_key": "station-close",
			"name": "ssrf",
			"filters": {"station_id": %q},
			"schedule": {"frequency": "daily", "hour": 6},
			"recipients": [],
			"delivery_channel": "webhook",
			"format": "csv",
			"webhook_url": %q
		}`, station, bad)
		code, resp := h.postJSON(t, "/api/v1/reports/scheduled", admin, body)
		if code != http.StatusBadRequest {
			t.Fatalf("webhook %q = %d, want 400 (SSRF guard) (%v)", bad, code, resp)
		}
	}
}

// --- harness helpers used only by these tests ---

// do2JSON issues a request with an optional JSON body and returns the decoded map.
func (h *harness) do2JSON(t *testing.T, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	ct := ""
	if body != "" {
		rdr = bytes.NewReader([]byte(body))
		ct = "application/json"
	}
	var code int
	var raw []byte
	if rdr != nil {
		code, raw = h.do(t, method, path, token, rdr, ct)
	} else {
		code, raw = h.do(t, method, path, token, nil, ct)
	}
	var m map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	return code, m
}

// adminID resolves the seeded system_admin user id for the tenant.
func (h *harness) adminID(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE tenant_id = $1 AND email = $2`, h.ids.tenantID, h.ids.adminEmail).Scan(&id); err != nil {
		t.Fatalf("resolve admin id: %v", err)
	}
	return id
}

// countNotifications counts private notification rows for the given user.
func (h *harness) countNotifications(t *testing.T, ctx context.Context, userID uuid.UUID) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE tenant_id = $1 AND user_id = $2`,
		h.ids.tenantID, userID).Scan(&n); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	return n
}

// seedSecondTenant creates a minimal second tenant + owner user for cross-tenant
// isolation testing and returns their ids.
func (h *harness) seedSecondTenant(t *testing.T, ctx context.Context) (tenantID, userID uuid.UUID) {
	t.Helper()
	suffix := time.Now().UnixNano()
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO tenants (name, slug, status) VALUES ($1, $2, 'active') RETURNING id`,
		fmt.Sprintf("Other %d", suffix), fmt.Sprintf("other-%d", suffix)).Scan(&tenantID); err != nil {
		t.Fatalf("seed second tenant: %v", err)
	}
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'Other Owner', 'active', 'x', now()) RETURNING id`,
		tenantID, fmt.Sprintf("owner-%d@other.local", suffix)).Scan(&userID); err != nil {
		t.Fatalf("seed second tenant user: %v", err)
	}
	return tenantID, userID
}
