package server_test

import (
	"context"
	"net/http"
	"testing"
)

// seedClosedPeriod inserts an accounting period in the given status directly,
// returning its id. Used so the closed-period change-request tests don't have to
// drive the whole posting/close pipeline.
func seedClosedPeriod(t *testing.T, ctx context.Context, h *harness, start, end, status string) string {
	t.Helper()
	var id string
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO accounting_periods (tenant_id, start_date, end_date, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, h.ids.tenantID, start, end, status).Scan(&id); err != nil {
		t.Fatalf("seed period: %v", err)
	}
	return id
}

// TestRetention_PolicyCRUD drives the retention-policy CRUD surface end to end:
// create, list, update, conflict-on-duplicate-scope, validation, and delete.
func TestRetention_PolicyCRUD(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	// Empty to start.
	code, body := h.getJSON(t, "/api/v1/retention-policies", admin)
	if code != http.StatusOK || countOf(body) != 0 {
		t.Fatalf("initial list: status %d count %v", code, body["count"])
	}

	// Create an audit policy.
	code, body = h.invPostJSON(t, "/api/v1/retention-policies", admin, map[string]any{
		"scope": "audit", "retention_days": 365,
	})
	if code != http.StatusCreated || body["scope"] != "audit" {
		t.Fatalf("create policy: status %d: %v", code, body)
	}
	if rd, _ := body["retention_days"].(float64); int(rd) != 365 {
		t.Fatalf("retention_days = %v, want 365", body["retention_days"])
	}
	if body["status"] != "active" {
		t.Fatalf("status = %v, want active (default)", body["status"])
	}
	policyID := body["id"].(string)

	// Invalid scope is rejected.
	if code, _ := h.invPostJSON(t, "/api/v1/retention-policies", admin, map[string]any{
		"scope": "bogus", "retention_days": 10,
	}); code != http.StatusBadRequest {
		t.Fatalf("invalid scope: status %d, want 400", code)
	}

	// Non-positive days is rejected.
	if code, _ := h.invPostJSON(t, "/api/v1/retention-policies", admin, map[string]any{
		"scope": "session", "retention_days": 0,
	}); code != http.StatusBadRequest {
		t.Fatalf("zero days: status %d, want 400", code)
	}

	// A second policy for the same scope is a 409.
	if code, _ := h.invPostJSON(t, "/api/v1/retention-policies", admin, map[string]any{
		"scope": "audit", "retention_days": 90,
	}); code != http.StatusConflict {
		t.Fatalf("duplicate scope: status %d, want 409", code)
	}

	// Update the policy (PATCH days + status).
	code, body = h.patchJSON(t, "/api/v1/retention-policies/"+policyID, admin, `{"retention_days":730,"status":"disabled"}`)
	if code != http.StatusOK {
		t.Fatalf("update policy: status %d: %v", code, body)
	}
	if rd, _ := body["retention_days"].(float64); int(rd) != 730 || body["status"] != "disabled" {
		t.Fatalf("after update = days %v status %v, want 730/disabled", body["retention_days"], body["status"])
	}

	// List now shows the one policy.
	code, body = h.getJSON(t, "/api/v1/retention-policies", admin)
	if code != http.StatusOK || countOf(body) != 1 {
		t.Fatalf("list after update: status %d count %v", code, body["count"])
	}

	// Delete it → 204, then list is empty again.
	if code, _ := h.do(t, http.MethodDelete, "/api/v1/retention-policies/"+policyID, admin, nil, ""); code != http.StatusNoContent {
		t.Fatalf("delete policy: status %d, want 204", code)
	}
	code, body = h.getJSON(t, "/api/v1/retention-policies", admin)
	if code != http.StatusOK || countOf(body) != 0 {
		t.Fatalf("list after delete: status %d count %v", code, body["count"])
	}

	// The retention job-run history endpoint is reachable (empty until the sweep
	// runs).
	if code, _ := h.getJSON(t, "/api/v1/retention-policies/job-runs", admin); code != http.StatusOK {
		t.Fatalf("job-runs: status %d, want 200", code)
	}
}

// TestRetention_PolicyForbidden proves a freshly-created attendant (lacking
// retention.manage) is forbidden from the retention-policy surface.
func TestRetention_PolicyForbidden(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, _ := h.adminContext(t, ctx)

	attEmail := "retention-att@fuelgrid.local"
	seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, attEmail)
	attendant := h.login(t, slug, attEmail)

	if code, _ := h.getJSON(t, "/api/v1/retention-policies", attendant); code != http.StatusForbidden {
		t.Fatalf("attendant list policies: status %d, want 403", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/retention-policies", attendant, map[string]any{
		"scope": "audit", "retention_days": 30,
	}); code != http.StatusForbidden {
		t.Fatalf("attendant create policy: status %d, want 403", code)
	}
}

