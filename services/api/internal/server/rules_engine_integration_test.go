package server_test

// DB-backed integration tests for the Rules & Insights Engine (Workstream D).
// They lock the RISK-001/RISK-002 fix (config-driven detection, disabling a
// rule stops its alerts) and assert per-evaluator fire/no-fire at the boundary.
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL via the shared Phase 2 harness.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/risk"
)

// seedFiringCashShortage configures an active repeated_cash_shortage rule
// (threshold 1 so a single linked shortage fires) and seeds one posted cash
// shortage attributed to adminID via the recon-line -> shift -> attendant path.
// It is the new-engine equivalent of the legacy "one cash_reconciliation
// variance raises a cash_shortage alert" fixture the Phase 10 tests relied on.
// Returns the rule id. The raised alert has alert_type 'repeated_cash_shortage'.
func seedFiringCashShortage(t *testing.T, ctx context.Context, h *harness, adminID uuid.UUID, date string) uuid.UUID {
	t.Helper()
	ruleID := insertRule(t, ctx, h, "repeated_cash_shortage", "repeated_cash_shortage",
		"cash", "high", "Attendant {attendant} {count} shifts in {days} days",
		"Review cash submissions and supervisor approvals.", "1", 30, true, "active")

	var nozzleID uuid.UUID
	_ = h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`, h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID)
	day, shift := seedClosedDayShift(t, ctx, h, adminID, nozzleID, date, 1000)
	if _, err := h.pool.Exec(ctx, `INSERT INTO shift_attendants (tenant_id, shift_id, user_id, assigned_by) VALUES ($1, $2, $3, $3) ON CONFLICT DO NOTHING`, h.ids.tenantID, shift, adminID); err != nil {
		t.Fatalf("attendant: %v", err)
	}
	var reconID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO cash_reconciliations (tenant_id, station_id, operating_day_id, expected_cash, counted_cash, variance, status, created_by)
		VALUES ($1, $2, $3, 50000, 49500, -500, 'posted', $4) RETURNING id
	`, h.ids.tenantID, h.ids.station1, day, adminID).Scan(&reconID); err != nil {
		t.Fatalf("seed cash recon: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO cash_reconciliation_lines (tenant_id, cash_reconciliation_id, shift_id, expected_cash)
		VALUES ($1, $2, $3, 50000)
	`, h.ids.tenantID, reconID, shift); err != nil {
		t.Fatalf("recon line: %v", err)
	}
	return ruleID
}

// insertRule inserts a configured rule directly and returns its id.
func insertRule(t *testing.T, ctx context.Context, h *harness, code, condition, category, severity, msg, action, threshold string, comparison int, enabled bool, status string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO risk_rules
		    (tenant_id, code, name, rule_type, status, category, condition,
		     threshold, lookback_days, comparison_period_days, severity,
		     message_template, recommended_action, enabled)
		VALUES ($1, $2, $2, 'threshold', $11, $3, $4,
		        NULLIF($5,'')::numeric, 30, $6, $7, $8, $9, $10)
		RETURNING id
	`, h.ids.tenantID, code, category, condition, threshold, comparison, severity, msg, action, enabled, status).Scan(&id); err != nil {
		t.Fatalf("insert rule %s: %v", code, err)
	}
	return id
}

