package server_test

// DB-backed integration tests for the Report Insight Rules Engine (Reports Center
// Phase 15). They run against a MIGRATED throwaway database (the 0115 migration
// must have created report_rules + seeded the system rules + the
// reports.rules.manage permission). Gated on TEST_DATABASE_URL + TEST_REDIS_URL
// via the shared Phase 2 harness; the suite skips when either is unset.
//
// Coverage: the seeded system rules exist (and are shadow), CRUD round-trips,
// permission gating (a role without reports.rules.manage is 403), cross-tenant
// isolation (a rule id from tenant A is a clean 404 for tenant B), a system rule
// cannot be DELETEd, condition validation rejects an unknown evaluator, and the
// audit trail records the writes.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// rawBody wraps a raw JSON string as a request body WITHOUT re-marshaling (the
// shared jsonBody helper marshals its argument, which would double-encode a string
// literal).
func rawBody(s string) io.Reader { return bytes.NewReader([]byte(s)) }

// listReportRules returns the JSON rows from GET /reports/rules for a token.
func listReportRules(t *testing.T, h *harness, token string) (int, []map[string]any) {
	t.Helper()
	code, raw := h.do(t, http.MethodGet, "/api/v1/reports/rules?limit=200", token, nil, "")
	if code != http.StatusOK {
		return code, nil
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(raw, &out)
	return code, out.Items
}

func TestReportRules_SeededSystemRulesExist(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	code, rows := listReportRules(t, h, admin)
	if code != http.StatusOK {
		t.Fatalf("list rules: code=%d", code)
	}
	// The 0115 seed inserts 8 system rules; assert the canonical codes are present,
	// all is_system, all shadow (so they cannot regress default report output).
	byCode := map[string]map[string]any{}
	for _, r := range rows {
		byCode[fmt.Sprint(r["code"])] = r
	}
	for _, want := range []string{
		"gross_swing", "gross_variance", "cash_variance", "tank_over_tolerance",
		"margin_health", "overdue_receivables", "delivery_shortfall", "period_unlocked",
	} {
		r, ok := byCode[want]
		if !ok {
			t.Fatalf("seeded system rule %q missing", want)
		}
		if r["is_system"] != true {
			t.Fatalf("rule %q should be is_system", want)
		}
		if fmt.Sprint(r["mode"]) != "shadow" {
			t.Fatalf("rule %q should seed as shadow (no-regression), got %v", want, r["mode"])
		}
	}
}

func TestReportRules_CRUDRoundTrip(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	// CREATE a tenant rule.
	code, created := h.postJSON(t, "/api/v1/reports/rules", admin, `{
		"code":"custom_swing","name":"Custom swing","report_key":"sales",
		"condition":"period_over_period","severity":"warning","mode":"augment",
		"message_template":"{metric} moved {direction} {pct}%","threshold_config":{"warn_pct":5}
	}`)
	if code != http.StatusCreated {
		t.Fatalf("create: code=%d body=%v", code, created)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create: no id in %v", created)
	}

	// GET it back.
	gcode, got := h.getJSON(t, "/api/v1/reports/rules/"+id, admin)
	if gcode != http.StatusOK || fmt.Sprint(got["code"]) != "custom_swing" {
		t.Fatalf("get: code=%d body=%v", gcode, got)
	}

	// UPDATE the threshold + mode.
	ucode, _ := h.do(t, http.MethodPut, "/api/v1/reports/rules/"+id, admin,
		rawBody(`{"threshold_config":{"warn_pct":10},"severity":"critical"}`), "application/json")
	if ucode != http.StatusOK {
		t.Fatalf("update: code=%d", ucode)
	}

	// DISABLE it.
	dcode, _ := h.do(t, http.MethodPost, "/api/v1/reports/rules/"+id+"/enabled", admin,
		rawBody(`{"enabled":false}`), "application/json")
	if dcode != http.StatusOK {
		t.Fatalf("disable: code=%d", dcode)
	}
	_, after := h.getJSON(t, "/api/v1/reports/rules/"+id, admin)
	if after["enabled"] != false {
		t.Fatalf("rule should be disabled, got %v", after["enabled"])
	}

	// DELETE it (tenant rule -> allowed).
	delCode, _ := h.do(t, http.MethodDelete, "/api/v1/reports/rules/"+id, admin, nil, "")
	if delCode != http.StatusOK {
		t.Fatalf("delete: code=%d", delCode)
	}
	if gone, _ := h.getJSON(t, "/api/v1/reports/rules/"+id, admin); true {
		_ = gone
	}
	if c, _ := h.do(t, http.MethodGet, "/api/v1/reports/rules/"+id, admin, nil, ""); c != http.StatusNotFound {
		t.Fatalf("deleted rule should be 404, got %d", c)
	}
}

func TestReportRules_UnknownConditionRejected(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)
	code, _ := h.postJSON(t, "/api/v1/reports/rules", admin, `{
		"code":"bad","name":"Bad","condition":"no_such_evaluator",
		"message_template":"x"
	}`)
	if code != http.StatusBadRequest {
		t.Fatalf("unknown condition should be 400, got %d", code)
	}
}

