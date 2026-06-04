package server_test

// DB-backed integration tests for Phase 9 — Chain & Enterprise Command.
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL.

import (
	"context"
	"encoding/json"
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
	adminID, slug, admin := h.adminContext(t, ctx)

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

	// Simulation before any policy exists: a workflow with no matching policy is
	// not required to be approved (feature 9.2).
	if code, sim := h.invPostJSON(t, "/api/v1/approval-policies/simulate", admin, map[string]any{
		"workflow_type": "central_price", "amount": "100",
	}); code != http.StatusOK || sim["approval_required"].(bool) || sim["matched"].(bool) {
		t.Fatalf("simulate (no policy) = %d %v; want not required", code, sim)
	}

	// Approval engine: a single-approval policy.
	if code, _ := h.invPostJSON(t, "/api/v1/approval-policies", admin, map[string]any{
		"workflow_type": "central_price", "min_amount": "0", "required_approvals": 1,
	}); code != http.StatusCreated {
		t.Fatalf("create policy: %d", code)
	}

	// Simulation after the policy exists: now an approval IS required, and the
	// simulated required-approvals count matches what RaiseRequest will snapshot.
	if code, sim := h.invPostJSON(t, "/api/v1/approval-policies/simulate", admin, map[string]any{
		"workflow_type": "central_price", "amount": "100",
	}); code != http.StatusOK || !sim["approval_required"].(bool) || sim["required_approvals"].(float64) != 1 {
		t.Fatalf("simulate (with policy) = %d %v; want required, 1 approval", code, sim)
	}
	// A workflow with no policy at all stays not-required even when one exists
	// for a different workflow.
	if code, sim := h.invPostJSON(t, "/api/v1/approval-policies/simulate", admin, map[string]any{
		"workflow_type": "unconfigured_workflow", "amount": "5000",
	}); code != http.StatusOK || sim["approval_required"].(bool) {
		t.Fatalf("simulate (other workflow) = %d %v; want not required", code, sim)
	}
	// workflow_type is mandatory.
	if code, _ := h.invPostJSON(t, "/api/v1/approval-policies/simulate", admin, map[string]any{"amount": "1"}); code != http.StatusBadRequest {
		t.Fatalf("simulate without workflow_type should be 400, got %d", code)
	}
	code, ar := h.invPostJSON(t, "/api/v1/approval-requests", admin, map[string]any{
		"workflow_type": "central_price", "amount": "100", "reference_type": "price_rollout",
	})
	if code != http.StatusCreated || ar["required_approvals"].(float64) != 1 || ar["status"] != "requested" {
		t.Fatalf("raise request = %d %v", code, ar)
	}
	reqID := ar["id"].(string)
	// Separation of duties: the requester cannot decide their own request.
	if code, _ := h.invPostJSON(t, "/api/v1/approval-requests/"+reqID+"/decide", admin, map[string]any{"decision": "approve"}); code != http.StatusForbidden {
		t.Fatalf("self-decide should be 403, got %d", code)
	}
	approver := h.secondApprover(t, ctx, slug)
	if code, dec := h.invPostJSON(t, "/api/v1/approval-requests/"+reqID+"/decide", approver, map[string]any{"decision": "approve"}); code != http.StatusOK || dec["status"] != "approved" {
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
	if code, dec := h.invPostJSON(t, "/api/v1/approval-requests/"+ar2["id"].(string)+"/decide", approver, map[string]any{"decision": "reject", "comment": "no"}); code != http.StatusOK || dec["status"] != "rejected" {
		t.Fatalf("reject = %d %v", code, dec)
	}
}

// TestPhase9_Dashboards covers Category B: the station-KPI projection rebuild,
// the executive overview aggregate, and station ranking.
func TestPhase9_Dashboards(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`, h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("nozzle: %v", err)
	}
	day, _ := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-05", 1000)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO revenue_days (tenant_id, station_id, operating_day_id, business_date, gross_revenue, net_revenue, cogs_total, margin_total, status)
		VALUES ($1, $2, $3, '2026-06-05', 12000, 12000, 9000, 3000, 'locked')
	`, h.ids.tenantID, h.ids.station1, day); err != nil {
		t.Fatalf("seed revenue day: %v", err)
	}

	// Rebuild the projection from posted revenue days.
	if code, rb := h.invPostJSON(t, "/api/v1/enterprise/projections/rebuild", admin, map[string]any{}); code != http.StatusOK || rb["rows"].(float64) < 1 {
		t.Fatalf("rebuild projection = %d %v", code, rb)
	}

	// Overview aggregates the network KPIs.
	code, ov := h.getJSON(t, "/api/v1/enterprise/overview?from=2026-01-01&to=2026-12-31", admin)
	if code != http.StatusOK || ov["gross_revenue"] != "12000.00" || ov["margin_total"] != "3000.00" {
		t.Fatalf("overview = %d %v", code, ov)
	}

	// Station ranking lists the revenue-bearing station first.
	code, rank := h.getJSON(t, "/api/v1/enterprise/station-ranking?from=2026-01-01&to=2026-12-31", admin)
	items, _ := rank["items"].([]any)
	if code != http.StatusOK || len(items) < 1 {
		t.Fatalf("ranking = %d %v", code, rank)
	}
	if items[0].(map[string]any)["gross_revenue"] != "12000.00" {
		t.Fatalf("top station gross = %v", items[0])
	}

	// Rebuild is idempotent (re-running does not double-count).
	if code, _ := h.invPostJSON(t, "/api/v1/enterprise/projections/rebuild", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("re-rebuild: %d", code)
	}
	if code, ov := h.getJSON(t, "/api/v1/enterprise/overview?from=2026-01-01&to=2026-12-31", admin); code != http.StatusOK || ov["gross_revenue"] != "12000.00" {
		t.Fatalf("overview after re-rebuild = %v", ov)
	}
}