// runDetect runs detection in a tx and returns the count of new alerts.
func runDetect(t *testing.T, ctx context.Context, h *harness, tenantID uuid.UUID) int {
	t.Helper()
	r := risk.New(h.pool)
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	n, err := r.RunDetection(ctx, tx, tenantID)
	if err != nil {
		t.Fatalf("RunDetection: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return n
}

func openAlertCount(t *testing.T, ctx context.Context, h *harness, alertType string) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*) FROM risk_alerts
		WHERE tenant_id = $1 AND alert_type = $2
		  AND status IN ('open','acknowledged','investigating','escalated')
	`, h.ids.tenantID, alertType).Scan(&n); err != nil {
		t.Fatalf("count alerts: %v", err)
	}
	return n
}

// seedTankReconException seeds an exception reconciliation with the given
// variance for tankPMS on a closed operating day. Returns the recon id. The
// day is closed so multiple recons (different dates) can coexist without
// tripping the one-active-day-per-station unique index.
func seedTankReconException(t *testing.T, ctx context.Context, h *harness, openedBy uuid.UUID, variance float64, date string) uuid.UUID {
	t.Helper()
	var dayID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO operating_days (tenant_id, station_id, business_date, opened_by, status)
		VALUES ($1, $2, $3, $4, 'closed') RETURNING id
	`, h.ids.tenantID, h.ids.station1, date, openedBy).Scan(&dayID); err != nil {
		t.Fatalf("seed day: %v", err)
	}
	var id uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO tank_reconciliations
		    (tenant_id, tank_id, operating_day_id, opening_book, deliveries_total,
		     sales_total, adjustments_total, closing_book, closing_physical,
		     variance_litres, variance_percent, tolerance_percent, status)
		VALUES ($1, $2, $3, 10000, 0, 0, 0, 10000, 10000 + $4, $4, 0, 1.0, 'exception')
		RETURNING id
	`, h.ids.tenantID, h.ids.tankPMS, dayID, variance).Scan(&id); err != nil {
		t.Fatalf("seed recon: %v", err)
	}
	return id
}

// TestRulesEngine_DisabledRuleProducesNoAlerts locks the RISK-002 fix: a rule
// that is disabled (or paused) must not raise alerts even when its source facts
// would otherwise fire. Re-enabling it then fires.
func TestRulesEngine_DisabledRuleProducesNoAlerts(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, _ := h.adminContext(t, ctx)

	// A 500 L variance with no per-product tolerance => would fire.
	seedTankReconException(t, ctx, h, adminID, 500, "2026-05-01")

	// Disabled rule: no alerts.
	id := insertRule(t, ctx, h, "fuel_variance_over_tolerance", "fuel_variance_over_tolerance",
		"inventory", "high", "{product} variance {variance_litres} L", "Review.", "", 0, false, "active")
	if n := runDetect(t, ctx, h, h.ids.tenantID); n != 0 {
		t.Fatalf("disabled rule created %d alerts, want 0", n)
	}
	if c := openAlertCount(t, ctx, h, "fuel_variance_over_tolerance"); c != 0 {
		t.Fatalf("disabled rule left %d open alerts, want 0", c)
	}

	// Paused (but enabled) rule: still no alerts — status gate also applies.
	if _, err := h.pool.Exec(ctx, `UPDATE risk_rules SET enabled = true, status = 'paused' WHERE id = $1`, id); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if n := runDetect(t, ctx, h, h.ids.tenantID); n != 0 {
		t.Fatalf("paused rule created %d alerts, want 0", n)
	}

	// Enable + active: now it fires exactly one alert, rendered + linked.
	if _, err := h.pool.Exec(ctx, `UPDATE risk_rules SET enabled = true, status = 'active' WHERE id = $1`, id); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if n := runDetect(t, ctx, h, h.ids.tenantID); n != 1 {
		t.Fatalf("enabled rule created %d alerts, want 1", n)
	}
	var detail, action string
	var ruleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		SELECT detail, recommended_action, rule_id FROM risk_alerts
		WHERE tenant_id = $1 AND alert_type = 'fuel_variance_over_tolerance' LIMIT 1
	`, h.ids.tenantID).Scan(&detail, &action, &ruleID); err != nil {
		t.Fatalf("read alert: %v", err)
	}
	if ruleID != id {
		t.Fatalf("alert rule_id = %s, want %s", ruleID, id)
	}
	if action != "Review." {
		t.Fatalf("recommended_action = %q", action)
	}
	if detail == "" || detail == "{product} variance {variance_litres} L" {
		t.Fatalf("template not rendered: %q", detail)
	}
}

