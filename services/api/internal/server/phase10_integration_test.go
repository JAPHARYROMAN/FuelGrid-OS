package server_test

// DB-backed integration tests for Phase 10 — Risk, Fraud & Intelligence.
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL.

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestPhase10_SignalsRulesDetection covers Categories A-B: signal backfill, the
// rule registry, the cash-shortage detection pack raising an idempotent alert,
// and the alert lifecycle.
func TestPhase10_SignalsRulesDetection(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	// Seed a posted cash reconciliation with a 500 shortfall.
	var nozzleID uuid.UUID
	_ = h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`, h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID)
	day, _ := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-05", 1000)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO cash_reconciliations (tenant_id, station_id, operating_day_id, expected_cash, counted_cash, variance, status, created_by)
		VALUES ($1, $2, $3, 50000, 49500, -500, 'posted', $4)
	`, h.ids.tenantID, h.ids.station1, day, adminID); err != nil {
		t.Fatalf("seed cash recon: %v", err)
	}

	// Backfill derives signals from source facts.
	if code, bf := h.invPostJSON(t, "/api/v1/risk/signals/backfill", admin, map[string]any{}); code != http.StatusOK || bf["created"].(float64) < 1 {
		t.Fatalf("backfill = %d %v", code, bf)
	}
	if code, sig := h.getJSON(t, "/api/v1/risk/signals?type=cash_variance", admin); code != http.StatusOK || sig["count"].(float64) < 1 {
		t.Fatalf("signals = %d %v", code, sig)
	}

	// Rule registry.
	if code, _ := h.invPostJSON(t, "/api/v1/risk/rules", admin, map[string]any{
		"code": "cash_short", "name": "Cash shortage", "rule_type": "threshold", "severity": "medium",
	}); code != http.StatusCreated {
		t.Fatalf("create rule: %d", code)
	}
	if code, rules := h.getJSON(t, "/api/v1/risk/rules", admin); code != http.StatusOK || rules["count"].(float64) < 1 {
		t.Fatalf("rules = %d %v", code, rules)
	}

	// Detection raises a cash_shortage alert.
	code, det := h.invPostJSON(t, "/api/v1/risk/detect", admin, map[string]any{})
	if code != http.StatusOK || det["alerts_created"].(float64) < 1 {
		t.Fatalf("detect = %d %v", code, det)
	}
	code, alerts := h.getJSON(t, "/api/v1/risk/alerts?type=cash_shortage", admin)
	items, _ := alerts["items"].([]any)
	if code != http.StatusOK || len(items) != 1 {
		t.Fatalf("alerts = %d %v", code, alerts)
	}
	alertID := items[0].(map[string]any)["id"].(string)

	// Detection is idempotent while the alert is open.
	if code, det := h.invPostJSON(t, "/api/v1/risk/detect", admin, map[string]any{}); code != http.StatusOK || det["alerts_created"].(float64) != 0 {
		t.Fatalf("re-detect should create 0: %v", det)
	}

	// Alert lifecycle: acknowledge then resolve with a disposition.
	if code, _ := h.invPostJSON(t, "/api/v1/risk/alerts/"+alertID+"/acknowledge", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("acknowledge: %d", code)
	}
	if code, res := h.invPostJSON(t, "/api/v1/risk/alerts/"+alertID+"/resolve", admin, map[string]any{"disposition": "data_entry_error"}); code != http.StatusOK || res["status"] != "resolved" {
		t.Fatalf("resolve = %d %v", code, res)
	}
}

// TestPhase10_ScoringDashboard covers Category C: station risk scoring from
// open alerts and the risk overview dashboard.
func TestPhase10_ScoringDashboard(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	var nozzleID uuid.UUID
	_ = h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`, h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID)
	day, _ := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-05", 1000)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO cash_reconciliations (tenant_id, station_id, operating_day_id, expected_cash, counted_cash, variance, status, created_by)
		VALUES ($1, $2, $3, 50000, 49500, -500, 'posted', $4)
	`, h.ids.tenantID, h.ids.station1, day, adminID); err != nil {
		t.Fatalf("seed cash recon: %v", err)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/risk/detect", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("detect: %d", code)
	}

	// Recompute station scores from open alerts.
	if code, sc := h.invPostJSON(t, "/api/v1/risk/scores/recompute", admin, map[string]any{}); code != http.StatusOK || sc["scored_stations"].(float64) < 1 {
		t.Fatalf("recompute = %d %v", code, sc)
	}

	// The overview reports open alerts by severity and top stations.
	code, ov := h.getJSON(t, "/api/v1/risk/overview", admin)
	if code != http.StatusOK || ov["open_total"].(float64) < 1 {
		t.Fatalf("overview = %d %v", code, ov)
	}
	if len(ov["top_stations"].([]any)) < 1 {
		t.Fatalf("expected a top scored station: %v", ov["top_stations"])
	}
	top := ov["top_stations"].([]any)[0].(map[string]any)
	if top["score"].(float64) < 1 || top["band"] == "" {
		t.Fatalf("top station score = %v", top)
	}
}

