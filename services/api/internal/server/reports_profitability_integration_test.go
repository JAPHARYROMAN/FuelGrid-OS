package server_test

// DB-backed integration tests for the profitability (10.4) and station-comparison
// (10.6) structured reports. Reuses the Phase 2 harness; gated on
// TEST_DATABASE_URL + TEST_REDIS_URL.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5433/fuelgrid?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6379/0 \
//	go test ./services/api/internal/server -run ReportsProfitability -v
//
// They assert:
//
//	(a) profitability totals are summed correctly in SQL (net revenue, COGS,
//	    gross margin = net − COGS, expenses, net operating = margin − expenses),
//	    net of an approved sale void;
//	(b) the station-comparison report respects the actor's station access — the
//	    station-restricted operator only sees their granted station, while the
//	    tenant-wide admin sees every station; and
//	(c) a freshly-created attendant (no revenue.read) is forbidden both reports.

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// seedSale inserts one recognized sale (with its own operating_day + shift) for a
// station on today's business date, with the supplied net + cogs amounts. Returns
// the sale id so the caller can void it. All money is passed as decimal literals
// so the SQL ::numeric sums are exact.
func seedProfitSale(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, stationID, tankID, productID, adminID uuid.UUID, gross, tax, net, cogs string, litres string) uuid.UUID {
	t.Helper()
	// Reuse the station's active day for today if one already exists — only one
	// non-locked operating_day is allowed per station/date (idx_operating_days_active).
	var dayID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT id FROM operating_days
		WHERE tenant_id = $1 AND station_id = $2 AND business_date = CURRENT_DATE AND status <> 'locked'
		LIMIT 1`, tenantID, stationID).Scan(&dayID); err != nil {
		if err := pool.QueryRow(ctx, `
			INSERT INTO operating_days (tenant_id, station_id, business_date, status, opened_by)
			VALUES ($1, $2, CURRENT_DATE, 'open', $3) RETURNING id`,
			tenantID, stationID, adminID).Scan(&dayID); err != nil {
			t.Fatalf("seed operating day: %v", err)
		}
	}
	var shiftID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO shifts (tenant_id, station_id, operating_day_id, name, status, opened_by)
		VALUES ($1, $2, $3, 'Day', 'approved', $4) RETURNING id`,
		tenantID, stationID, dayID, adminID).Scan(&shiftID); err != nil {
		t.Fatalf("seed shift: %v", err)
	}
	// A dedicated pump + nozzle for this sale. Each gets a unique pump number so
	// the (station_id, number) pump index and the partial-unique (pump_id, number)
	// nozzle index never collide across repeated seeds within a test.
	num := nextSeedNum()
	var pumpID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO pumps (tenant_id, station_id, number, name) VALUES ($1, $2, $3, 'PX') RETURNING id`,
		tenantID, stationID, num).Scan(&pumpID); err != nil {
		t.Fatalf("seed pump: %v", err)
	}
	var nozzleID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO nozzles (tenant_id, station_id, pump_id, tank_id, product_id, number, default_price)
		VALUES ($1, $2, $3, $4, $5, 1, 2950.00) RETURNING id`,
		tenantID, stationID, pumpID, tankID, productID).Scan(&nozzleID); err != nil {
		t.Fatalf("seed nozzle: %v", err)
	}
	var saleID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO sales
		    (tenant_id, shift_id, station_id, operating_day_id, nozzle_id, product_id, tank_id,
		     litres, unit_price, gross_amount, tax_rate, tax_amount, net_amount, cogs_amount, margin_amount, recorded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, 2950.00, $9::numeric, 18.00, $10::numeric, $11::numeric, $12::numeric, ($11::numeric - $12::numeric), $13)
		RETURNING id`,
		tenantID, shiftID, stationID, dayID, nozzleID, productID, tankID,
		litres, gross, tax, net, cogs, adminID).Scan(&saleID); err != nil {
		t.Fatalf("seed sale: %v", err)
	}
	return saleID
}

// seedNum hands out monotonically increasing numbers so seeded pumps/nozzles
// never collide on their (station, number) / (pump, number) unique indexes.
var seedNumCounter atomic.Int64

func nextSeedNum() int { return int(seedNumCounter.Add(1)) + 1000 }

// seedExpense inserts a posted operating expense booked to a station on today.
func seedExpense(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, stationID, adminID uuid.UUID, amount string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO expenses (tenant_id, station_id, expense_date, amount, account_key, payment_mode, status, created_by)
		VALUES ($1, $2, CURRENT_DATE, $3::numeric, 'operating_expense', 'cash', 'posted', $4)`,
		tenantID, stationID, amount, adminID); err != nil {
		t.Fatalf("seed expense: %v", err)
	}
}

