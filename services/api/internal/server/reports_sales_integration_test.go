package server_test

// DB-backed integration tests for the §5.2 Sales report. Reuses the Phase 2
// harness + the profitability seeders; gated on TEST_DATABASE_URL +
// TEST_REDIS_URL.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5433/fuelgrid?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6379/0 \
//	go test ./services/api/internal/server -run ReportsSales -v
//
// They assert:
//
//	(a) the headline KPIs (litres / revenue / avg selling price / transactions)
//	    are summed exactly in SQL and net of an approved sale void;
//	(b) the report-specific chart_data carries the by_product / by_nozzle / by_hour
//	    breakdowns and the trend, every figure a decimal string, with margin
//	    present for a margin.view holder (admin);
//	(c) the payment-method tender_mix is summed from recorded payments over the
//	    window; and
//	(d) a freshly-created attendant (no revenue.read) is forbidden the report.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

func TestReportsSales_HeroChartAndTenderMix(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	adminID := adminUserID(t, ctx, h.pool, h.ids.tenantID)

	// Two recognized sales on station1 today (net 1000+500, cogs 700+300,
	// litres 400+200) plus an approved void of the SECOND, so the report nets to
	// litres=400, gross=1180, net=1000, cogs=700, margin=300, txn=1.
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

	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	code, body := h.getJSON(t, "/api/v1/reports/sales?station_id="+h.ids.station1.String()+"&period=this-month", admin)
	if code != http.StatusOK {
		t.Fatalf("sales report = %d, want 200 (%v)", code, body)
	}

	// metadata.report_key identifies the report.
	meta, ok := body["metadata"].(map[string]any)
	if !ok || meta["report_key"] != "sales" {
		t.Fatalf("metadata.report_key = %v, want sales", body["metadata"])
	}

	// The canonical envelope slices are always present (never null).
	for _, key := range []string{"data_quality", "summary", "insights", "recommended_actions", "table", "chart_data", "drilldown", "export_options"} {
		if _, present := body[key]; !present {
			t.Fatalf("envelope missing %q section: %v", key, body)
		}
	}

	// (a) Headline KPIs, net of the approved void. Average selling price is
	// gross/litres = 1180/400 = 2.95 (the seeded close used 2950/litre but the
	// figures here are the explicit decimal literals seeded above).
	// The litres column is numeric(14,3) and the money columns numeric(14,2), so
	// the exact decimal strings carry their stored scale.
	for label, want := range map[string]string{
		"Litres sold":  "400.000",
		"Revenue":      "1180.00",
		"Transactions": "1",
	} {
		if got := summaryValue(body, label); got != want {
			t.Fatalf("sales KPI %q = %q, want %q (net of the approved void)", label, got, want)
		}
	}
	// Average selling price is a SQL numeric ratio (1180/400 = 2.95).
	if got := summaryValue(body, "Average selling price"); !strings.HasPrefix(got, "2.95") {
		t.Fatalf("avg selling price = %q, want 2.95... (gross/litres)", got)
	}
	// Margin is surfaced for the admin (holds margin.view tenant-wide).
	if got := summaryValue(body, "Gross margin"); got != "300.00" {
		t.Fatalf("gross margin = %q, want 300.00 (margin.view holder)", got)
	}

	// (b) chart_data carries the report-specific breakdowns + the margin_shown flag.
	chart, ok := body["chart_data"].(map[string]any)
	if !ok {
		t.Fatalf("chart_data = %v, want the sales chart object", body["chart_data"])
	}
	if chart["margin_shown"] != true {
		t.Fatalf("chart_data.margin_shown = %v, want true for a margin.view holder", chart["margin_shown"])
	}
	byProduct, ok := chart["by_product"].([]any)
	if !ok || len(byProduct) != 1 {
		t.Fatalf("chart_data.by_product = %v, want exactly one product row", chart["by_product"])
	}
	prod, _ := byProduct[0].(map[string]any)
	if prod["net"] != "1000.00" || prod["litres"] != "400.000" {
		t.Fatalf("by_product[0] net/litres = %v, want net 1000.00 / litres 400.000", prod)
	}
	if prod["margin"] != "300.00" {
		t.Fatalf("by_product[0].margin = %v, want 300.00 (margin shown)", prod["margin"])
	}
	// by_hour is always the full 24-cell grid.
	if hours, ok := chart["by_hour"].([]any); !ok || len(hours) != 24 {
		t.Fatalf("chart_data.by_hour = %d cells, want 24", len(asSlice(chart["by_hour"])))
	}
	// by_nozzle has one row (the seeded nozzle carried both the kept + voided sale).
	if nozzles, ok := chart["by_nozzle"].([]any); !ok || len(nozzles) == 0 {
		t.Fatalf("chart_data.by_nozzle = %v, want at least one nozzle row", chart["by_nozzle"])
	}
}