// TestPhase10_Investigations covers Category D: escalating an alert into a
// case, building its timeline (linked alert + comment + action), and closing
// with a resolution.
func TestPhase10_Investigations(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	var nozzleID uuid.UUID
	_ = h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`, h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID)
	day, _ := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-05", 1000)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO cash_reconciliations (tenant_id, station_id, operating_day_id, expected_cash, counted_cash, variance, status, created_by)
		VALUES ($1, $2, $3, 50000, 49500, -500, 'posted', $4)
	`, h.ids.tenantID, h.ids.station1, day, adminID); err != nil {
		t.Fatalf("seed cash recon: %v", err)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/risk/detect", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("detect: %d", code)
	}
	code, alerts := h.getJSON(t, "/api/v1/risk/alerts?type=cash_shortage", admin)
	alertID := alerts["items"].([]any)[0].(map[string]any)["id"].(string)
	if code != http.StatusOK {
		t.Fatalf("alerts: %d", code)
	}

	// Open a case linked to the alert.
	code, c := h.invPostJSON(t, "/api/v1/investigations", admin, map[string]any{
		"title": "Repeated cash shortage", "case_type": "cash_shortage", "severity": "high", "alert_id": alertID,
	})
	if code != http.StatusCreated || c["status"] != "open" {
		t.Fatalf("create case = %d %v", code, c)
	}
	caseID := c["id"].(string)

	// Add a comment and a recommended action; complete the action.
	if code, _ := h.invPostJSON(t, "/api/v1/investigations/"+caseID+"/comments", admin, map[string]any{"body": "Supervisor recount requested"}); code != http.StatusCreated {
		t.Fatalf("comment: %d", code)
	}
	code, act := h.invPostJSON(t, "/api/v1/investigations/"+caseID+"/actions", admin, map[string]any{"action_type": "request_recount", "detail": "count drawer"})
	if code != http.StatusCreated {
		t.Fatalf("action: %d", code)
	}
	actionID := act["id"].(string)
	if code, _ := h.invPostJSON(t, "/api/v1/investigations/"+caseID+"/actions/"+actionID+"/status", admin, map[string]any{"status": "completed"}); code != http.StatusOK {
		t.Fatalf("complete action: %d", code)
	}

	// The timeline reconstructs the case-scoped events.
	code, tl := h.getJSON(t, "/api/v1/investigations/"+caseID, admin)
	if code != http.StatusOK || len(tl["timeline"].([]any)) < 3 {
		t.Fatalf("timeline = %d %v", code, tl)
	}

	// Resolve the case with a disposition.
	if code, res := h.invPostJSON(t, "/api/v1/investigations/"+caseID+"/status", admin, map[string]any{"status": "resolved", "resolution": "training issued"}); code != http.StatusOK || res["status"] != "resolved" {
		t.Fatalf("resolve case = %d %v", code, res)
	}
}

// TestPhase10_Governance covers Category E: alert suppression silencing
// detection, rule tuning, the governance summary, and the engine pause switch.
func TestPhase10_Governance(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	var nozzleID uuid.UUID
	_ = h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`, h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID)
	day, _ := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-05", 1000)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO cash_reconciliations (tenant_id, station_id, operating_day_id, expected_cash, counted_cash, variance, status, created_by)
		VALUES ($1, $2, $3, 50000, 49500, -500, 'posted', $4)
	`, h.ids.tenantID, h.ids.station1, day, adminID); err != nil {
		t.Fatalf("seed cash recon: %v", err)
	}

	// Suppress cash_shortage before detecting; the alert must not be raised.
	if code, _ := h.invPostJSON(t, "/api/v1/risk/suppressions", admin, map[string]any{
		"alert_type": "cash_shortage", "reason": "known cash pickup timing",
	}); code != http.StatusCreated {
		t.Fatalf("create suppression: %d", code)
	}
	if code, det := h.invPostJSON(t, "/api/v1/risk/detect", admin, map[string]any{}); code != http.StatusOK || det["alerts_created"].(float64) != 0 {
		t.Fatalf("detect should be suppressed: %v", det)
	}
	if code, alerts := h.getJSON(t, "/api/v1/risk/alerts?type=cash_shortage", admin); code != http.StatusOK || len(alerts["items"].([]any)) != 0 {
		t.Fatalf("expected no cash_shortage alerts: %v", alerts)
	}

	// Rule tuning.
	code, rule := h.invPostJSON(t, "/api/v1/risk/rules", admin, map[string]any{"code": "cs", "name": "Cash short"})
	if code != http.StatusCreated {
		t.Fatalf("create rule: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/risk/rules/"+rule["id"].(string)+"/tune", admin, map[string]any{"threshold": "1000", "lookback_days": 14, "severity": "high"}); code != http.StatusOK {
		t.Fatalf("tune rule: %d", code)
	}

	// Governance summary + engine pause.
	code, gov := h.getJSON(t, "/api/v1/risk/governance", admin)
	if code != http.StatusOK || gov["active_suppressions"].(float64) < 1 {
		t.Fatalf("governance = %d %v", code, gov)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/risk/engine/pause", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("pause engine: %d", code)
	}
}