func adminUserID(t *testing.T, ctx context.Context, pool *database.Pool, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT u.id FROM users u
		JOIN user_roles ur ON ur.user_id = u.id AND ur.tenant_id = u.tenant_id
		JOIN roles r ON r.id = ur.role_id
		WHERE u.tenant_id = $1 AND r.code = 'system_admin' ORDER BY u.created_at LIMIT 1`,
		tenantID).Scan(&id); err != nil {
		t.Fatalf("lookup admin: %v", err)
	}
	return id
}

func summaryValue(m map[string]any, label string) string {
	arr, ok := m["summary"].([]any)
	if !ok {
		return ""
	}
	for _, it := range arr {
		row, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if row["label"] == label {
			if v, ok := row["value"].(string); ok {
				return v
			}
		}
	}
	return ""
}

func TestReportsProfitability_TotalsAndScope(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	adminID := adminUserID(t, ctx, h.pool, h.ids.tenantID)

	// Station1: two sales (net 1000+500, cogs 700+300) plus an approved void of
	// the second sale, so the report nets to net=1000, cogs=700, margin=300.
	// One posted expense of 100 -> net operating = 300 - 100 = 200.
	seedProfitSale(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, h.ids.tankPMS, h.ids.pmsProduct, adminID,
		"1180", "180", "1000", "700", "400.000")
	voided := seedProfitSale(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, h.ids.tankPMS, h.ids.pmsProduct, adminID,
		"590", "90", "500", "300", "200.000")
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO sale_voids (tenant_id, sale_id, status, reason, requested_by, reversal_litres, reversal_gross, reversal_tax, reversal_net, reversal_cogs, reversal_margin)
		VALUES ($1, $2, 'approved', 'test void', $3, -200.000, -590, -90, -500, -300, -200)`,
		h.ids.tenantID, voided, adminID); err != nil {
		t.Fatalf("seed approved void: %v", err)
	}
	seedExpense(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, adminID, "100")

	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	code, body := h.getJSON(t, "/api/v1/reports/profitability?station_id="+h.ids.station1.String()+"&period=this-month", admin)
	if code != http.StatusOK {
		t.Fatalf("profitability report = %d, want 200 (%v)", code, body)
	}
	if got := summaryValue(body, "Net revenue"); got != "1000.00" {
		t.Fatalf("net revenue = %q, want 1000.00", got)
	}
	if got := summaryValue(body, "COGS"); got != "700.00" {
		t.Fatalf("cogs = %q, want 700.00", got)
	}
	if got := summaryValue(body, "Gross margin"); got != "300.00" {
		t.Fatalf("gross margin = %q, want 300.00 (net − cogs)", got)
	}
	if got := summaryValue(body, "Operating expenses"); got != "100.00" {
		t.Fatalf("expenses = %q, want 100.00", got)
	}
	if got := summaryValue(body, "Net operating result"); got != "200.00" {
		t.Fatalf("net operating = %q, want 200.00 (margin − expenses)", got)
	}
	if got := summaryValue(body, "Litres sold"); got != "400.000" {
		t.Fatalf("litres = %q, want 400.000 (600 sold − 200 voided)", got)
	}

	// A freshly-created attendant holds no revenue.read: the report is 403.
	att := freshAttendant(t, ctx, h, tenantSlug)
	if code, _ := h.getJSON(t, "/api/v1/reports/profitability?station_id="+h.ids.station1.String(), att); code != http.StatusForbidden {
		t.Fatalf("attendant profitability report = %d, want 403", code)
	}
}

func TestReportsStationComparison_RespectsScope(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	adminID := adminUserID(t, ctx, h.pool, h.ids.tenantID)

	// Sales on BOTH stations so each has a comparison row when visible.
	seedProfitSale(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, h.ids.tankPMS, h.ids.pmsProduct, adminID,
		"1180", "180", "1000", "700", "400.000")
	seedProfitSale(t, ctx, h.pool, h.ids.tenantID, h.ids.station2, h.ids.tankMSA, h.ids.pmsProduct, adminID,
		"590", "90", "500", "300", "200.000")

	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	op := h.login(t, tenantSlug, h.ids.opEmail)

	// Admin (tenant-wide) sees BOTH stations.
	code, body := h.getJSON(t, "/api/v1/reports/station-comparison?period=this-month", admin)
	if code != http.StatusOK {
		t.Fatalf("admin station-comparison = %d, want 200 (%v)", code, body)
	}
	if got := summaryValue(body, "Stations compared"); got != "2" {
		t.Fatalf("admin stations compared = %q, want 2", got)
	}

	// Operator is scoped to station1 only: the comparison must show exactly one
	// station (its own), proving the report respects station access.
	code, body = h.getJSON(t, "/api/v1/reports/station-comparison?period=this-month", op)
	if code != http.StatusOK {
		t.Fatalf("operator station-comparison = %d, want 200 (%v)", code, body)
	}
	if got := summaryValue(body, "Stations compared"); got != "1" {
		t.Fatalf("operator stations compared = %q, want 1 (own station only)", got)
	}
	// And the single row must be station1 (MIK-01), never station2.
	tbl, _ := body["table"].(map[string]any)
	rows, _ := tbl["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("operator comparison rows = %d, want 1", len(rows))
	}
	if first, _ := rows[0].([]any); len(first) == 0 || first[0] != "MIK-01" {
		t.Fatalf("operator comparison row = %v, want station MIK-01", rows)
	}

	// A freshly-created attendant holds no revenue.read: the report is 403.
	att := freshAttendant(t, ctx, h, tenantSlug)
	if code, _ := h.getJSON(t, "/api/v1/reports/station-comparison", att); code != http.StatusForbidden {
		t.Fatalf("attendant station-comparison = %d, want 403", code)
	}
}

// freshAttendant creates a brand-new attendant user (the minimal role, which
// holds neither revenue.read nor finance.read) with a station grant on station1,
// and logs in. Used to assert the report permission gate (403).
func freshAttendant(t *testing.T, ctx context.Context, h *harness, tenantSlug string) string {
	t.Helper()
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := "att-report-" + uuid.NewString()[:8] + "@it.local"
	var uid uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'Report Attendant', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, email, hash).Scan(&uid); err != nil {
		t.Fatalf("seed attendant: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, uid, "attendant")
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO user_station_access (user_id, station_id, tenant_id) VALUES ($1, $2, $3)`,
		uid, h.ids.station1, h.ids.tenantID); err != nil {
		t.Fatalf("station access: %v", err)
	}
	return h.login(t, tenantSlug, email)
}