// TestClosedPeriodChange_MakerChecker drives the closed-period change-request
// maker-checker: request against a closed period, a different user approves; and
// proves the separation-of-duties guard (no self-approve / self-reject), the
// "only closed periods" guard, and "no duplicate pending request".
func TestClosedPeriodChange_MakerChecker(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, admin := h.adminContext(t, ctx)
	approver := h.secondApprover(t, ctx, slug)

	closedPeriod := seedClosedPeriod(t, ctx, h, "2026-01-01", "2026-01-31", "closed")
	openPeriod := seedClosedPeriod(t, ctx, h, "2026-02-01", "2026-02-28", "open")

	// A blank reason is rejected.
	if code, _ := h.invPostJSON(t, "/api/v1/accounting-periods/"+closedPeriod+"/change-requests", admin, map[string]any{
		"change_type": "reopen", "reason": "  ",
	}); code != http.StatusBadRequest {
		t.Fatalf("blank reason: status %d, want 400", code)
	}

	// A change request against an OPEN period is rejected (only closed/locked
	// periods need one).
	if code, _ := h.invPostJSON(t, "/api/v1/accounting-periods/"+openPeriod+"/change-requests", admin, map[string]any{
		"change_type": "reopen", "reason": "should fail",
	}); code != http.StatusUnprocessableEntity {
		t.Fatalf("change-request on open period: status %d, want 422", code)
	}

	// Request a reopen of the closed period.
	code, body := h.invPostJSON(t, "/api/v1/accounting-periods/"+closedPeriod+"/change-requests", admin, map[string]any{
		"change_type": "reopen", "reason": "month-end correction discovered",
	})
	if code != http.StatusCreated || body["status"] != "requested" || body["change_type"] != "reopen" {
		t.Fatalf("request change: status %d: %v", code, body)
	}
	reqID := body["id"].(string)

	// A second pending request for the same period is a 409.
	if code, _ := h.invPostJSON(t, "/api/v1/accounting-periods/"+closedPeriod+"/change-requests", admin, map[string]any{
		"change_type": "reopen", "reason": "again",
	}); code != http.StatusConflict {
		t.Fatalf("duplicate pending request: status %d, want 409", code)
	}

	// The requester cannot approve their own request (separation of duties).
	if code, _ := h.invPostJSON(t, "/api/v1/closed-period-change-requests/"+reqID+"/approve", admin, map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("self-approve: status %d, want 403", code)
	}
	// Nor reject it.
	if code, _ := h.invPostJSON(t, "/api/v1/closed-period-change-requests/"+reqID+"/reject", admin, map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("self-reject: status %d, want 403", code)
	}

	// A different user approves → approved.
	code, body = h.invPostJSON(t, "/api/v1/closed-period-change-requests/"+reqID+"/approve", approver, map[string]any{
		"note": "verified with finance",
	})
	if code != http.StatusOK || body["status"] != "approved" {
		t.Fatalf("approve: status %d: %v", code, body)
	}

	// A second approve of an already-approved request is a 409 (terminal state).
	if code, _ := h.invPostJSON(t, "/api/v1/closed-period-change-requests/"+reqID+"/approve", approver, map[string]any{}); code != http.StatusConflict {
		t.Fatalf("re-approve: status %d, want 409", code)
	}

	// Once the prior request is decided, the period is free to be requested again.
	code, body = h.invPostJSON(t, "/api/v1/accounting-periods/"+closedPeriod+"/change-requests", admin, map[string]any{
		"change_type": "relock", "reason": "lock it down again",
	})
	if code != http.StatusCreated || body["status"] != "requested" {
		t.Fatalf("re-request after decision: status %d: %v", code, body)
	}
	req2 := body["id"].(string)

	// The approver rejects this one → rejected.
	code, body = h.invPostJSON(t, "/api/v1/closed-period-change-requests/"+req2+"/reject", approver, map[string]any{
		"note": "not warranted",
	})
	if code != http.StatusOK || body["status"] != "rejected" {
		t.Fatalf("reject: status %d: %v", code, body)
	}

	// The queue lists both decided requests.
	code, list := h.getJSON(t, "/api/v1/closed-period-change-requests", admin)
	if code != http.StatusOK {
		t.Fatalf("list change requests: %d", code)
	}
	if items, _ := list["items"].([]any); len(items) != 2 {
		t.Fatalf("change-request count = %d, want 2", len(items))
	}
}

// TestClosedPeriodChange_Forbidden proves a freshly-created attendant (lacking
// closed_period.change) is forbidden from requesting and deciding change
// requests.
func TestClosedPeriodChange_Forbidden(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, admin := h.adminContext(t, ctx)

	closedPeriod := seedClosedPeriod(t, ctx, h, "2026-03-01", "2026-03-31", "closed")
	code, body := h.invPostJSON(t, "/api/v1/accounting-periods/"+closedPeriod+"/change-requests", admin, map[string]any{
		"change_type": "reopen", "reason": "test",
	})
	if code != http.StatusCreated {
		t.Fatalf("seed change request: %d %v", code, body)
	}
	reqID := body["id"].(string)

	attEmail := "cpcr-att@fuelgrid.local"
	seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, attEmail)
	attendant := h.login(t, slug, attEmail)

	if code, _ := h.invPostJSON(t, "/api/v1/accounting-periods/"+closedPeriod+"/change-requests", attendant, map[string]any{
		"change_type": "reopen", "reason": "x",
	}); code != http.StatusForbidden {
		t.Fatalf("attendant request: status %d, want 403", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/closed-period-change-requests/"+reqID+"/approve", attendant, map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("attendant approve: status %d, want 403", code)
	}
}