// TestPhase10_DetectionHonorsRuleConfig proves the risk-engine Critical fix:
// detection now honors a rule's configured threshold and severity, and pausing
// a rule stops its pack. Previously RunDetection ran hardcoded packs regardless
// of rule status or params — "pause" was a no-op and tuning was ignored.
func TestPhase10_DetectionHonorsRuleConfig(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	var nozzleID uuid.UUID
	_ = h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`, h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID)
	day, _ := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-07", 1000)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO cash_reconciliations (tenant_id, station_id, operating_day_id, expected_cash, counted_cash, variance, status, created_by)
		VALUES ($1, $2, $3, 50000, 49500, -500, 'posted', $4)
	`, h.ids.tenantID, h.ids.station1, day, adminID); err != nil {
		t.Fatalf("seed cash recon: %v", err)
	}

	// Configure + activate a cash_shortage rule with a 1000 threshold. The 500
	// shortage is below it, so detection must raise nothing — the configured
	// threshold is honored (the hardcoded pack used to fire on any shortage).
	code, rule := h.invPostJSON(t, "/api/v1/risk/rules", admin, map[string]any{
		"code": "cash_shortage", "name": "Cash shortage", "rule_type": "threshold",
		"severity": "high", "threshold": "1000", "lookback_days": 30,
	})
	if code != http.StatusCreated {
		t.Fatalf("create rule: %d %v", code, rule)
	}
	ruleID := rule["id"].(string)
	if code, _ := h.invPostJSON(t, "/api/v1/risk/rules/"+ruleID+"/status", admin, map[string]any{"status": "active"}); code != http.StatusOK {
		t.Fatalf("activate rule: %d", code)
	}
	if code, det := h.invPostJSON(t, "/api/v1/risk/detect", admin, map[string]any{}); code != http.StatusOK || det["alerts_created"].(float64) != 0 {
		t.Fatalf("detect with threshold 1000 must create 0 (500 < 1000): %v", det)
	}

	// Tune the threshold below the shortage; it now fires, at the rule's severity.
	if code, _ := h.invPostJSON(t, "/api/v1/risk/rules/"+ruleID+"/tune", admin,
		map[string]any{"threshold": "100", "lookback_days": 30, "severity": "high"}); code != http.StatusOK {
		t.Fatalf("tune: %d", code)
	}
	if code, det := h.invPostJSON(t, "/api/v1/risk/detect", admin, map[string]any{}); code != http.StatusOK || det["alerts_created"].(float64) != 1 {
		t.Fatalf("detect with threshold 100 must create 1: %v", det)
	}
	// The alert carries the rule's severity and the derived score.
	var alertID, sev string
	var score int
	if err := h.pool.QueryRow(ctx, `
		SELECT id, severity, score FROM risk_alerts
		WHERE tenant_id = $1 AND alert_type = 'cash_shortage' AND status NOT IN ('resolved','dismissed')
		ORDER BY created_at DESC LIMIT 1
	`, h.ids.tenantID).Scan(&alertID, &sev, &score); err != nil {
		t.Fatalf("read alert: %v", err)
	}
	if sev != "high" || score != 75 {
		t.Fatalf("alert severity/score = %s/%d, want high/75 (from the rule)", sev, score)
	}

	// Resolve it, then pause the rule. Detection must not re-raise it — a
	// resolved alert no longer blocks a fresh insert, so only the pause prevents
	// it. Pause is no longer a no-op.
	if code, _ := h.invPostJSON(t, "/api/v1/risk/alerts/"+alertID+"/resolve", admin, map[string]any{"disposition": "confirmed"}); code != http.StatusOK {
		t.Fatalf("resolve: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/risk/rules/"+ruleID+"/status", admin, map[string]any{"status": "paused"}); code != http.StatusOK {
		t.Fatalf("pause rule: %d", code)
	}
	if code, det := h.invPostJSON(t, "/api/v1/risk/detect", admin, map[string]any{}); code != http.StatusOK || det["alerts_created"].(float64) != 0 {
		t.Fatalf("detect while paused must create 0: %v", det)
	}
}