// TestRulesEngine_FuelVarianceBoundary asserts the fuel variance evaluator fires
// above the per-product tolerance but not at/under it.
func TestRulesEngine_FuelVarianceBoundary(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, _ := h.adminContext(t, ctx)

	// Set a 1% loss tolerance on PMS => tolerance litres = closing_book(10000)*1% = 100.
	if _, err := h.pool.Exec(ctx, `UPDATE products SET loss_tolerance_percent = 1.0 WHERE id = $1`, h.ids.pmsProduct); err != nil {
		t.Fatalf("set tolerance: %v", err)
	}
	insertRule(t, ctx, h, "fuel_variance_over_tolerance", "fuel_variance_over_tolerance",
		"inventory", "high", "{product} variance {variance_litres} L", "Review.", "", 0, true, "active")

	// At tolerance (exactly 100) => no fire (strict >).
	seedTankReconException(t, ctx, h, adminID, 100, "2026-05-01")
	if n := runDetect(t, ctx, h, h.ids.tenantID); n != 0 {
		t.Fatalf("variance == tolerance fired %d, want 0", n)
	}
	// Above tolerance (150) => fire.
	seedTankReconException(t, ctx, h, adminID, 150, "2026-05-02")
	if n := runDetect(t, ctx, h, h.ids.tenantID); n != 1 {
		t.Fatalf("variance > tolerance fired %d, want 1", n)
	}
}

// TestRulesEngine_RepeatedCashShortageBoundary asserts the evaluator fires on
// the 3rd shortage within the window but not on the 2nd.
func TestRulesEngine_RepeatedCashShortageBoundary(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, _ := h.adminContext(t, ctx)

	insertRule(t, ctx, h, "repeated_cash_shortage", "repeated_cash_shortage",
		"cash", "high", "Attendant {attendant} {count} shifts in {days} days", "Review.", "3", 7, true, "active")

	// Each helper builds operating day + shift; attribute attendant via
	// shift_attendants and tie a posted shortage recon to that shift.
	mkShortage := func(date string, amount float64) {
		var nozzleID uuid.UUID
		_ = h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`, h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID)
		day, shift := seedClosedDayShift(t, ctx, h, adminID, nozzleID, date, 1000)
		if _, err := h.pool.Exec(ctx, `INSERT INTO shift_attendants (tenant_id, shift_id, user_id, assigned_by) VALUES ($1, $2, $3, $3) ON CONFLICT DO NOTHING`, h.ids.tenantID, shift, adminID); err != nil {
			t.Fatalf("attendant: %v", err)
		}
		var reconID uuid.UUID
		if err := h.pool.QueryRow(ctx, `
			INSERT INTO cash_reconciliations (tenant_id, station_id, operating_day_id, expected_cash, counted_cash, variance, status, created_by)
			VALUES ($1, $2, $3, 50000, 50000 - $4, -$4, 'posted', $5) RETURNING id
		`, h.ids.tenantID, h.ids.station1, day, amount, adminID).Scan(&reconID); err != nil {
			t.Fatalf("recon: %v", err)
		}
		if _, err := h.pool.Exec(ctx, `
			INSERT INTO cash_reconciliation_lines (tenant_id, cash_reconciliation_id, shift_id, expected_cash)
			VALUES ($1, $2, $3, 50000)
		`, h.ids.tenantID, reconID, shift); err != nil {
			t.Fatalf("recon line: %v", err)
		}
	}

	today := time.Now()
	mkShortage(today.AddDate(0, 0, -1).Format("2006-01-02"), 300)
	mkShortage(today.AddDate(0, 0, -2).Format("2006-01-02"), 400)
	// 2 shortages => below threshold of 3 => no fire.
	if n := runDetect(t, ctx, h, h.ids.tenantID); n != 0 {
		t.Fatalf("2 shortages fired %d, want 0", n)
	}
	// 3rd shortage within window => fire.
	mkShortage(today.AddDate(0, 0, -3).Format("2006-01-02"), 500)
	if n := runDetect(t, ctx, h, h.ids.tenantID); n != 1 {
		t.Fatalf("3 shortages fired %d, want 1", n)
	}
}

// TestRulesEngine_SupplierDeliveryShortageBoundary asserts the procurement
// evaluator fires only when received falls short by more than the tolerance
// fraction.
func TestRulesEngine_SupplierDeliveryShortageBoundary(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, _ := h.adminContext(t, ctx)

	// threshold = 0.02 (2% tolerance fraction).
	insertRule(t, ctx, h, "supplier_delivery_shortage", "supplier_delivery_shortage",
		"procurement", "high", "Supplier {supplier} short {shortage_litres} L", "Dispute.", "0.02", 0, true, "active")

	var supplierID, poID uuid.UUID
	if err := h.pool.QueryRow(ctx, `INSERT INTO suppliers (tenant_id, code, name) VALUES ($1, 'SUP1', 'Acme Fuels') RETURNING id`, h.ids.tenantID).Scan(&supplierID); err != nil {
		t.Fatalf("supplier: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO purchase_orders (tenant_id, station_id, supplier_id, status, raised_by)
		VALUES ($1, $2, $3, 'received', $4) RETURNING id
	`, h.ids.tenantID, h.ids.station1, supplierID, adminID).Scan(&poID); err != nil {
		t.Fatalf("po: %v", err)
	}
	// 1% short (within 2% tolerance) => no fire.
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO purchase_order_lines (tenant_id, purchase_order_id, product_id, ordered_litres, unit_price, received_litres)
		VALUES ($1, $2, $3, 10000, 2950, 9900)
	`, h.ids.tenantID, poID, h.ids.pmsProduct); err != nil {
		t.Fatalf("po line within tol: %v", err)
	}
	if n := runDetect(t, ctx, h, h.ids.tenantID); n != 0 {
		t.Fatalf("1%% short fired %d, want 0", n)
	}
	// 5% short (over 2% tolerance) => fire.
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO purchase_order_lines (tenant_id, purchase_order_id, product_id, ordered_litres, unit_price, received_litres)
		VALUES ($1, $2, $3, 10000, 2820, 9500)
	`, h.ids.tenantID, poID, h.ids.agoProduct); err != nil {
		t.Fatalf("po line over tol: %v", err)
	}
	if n := runDetect(t, ctx, h, h.ids.tenantID); n != 1 {
		t.Fatalf("5%% short fired %d, want 1", n)
	}
}

