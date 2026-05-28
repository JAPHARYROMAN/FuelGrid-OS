package server_test

// DB-backed integration tests for Phase 9 — Chain & Enterprise Command.
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL.

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestPhase9_Governance covers Category A: station groups, delegated scope
// resolution to effective stations, and the generic approval engine.
func TestPhase9_Governance(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	// Station group + membership.
	code, grp := h.invPostJSON(t, "/api/v1/enterprise/station-groups", admin, map[string]any{"name": "Highway Corridor", "kind": "corridor"})
	if code != http.StatusCreated {
		t.Fatalf("create group = %d %v", code, grp)
	}
	groupID := grp["id"].(string)
	if code, _ := h.invPostJSON(t, "/api/v1/enterprise/station-groups/"+groupID+"/members", admin, map[string]any{"station_id": h.ids.station1.String()}); code != http.StatusOK {
		t.Fatalf("add member: %d", code)
	}

	// Company-scope grant resolves to all stations in the company (2 seeded).
	var companyID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT company_id FROM stations WHERE tenant_id = $1 AND id = $2`, h.ids.tenantID, h.ids.station1).Scan(&companyID); err != nil {
		t.Fatalf("company id: %v", err)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/enterprise/scope-grants", admin, map[string]any{
		"user_id": adminID.String(), "scope_type": "company", "scope_id": companyID.String(),
	}); code != http.StatusCreated {
		t.Fatalf("grant scope: %d", code)
	}
	code, eff := h.getJSON(t, "/api/v1/enterprise/users/"+adminID.String()+"/effective-stations", admin)
	if code != http.StatusOK || eff["tenant_wide"].(bool) {
		t.Fatalf("effective stations = %d %v", code, eff)
	}
	if len(eff["station_ids"].([]any)) != 2 {
		t.Fatalf("expected 2 effective stations, got %v", eff["station_ids"])
	}

	// Approval engine: a single-approval policy.
	if code, _ := h.invPostJSON(t, "/api/v1/approval-policies", admin, map[string]any{
		"workflow_type": "central_price", "min_amount": "0", "required_approvals": 1,
	}); code != http.StatusCreated {
		t.Fatalf("create policy: %d", code)
	}
	code, ar := h.invPostJSON(t, "/api/v1/approval-requests", admin, map[string]any{
		"workflow_type": "central_price", "amount": "100", "reference_type": "price_rollout",
	})
	if code != http.StatusCreated || ar["required_approvals"].(float64) != 1 || ar["status"] != "requested" {
		t.Fatalf("raise request = %d %v", code, ar)
	}
	reqID := ar["id"].(string)
	if code, dec := h.invPostJSON(t, "/api/v1/approval-requests/"+reqID+"/decide", admin, map[string]any{"decision": "approve"}); code != http.StatusOK || dec["status"] != "approved" {
		t.Fatalf("approve = %d %v", code, dec)
	}
	// Deciding an already-approved request is rejected.
	if code, _ := h.invPostJSON(t, "/api/v1/approval-requests/"+reqID+"/decide", admin, map[string]any{"decision": "approve"}); code != http.StatusConflict {
		t.Fatalf("re-decide: %d, want 409", code)
	}

	// A reject path.
	code, ar2 := h.invPostJSON(t, "/api/v1/approval-requests", admin, map[string]any{"workflow_type": "central_price", "amount": "100"})
	if code != http.StatusCreated {
		t.Fatalf("raise request 2: %d", code)
	}
	if code, dec := h.invPostJSON(t, "/api/v1/approval-requests/"+ar2["id"].(string)+"/decide", admin, map[string]any{"decision": "reject", "comment": "no"}); code != http.StatusOK || dec["status"] != "rejected" {
		t.Fatalf("reject = %d %v", code, dec)
	}
}