// TestPhase9_CentralCommercial covers Category C: central pricing producing
// station-effective price changes, central procurement plan release, and an
// inter-station stock transfer posting paired Phase-4 movements.
func TestPhase9_CentralCommercial(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	// Central pricing: a tenant-wide PMS rollout activates to both stations.
	code, ro := h.invPostJSON(t, "/api/v1/central-price-rollouts", admin, map[string]any{
		"product_id": h.ids.pmsProduct.String(), "scope_type": "tenant", "unit_price": "3100", "effective_from": "2026-06-01",
	})
	if code != http.StatusCreated || ro["status"] != "draft" {
		t.Fatalf("create rollout = %d %v", code, ro)
	}
	roID := ro["id"].(string)
	if code, _ := h.do(t, http.MethodPost, "/api/v1/central-price-rollouts/"+roID+"/approve", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("approve rollout: %d", code)
	}
	code, act := h.do2(t, http.MethodPost, "/api/v1/central-price-rollouts/"+roID+"/activate", admin)
	if code != http.StatusOK || act["status"] != "active" || act["stations_applied"].(float64) != 2 {
		t.Fatalf("activate rollout = %d %v", code, act)
	}

	// Central procurement: a one-line plan releases.
	code, plan := h.invPostJSON(t, "/api/v1/central-procurement-plans", admin, map[string]any{
		"name": "June replenishment",
		"lines": []map[string]any{
			{"station_id": h.ids.station1.String(), "product_id": h.ids.pmsProduct.String(), "target_litres": "20000"},
		},
	})
	if code != http.StatusCreated {
		t.Fatalf("create plan = %d %v", code, plan)
	}
	if code, rel := h.do2(t, http.MethodPost, "/api/v1/central-procurement-plans/"+plan["id"].(string)+"/release", admin); code != http.StatusOK || rel["released_lines"].(float64) != 1 {
		t.Fatalf("release plan = %d %v", code, rel)
	}

	// Stock transfer: seed source stock, transfer to the other station's tank.
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO stock_movements (tenant_id, tank_id, movement_type, source_ref_type, litres, balance_after, recorded_by)
		VALUES ($1, $2, 'opening', 'opening', 20000, 20000, $3)
	`, h.ids.tenantID, h.ids.tankPMS, adminID); err != nil {
		t.Fatalf("seed opening: %v", err)
	}
	code, tr := h.invPostJSON(t, "/api/v1/stock-transfers", admin, map[string]any{
		"from_tank_id": h.ids.tankPMS.String(), "to_tank_id": h.ids.tankMSA.String(),
		"product_id": h.ids.pmsProduct.String(), "litres": "5000",
	})
	if code != http.StatusCreated {
		t.Fatalf("create transfer = %d %v", code, tr)
	}
	trID := tr["id"].(string)
	if code, _ := h.do(t, http.MethodPost, "/api/v1/stock-transfers/"+trID+"/approve", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("approve transfer: %d", code)
	}
	if code, recv := h.do2(t, http.MethodPost, "/api/v1/stock-transfers/"+trID+"/receive", admin); code != http.StatusOK || recv["status"] != "received" {
		t.Fatalf("receive transfer = %d %v", code, recv)
	}
	// Source tank balance dropped to 15,000; destination rose by 5,000.
	var fromBal, toBal float64
	_ = h.pool.QueryRow(ctx, `SELECT balance_after FROM stock_movements WHERE tenant_id=$1 AND tank_id=$2 ORDER BY seq DESC LIMIT 1`, h.ids.tenantID, h.ids.tankPMS).Scan(&fromBal)
	_ = h.pool.QueryRow(ctx, `SELECT balance_after FROM stock_movements WHERE tenant_id=$1 AND tank_id=$2 ORDER BY seq DESC LIMIT 1`, h.ids.tenantID, h.ids.tankMSA).Scan(&toBal)
	if fromBal != 15000 || toBal != 5000 {
		t.Fatalf("balances after transfer: from=%v to=%v", fromBal, toBal)
	}

	// Over-transfer is refused.
	code, tr2 := h.invPostJSON(t, "/api/v1/stock-transfers", admin, map[string]any{
		"from_tank_id": h.ids.tankPMS.String(), "to_tank_id": h.ids.tankMSA.String(),
		"product_id": h.ids.pmsProduct.String(), "litres": "999999",
	})
	if code != http.StatusCreated {
		t.Fatalf("create transfer 2: %d", code)
	}
	if code, _ := h.do(t, http.MethodPost, "/api/v1/stock-transfers/"+tr2["id"].(string)+"/approve", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("approve transfer 2: %d", code)
	}
	if code, _ := h.do(t, http.MethodPost, "/api/v1/stock-transfers/"+tr2["id"].(string)+"/receive", admin, nil, ""); code != http.StatusUnprocessableEntity {
		t.Fatalf("over-transfer receive: %d, want 422", code)
	}
}

// do2 is a POST helper returning the decoded JSON object (for nil-body POSTs).
func (h *harness) do2(t *testing.T, method, path, token string) (int, map[string]any) {
	t.Helper()
	code, raw := h.do(t, method, path, token, nil, "")
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return code, out
}

// TestPhase9_ConsolidatedFinance covers Category D: consolidated finance tying
// the Phase-7 P&L/balance sheet to a per-station revenue breakdown, and the
// station-KPI CSV export.
func TestPhase9_ConsolidatedFinance(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	if code, _ := h.invPostJSON(t, "/api/v1/accounts/seed-defaults", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("seed chart: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/accounting-periods", admin, map[string]any{"start_date": "2026-06-01", "end_date": "2026-06-30"}); code != http.StatusCreated {
		t.Fatalf("create period: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/journal-entries", admin, map[string]any{
		"entry_date": "2026-06-12", "memo": "cash sale",
		"lines": []map[string]any{
			{"system_key": "cash_on_hand", "debit": "5000", "credit": "0"},
			{"system_key": "sales_revenue", "debit": "0", "credit": "5000"},
		},
	}); code != http.StatusCreated {
		t.Fatalf("post entry: %d", code)
	}

	// Seed a revenue day and rebuild the projection for the per-station view.
	var nozzleID uuid.UUID
	_ = h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`, h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID)
	day, _ := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-12", 1000)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO revenue_days (tenant_id, station_id, operating_day_id, business_date, gross_revenue, net_revenue, cogs_total, margin_total, status)
		VALUES ($1, $2, $3, '2026-06-12', 5000, 5000, 3500, 1500, 'locked')
	`, h.ids.tenantID, h.ids.station1, day); err != nil {
		t.Fatalf("seed revenue day: %v", err)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/enterprise/projections/rebuild", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("rebuild: %d", code)
	}

	// Consolidated finance reconciles tenant P&L to the per-station breakdown.
	code, cons := h.getJSON(t, "/api/v1/enterprise/finance/consolidated?from=2026-06-01&to=2026-06-30&as_of=2026-06-30", admin)
	if code != http.StatusOK {
		t.Fatalf("consolidated = %d %v", code, cons)
	}
	is := cons["income_statement"].(map[string]any)
	if is["revenue"] != "5000.00" || is["net_profit"] != "5000.00" {
		t.Fatalf("consolidated P&L = %v", is)
	}
	if len(cons["by_station"].([]any)) < 1 {
		t.Fatalf("consolidated by_station empty: %v", cons["by_station"])
	}

	// Station-KPI export returns CSV + checksum.
	code, exp := h.getJSON(t, "/api/v1/enterprise/reports/station-kpis?from=2026-06-01&to=2026-06-30", admin)
	if code != http.StatusOK || exp["checksum"] == "" || exp["row_count"].(float64) < 1 {
		t.Fatalf("station-kpi export = %d %v", code, exp)
	}
}

// TestPhase9_ExceptionQueue covers Category E: the enterprise exception
// aggregate that feeds the command queue.
func TestPhase9_ExceptionQueue(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	// A pending approval should appear in the queue total.
	if code, _ := h.invPostJSON(t, "/api/v1/approval-requests", admin, map[string]any{"workflow_type": "central_price", "amount": "100"}); code != http.StatusCreated {
		t.Fatalf("raise request: %d", code)
	}
	code, ex := h.getJSON(t, "/api/v1/enterprise/exceptions", admin)
	if code != http.StatusOK {
		t.Fatalf("exceptions = %d %v", code, ex)
	}
	checks, _ := ex["checks"].(map[string]any)
	if checks["approvals_waiting"].(float64) < 1 || ex["total"].(float64) < 1 {
		t.Fatalf("expected at least one waiting approval in exceptions: %v", ex)
	}
}
