package server_test

// DB-backed integration tests for the governance/enterprise PARTIAL finishes:
//   9.2  approval-policy edit + enable/disable (a disabled policy no longer
//        requires approval in /simulate)
//   13.1 enterprise scope-switch listing + cross-scope leakage guard
//   13.3 API-exposed observability health snapshot
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the other integration suites.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// invSendJSON issues a method+JSON-body request and decodes the response, for
// the PUT/PATCH verbs the existing invPostJSON helper doesn't cover.
func (h *harness) invSendJSON(t *testing.T, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	code, out := h.do(t, method, path, token, bytes.NewReader(raw), "application/json")
	var m map[string]any
	if len(out) > 0 {
		_ = json.Unmarshal(out, &m)
	}
	return code, m
}

// freshAttendant seeds a brand-new attendant (the minimal system role) and logs
// it in. Used to prove the manage/scope/observability gates 403 a principal that
// holds none of those permissions.
func (h *harness) freshAttendant(t *testing.T, ctx context.Context, slug string) string {
	t.Helper()
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := fmt.Sprintf("att-gef-%d@it.local", time.Now().UnixNano())
	var uid uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'GEF Attendant', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, email, hash).Scan(&uid); err != nil {
		t.Fatalf("seed attendant: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, uid, "attendant")
	return h.login(t, slug, email)
}

// TestGovEnterprise_PolicyEditDisable covers Feature 9.2: editing a policy and
// disabling it. Critically, a disabled policy must not require approval in
// /simulate — proving the enable/disable toggle actually gates the engine.
func TestGovEnterprise_PolicyEditDisable(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, admin := h.adminContext(t, ctx)

	// Create a policy that requires a single approval for the workflow.
	code, created := h.invPostJSON(t, "/api/v1/approval-policies", admin, map[string]any{
		"workflow_type": "gef_flow", "min_amount": "0", "required_approvals": 1,
	})
	if code != http.StatusCreated {
		t.Fatalf("create policy = %d %v", code, created)
	}
	policyID := created["id"].(string)

	// Sanity: with the active policy, an approval IS required.
	if code, sim := h.invPostJSON(t, "/api/v1/approval-policies/simulate", admin, map[string]any{
		"workflow_type": "gef_flow", "amount": "100",
	}); code != http.StatusOK || !sim["approval_required"].(bool) || sim["required_approvals"].(float64) != 1 {
		t.Fatalf("simulate (active) = %d %v; want required, 1", code, sim)
	}

	// Edit: bump the required approvals to 3 and set a role. PATCH and PUT both
	// edit; exercise PATCH here.
	code, edited := h.invSendJSON(t, http.MethodPatch, "/api/v1/approval-policies/"+policyID, admin, map[string]any{
		"workflow_type": "gef_flow", "min_amount": "0", "required_approvals": 3, "required_role": "finance_manager",
	})
	if code != http.StatusOK || edited["required_approvals"].(float64) != 3 || edited["required_role"] != "finance_manager" {
		t.Fatalf("edit policy = %d %v; want 200, 3 approvals, finance_manager", code, edited)
	}

	// The edit is reflected by the engine: simulate now demands 3 approvals.
	if code, sim := h.invPostJSON(t, "/api/v1/approval-policies/simulate", admin, map[string]any{
		"workflow_type": "gef_flow", "amount": "100",
	}); code != http.StatusOK || sim["required_approvals"].(float64) != 3 || sim["required_role"] != "finance_manager" {
		t.Fatalf("simulate (after edit) = %d %v; want 3 approvals, finance_manager", code, sim)
	}

	// Disable the policy (status -> archived).
	code, disabled := h.invSendJSON(t, http.MethodPatch, "/api/v1/approval-policies/"+policyID+"/status", admin, map[string]any{
		"status": "archived",
	})
	if code != http.StatusOK || disabled["status"] != "archived" {
		t.Fatalf("disable policy = %d %v; want 200 archived", code, disabled)
	}

	// THE KEY ASSERTION: a disabled policy is ignored by the engine, so the same
	// workflow + amount is no longer required to be approved.
	if code, sim := h.invPostJSON(t, "/api/v1/approval-policies/simulate", admin, map[string]any{
		"workflow_type": "gef_flow", "amount": "100",
	}); code != http.StatusOK || sim["approval_required"].(bool) || sim["matched"].(bool) {
		t.Fatalf("simulate (disabled) = %d %v; want NOT required", code, sim)
	}

	// Re-enable restores the gating.
	if code, re := h.invSendJSON(t, http.MethodPatch, "/api/v1/approval-policies/"+policyID+"/status", admin, map[string]any{
		"status": "active",
	}); code != http.StatusOK || re["status"] != "active" {
		t.Fatalf("re-enable = %d %v", code, re)
	}
	if code, sim := h.invPostJSON(t, "/api/v1/approval-policies/simulate", admin, map[string]any{
		"workflow_type": "gef_flow", "amount": "100",
	}); code != http.StatusOK || !sim["approval_required"].(bool) {
		t.Fatalf("simulate (re-enabled) = %d %v; want required again", code, sim)
	}

	// Validation: a bogus status is rejected; an unknown id is 404.
	if code, _ := h.invSendJSON(t, http.MethodPatch, "/api/v1/approval-policies/"+policyID+"/status", admin, map[string]any{"status": "nope"}); code != http.StatusBadRequest {
		t.Fatalf("bad status should be 400, got %d", code)
	}
	if code, _ := h.invSendJSON(t, http.MethodPatch, "/api/v1/approval-policies/"+uuid.NewString(), admin, map[string]any{"workflow_type": "x"}); code != http.StatusNotFound {
		t.Fatalf("edit unknown policy should be 404, got %d", code)
	}

	// 403: a freshly-created attendant holds neither approval_policy.manage, so
	// edit and status are refused at the route.
	att := h.freshAttendant(t, ctx, slug)
	if code, _ := h.invSendJSON(t, http.MethodPatch, "/api/v1/approval-policies/"+policyID, att, map[string]any{"workflow_type": "gef_flow"}); code != http.StatusForbidden {
		t.Fatalf("attendant edit should be 403, got %d", code)
	}
	if code, _ := h.invSendJSON(t, http.MethodPatch, "/api/v1/approval-policies/"+policyID+"/status", att, map[string]any{"status": "archived"}); code != http.StatusForbidden {
		t.Fatalf("attendant disable should be 403, got %d", code)
	}
}

// TestGovEnterprise_ScopeSwitch covers Feature 13.1: the scope-switcher listing
// reflects the user's grants, and a scoped user cannot read across a scope they
// were not granted (cross-scope leakage guard).
func TestGovEnterprise_ScopeSwitch(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, slug, admin := h.adminContext(t, ctx)

	// With no explicit enterprise scope grants the switcher reports no
	// tenant-level grant and an empty option list — the user's RBAC tenant-wide
	// power is not an enterprise scope grant, so there is nothing to switch
	// between (the UI hides the switcher in this case).
	if code, sc := h.getJSON(t, "/api/v1/enterprise/scopes", admin); code != http.StatusOK ||
		sc["tenant_wide"].(bool) || len(sc["scopes"].([]any)) != 0 {
		t.Fatalf("admin scopes (no grants) = %d %v; want not tenant_wide, no scopes", code, sc)
	}

	// Grant the admin an explicit company scope. The switcher reports the granted
	// scope with its resolved station count (2 seeded stations in the company),
	// and tenant_wide stays false (no tenant-level grant yet).
	var companyID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT company_id FROM stations WHERE tenant_id = $1 AND id = $2`, h.ids.tenantID, h.ids.station1).Scan(&companyID); err != nil {
		t.Fatalf("company id: %v", err)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/enterprise/scope-grants", admin, map[string]any{
		"user_id": adminID.String(), "scope_type": "company", "scope_id": companyID.String(),
	}); code != http.StatusCreated {
		t.Fatalf("grant company scope: %d", code)
	}
	code, sc := h.getJSON(t, "/api/v1/enterprise/scopes", admin)
	if code != http.StatusOK || sc["tenant_wide"].(bool) {
		t.Fatalf("scopes after grant = %d %v; want not tenant_wide", code, sc)
	}
	scopes := sc["scopes"].([]any)
	if len(scopes) != 1 {
		t.Fatalf("want 1 scope, got %v", scopes)
	}
	opt := scopes[0].(map[string]any)
	if opt["scope_type"] != "company" || opt["station_count"].(float64) != 2 {
		t.Fatalf("scope option = %v; want company / 2 stations", opt)
	}

	// A tenant-level grant flips tenant_wide to true (it short-circuits the
	// per-scope enumeration: a tenant grant means "all stations").
	if code, _ := h.invPostJSON(t, "/api/v1/enterprise/scope-grants", admin, map[string]any{
		"user_id": adminID.String(), "scope_type": "tenant",
	}); code != http.StatusCreated {
		t.Fatalf("grant tenant scope: %d", code)
	}
	if code, sc := h.getJSON(t, "/api/v1/enterprise/scopes", admin); code != http.StatusOK || !sc["tenant_wide"].(bool) {
		t.Fatalf("admin scopes (tenant grant) = %d %v; want tenant_wide", code, sc)
	}

	// Cross-scope leakage guard: the station-scoped operator (scoped to station1)
	// cannot read a tank that belongs to the out-of-scope station2 — scoped reads
	// enforce station access server-side, so a scope switch can never widen what
	// a user may see. This is the invariant the switcher relies on.
	op := h.login(t, slug, h.ids.opEmail)
	if code, _ := h.do(t, http.MethodGet, "/api/v1/tanks/"+h.ids.tankMSA.String(), op, nil, ""); code != http.StatusForbidden {
		t.Fatalf("operator cross-scope tank read = %d; want 403", code)
	}
	if code, _ := h.do(t, http.MethodGet, "/api/v1/tanks?station_id="+h.ids.station2.String(), op, nil, ""); code != http.StatusForbidden {
		t.Fatalf("operator cross-scope filter = %d; want 403", code)
	}

	// 403: a fresh attendant holds no enterprise.scope.switch, so the listing is
	// refused at the route.
	att := h.freshAttendant(t, ctx, slug)
	if code, _ := h.getJSON(t, "/api/v1/enterprise/scopes", att); code != http.StatusForbidden {
		t.Fatalf("attendant scopes should be 403, got %d", code)
	}
}

// TestGovEnterprise_ObservabilityHealth covers Feature 13.3: the API-exposed
// observability snapshot reports dependency health, outbox counts, and the
// scheduler last run, and is gated on audit.read.
func TestGovEnterprise_ObservabilityHealth(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, admin := h.adminContext(t, ctx)

	code, body := h.getJSON(t, "/api/v1/observability/health", admin)
	if code != http.StatusOK {
		t.Fatalf("observability health = %d %v", code, body)
	}
	// Postgres reachable (the harness runs against a live DB).
	checks, _ := body["checks"].(map[string]any)
	if checks["postgres"] != "ok" {
		t.Fatalf("postgres check = %v; want ok", checks["postgres"])
	}
	// Outbox block is present with numeric counts.
	outbox, ok := body["outbox"].(map[string]any)
	if !ok {
		t.Fatalf("outbox block missing: %v", body)
	}
	if _, ok := outbox["backlog"].(float64); !ok {
		t.Fatalf("outbox.backlog not numeric: %v", outbox)
	}
	if _, ok := outbox["dead_letter"].(float64); !ok {
		t.Fatalf("outbox.dead_letter not numeric: %v", outbox)
	}
	if _, ok := body["healthy"].(bool); !ok {
		t.Fatalf("healthy not boolean: %v", body)
	}

	// 403: a fresh attendant holds no audit.read.
	att := h.freshAttendant(t, ctx, slug)
	if code, _ := h.getJSON(t, "/api/v1/observability/health", att); code != http.StatusForbidden {
		t.Fatalf("attendant observability should be 403, got %d", code)
	}
}