// TestRulesEngine_StockoutCoverageBoundary asserts coverage fires below the
// threshold of days-of-cover but not at/above it.
func TestRulesEngine_StockoutCoverageBoundary(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, _ := h.adminContext(t, ctx)

	// threshold = 2 coverage-days, window = 10 days.
	insertRule(t, ctx, h, "stockout_coverage", "stockout_coverage",
		"inventory", "medium", "{product} ~{hours}h to min", "Order.", "2", 10, true, "active")

	// On-hand opening, then sales draw over the window. Average daily sales =
	// total sold / 10. Pick coverage just under 2 days for tankPMS and >= 2 for
	// tankAGO.
	now := time.Now()
	// tankPMS: on-hand 300, sold 2000 over 10d => 200/day => coverage 1.5d (<2 fires).
	mustMovement(t, ctx, h, adminID, h.ids.tankPMS, "opening", 2300, now.AddDate(0, 0, -10))
	mustMovement(t, ctx, h, adminID, h.ids.tankPMS, "sales", -2000, now.AddDate(0, 0, -5))
	// tankAGO: on-hand 600, sold 2000 over 10d => 200/day => coverage 3d (>=2 no fire).
	mustMovement(t, ctx, h, adminID, h.ids.tankAGO, "opening", 2600, now.AddDate(0, 0, -10))
	mustMovement(t, ctx, h, adminID, h.ids.tankAGO, "sales", -2000, now.AddDate(0, 0, -5))

	if n := runDetect(t, ctx, h, h.ids.tenantID); n != 1 {
		t.Fatalf("stockout fired %d, want 1 (only the low-cover tank)", n)
	}
}

// mustMovement appends a stock ledger movement (balance_after is a per-row
// snapshot; the evaluator sums litres so the snapshot value is not load-bearing).
func mustMovement(t *testing.T, ctx context.Context, h *harness, by, tankID uuid.UUID, mtype string, litres float64, at time.Time) {
	t.Helper()
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO stock_movements (tenant_id, tank_id, movement_type, litres, balance_after, recorded_by, recorded_at)
		VALUES ($1, $2, $3, $4, 0, $5, $6)
	`, h.ids.tenantID, tankID, mtype, litres, by, at); err != nil {
		t.Fatalf("movement: %v", err)
	}
}