func TestReportRules_SystemRuleCannotBeDeleted(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	// Resolve a seeded system rule id.
	var sysID uuid.UUID
	if err := h.pool.QueryRow(context.Background(),
		`SELECT id FROM report_rules WHERE tenant_id=$1 AND code='gross_swing'`, h.ids.tenantID).Scan(&sysID); err != nil {
		t.Fatalf("resolve system rule: %v", err)
	}
	code, _ := h.do(t, http.MethodDelete, "/api/v1/reports/rules/"+sysID.String(), admin, nil, "")
	if code != http.StatusConflict {
		t.Fatalf("deleting a system rule should 409, got %d", code)
	}
	// But it CAN be disabled.
	dcode, _ := h.do(t, http.MethodPost, "/api/v1/reports/rules/"+sysID.String()+"/enabled", admin,
		rawBody(`{"enabled":false}`), "application/json")
	if dcode != http.StatusOK {
		t.Fatalf("disabling a system rule should 200, got %d", dcode)
	}
}

func TestReportRules_PermissionGated(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)

	// A user with a role that does NOT hold reports.rules.manage (attendant).
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := fmt.Sprintf("noperm-%d@it.local", time.Now().UnixNano())
	var uid uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'No Perm', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, email, hash).Scan(&uid); err != nil {
		t.Fatalf("seed no-perm user: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, uid, "attendant")
	tok := h.login(t, tenantSlug, email)

	if code, _ := h.do(t, http.MethodGet, "/api/v1/reports/rules", tok, nil, ""); code != http.StatusForbidden {
		t.Fatalf("attendant GET /reports/rules should be 403, got %d", code)
	}
	if code, _ := h.postJSON(t, "/api/v1/reports/rules", tok, `{"code":"x","name":"x","condition":"period_unlocked","message_template":"x"}`); code != http.StatusForbidden {
		t.Fatalf("attendant POST /reports/rules should be 403, got %d", code)
	}
}

func TestReportRules_CrossTenantIsolation(t *testing.T) {
	hA, cleanupA := setupHarness(t)
	defer cleanupA()
	hB, cleanupB := setupHarness(t)
	defer cleanupB()
	adminA := hA.login(t, slug(hA), hA.ids.adminEmail)
	adminB := hB.login(t, slug(hB), hB.ids.adminEmail)

	// Create a rule in tenant A.
	code, created := hA.postJSON(t, "/api/v1/reports/rules", adminA, `{
		"code":"iso_rule","name":"Iso","condition":"period_unlocked",
		"report_placement":"data_quality","message_template":"x"
	}`)
	if code != http.StatusCreated {
		t.Fatalf("A create: code=%d", code)
	}
	id := fmt.Sprint(created["id"])

	// Tenant B must NOT see it (clean 404, not a leak).
	if c, _ := hB.do(t, http.MethodGet, "/api/v1/reports/rules/"+id, adminB, nil, ""); c != http.StatusNotFound {
		t.Fatalf("cross-tenant GET should be 404, got %d", c)
	}
	if c, _ := hB.do(t, http.MethodDelete, "/api/v1/reports/rules/"+id, adminB, nil, ""); c != http.StatusNotFound {
		t.Fatalf("cross-tenant DELETE should be 404, got %d", c)
	}
	// And B's list never contains A's custom code.
	_, rows := listReportRules(t, hB, adminB)
	for _, r := range rows {
		if fmt.Sprint(r["code"]) == "iso_rule" {
			t.Fatalf("tenant B leaked tenant A's rule")
		}
	}
}

func TestReportRules_AuditTrailRecorded(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	admin := h.login(t, slug(h), h.ids.adminEmail)

	code, created := h.postJSON(t, "/api/v1/reports/rules", admin, `{
		"code":"audited","name":"Audited","condition":"period_unlocked",
		"report_placement":"data_quality","message_template":"x"
	}`)
	if code != http.StatusCreated {
		t.Fatalf("create: code=%d", code)
	}
	id := fmt.Sprint(created["id"])

	var n int
	if err := h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_logs WHERE tenant_id=$1 AND action='report_rule.created' AND entity_id=$2`,
		h.ids.tenantID, id).Scan(&n); err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected one report_rule.created audit row, got %d", n)
	}
}