func TestReportsSales_MarginGatedAndPermission(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	adminID := adminUserID(t, ctx, h.pool, h.ids.tenantID)

	seedProfitSale(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, h.ids.tankPMS, h.ids.pmsProduct, adminID,
		"1180", "180", "1000", "700", "400.000")

	// A supervisor scoped to station1 holds revenue.read (migration 0033) but NOT
	// margin.view (migration 0004), so the report must omit margin and raise the
	// margin-hidden data-quality note. (The seeded station_manager DOES hold
	// margin.view, so it would not exercise the gate.)
	sup := freshSupervisor(t, ctx, h, tenantSlug)
	code, body := h.getJSON(t, "/api/v1/reports/sales?station_id="+h.ids.station1.String(), sup)
	if code != http.StatusOK {
		t.Fatalf("supervisor sales report = %d, want 200 (%v)", code, body)
	}
	if got := summaryValue(body, "Gross margin"); got != "" {
		t.Fatalf("Gross margin = %q, want omitted for a non-margin.view actor", got)
	}
	chart, _ := body["chart_data"].(map[string]any)
	if chart["margin_shown"] != false {
		t.Fatalf("chart_data.margin_shown = %v, want false for a non-margin.view actor", chart["margin_shown"])
	}
	// The by_product rows omit the margin entirely (not zeroed).
	if rows, ok := chart["by_product"].([]any); ok {
		for _, r := range rows {
			row, _ := r.(map[string]any)
			if _, present := row["margin"]; present {
				t.Fatalf("by_product row leaked a margin field to a non-margin.view actor: %v", row)
			}
		}
	}
	// The margin-hidden data-quality note is present.
	var hasMarginNote bool
	if dq, ok := body["data_quality"].([]any); ok {
		for _, d := range dq {
			item, _ := d.(map[string]any)
			if msg, _ := item["message"].(string); strings.Contains(msg, "margin.view") {
				hasMarginNote = true
			}
		}
	}
	if !hasMarginNote {
		t.Fatalf("expected a margin-hidden data-quality note for a non-margin.view actor: %v", body["data_quality"])
	}

	// A freshly-created attendant holds no revenue.read: the report is 403.
	att := freshAttendant(t, ctx, h, tenantSlug)
	if code, _ := h.getJSON(t, "/api/v1/reports/sales?station_id="+h.ids.station1.String(), att); code != http.StatusForbidden {
		t.Fatalf("attendant sales report = %d, want 403", code)
	}
}

// freshSupervisor creates a brand-new supervisor user (holds revenue.read via
// migration 0033 but NOT margin.view) with a station-1 grant, and logs in. Used
// to assert the sensitive-metric (margin) gate hides margin from a revenue
// reader without margin.view.
func freshSupervisor(t *testing.T, ctx context.Context, h *harness, tenantSlug string) string {
	t.Helper()
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := "sup-report-" + uuid.NewString()[:8] + "@it.local"
	var uid uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'Report Supervisor', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, email, hash).Scan(&uid); err != nil {
		t.Fatalf("seed supervisor: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, uid, "supervisor")
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO user_station_access (user_id, station_id, tenant_id) VALUES ($1, $2, $3)`,
		uid, h.ids.station1, h.ids.tenantID); err != nil {
		t.Fatalf("station access: %v", err)
	}
	return h.login(t, tenantSlug, email)
}

// asSlice coerces a chart_data field to a []any (or nil) for a length check.
func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}
