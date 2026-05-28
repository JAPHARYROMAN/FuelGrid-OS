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
