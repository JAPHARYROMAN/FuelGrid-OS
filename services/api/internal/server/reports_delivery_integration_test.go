package server_test

// DB-backed integration tests for the §5.7 Delivery & Procurement report. Reuses
// the Phase 2 harness; gated on TEST_DATABASE_URL + TEST_REDIS_URL.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5446/fuelgrid_delivery?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6446/0 \
//	go test ./services/api/internal/server -run ReportsDelivery -v
//
// They assert:
//
//	(a) the headline KPIs (ordered / received / variance / delivery count) are
//	    summed exactly in SQL from purchase_order_lines + deliveries, with the
//	    variance computed once in numeric;
//	(b) the report-specific chart_data carries the comparison / deliveries /
//	    scorecards / pipeline, every figure a decimal string, with the supplier
//	    scorecard scored and cost_shown:true for a margin.view holder (admin);
//	(c) supplier COST is gated: a station.read holder WITHOUT margin.view sees no
//	    fuel-cost KPI, cost_shown:false, no landed_cost on the delivery rows, no
//	    price dimension on the scorecard, and the cost-hidden data-quality note; and
//	(d) an actor without station.read is forbidden the report.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// seedDeliveryFixture seeds one supplier, a received PO (24000 ordered, 23600
// received on the line), and a priced delivery of 23600 L into station1's PMS
// tank with a small dip variance. Returns the supplier id. created_at/received_at
// default to now(), so the this-month window picks them up.
func seedDeliveryFixture(t *testing.T, ctx context.Context, h *harness, adminID uuid.UUID) uuid.UUID {
	t.Helper()
	var supplierID, poID, poLineID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO suppliers (tenant_id, code, name) VALUES ($1, 'DLV-SUP-1', 'Acme Petroleum') RETURNING id`,
		h.ids.tenantID).Scan(&supplierID); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO purchase_orders (tenant_id, station_id, supplier_id, status, raised_by, expected_delivery_date)
		VALUES ($1, $2, $3, 'received', $4, current_date) RETURNING id
	`, h.ids.tenantID, h.ids.station1, supplierID, adminID).Scan(&poID); err != nil {
		t.Fatalf("seed po: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO purchase_order_lines (tenant_id, purchase_order_id, product_id, ordered_litres, unit_price, received_litres)
		VALUES ($1, $2, $3, 24000, 2950, 23600) RETURNING id
	`, h.ids.tenantID, poID, h.ids.pmsProduct).Scan(&poLineID); err != nil {
		t.Fatalf("seed po line: %v", err)
	}
	// A priced, PO-matched delivery into the PMS tank: 23600 L received, declared
	// vs measured dip variance of -30 L, landed cost 3,670,000 (≈155.5/L).
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO deliveries (
			tenant_id, tank_id, supplier_id, purchase_order_id, po_line_id,
			volume_litres, dip_before_litres, dip_after_litres, dip_variance_litres,
			received_by, landed_cost_total, landed_cost_per_litre, match_status
		) VALUES ($1, $2, $3, $4, $5, 23600, 1000, 24570, -30, $6, 3670000, 155.5085, 'matched')
	`, h.ids.tenantID, h.ids.tankPMS, supplierID, poID, poLineID, adminID); err != nil {
		t.Fatalf("seed delivery: %v", err)
	}
	return supplierID
}

func TestReportsDelivery_HeroChartAndScorecard(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	adminID := adminUserID(t, ctx, h.pool, h.ids.tenantID)

	seedDeliveryFixture(t, ctx, h, adminID)

	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	code, body := h.getJSON(t, "/api/v1/reports/delivery?station_id="+h.ids.station1.String()+"&period=this-month", admin)
	if code != http.StatusOK {
		t.Fatalf("delivery report = %d, want 200 (%v)", code, body)
	}

	// metadata.report_key identifies the report.
	meta, ok := body["metadata"].(map[string]any)
	if !ok || meta["report_key"] != "delivery" {
		t.Fatalf("metadata.report_key = %v, want delivery", body["metadata"])
	}

	// The canonical envelope slices are always present (never null).
	for _, key := range []string{"data_quality", "summary", "insights", "recommended_actions", "table", "chart_data", "drilldown", "export_options"} {
		if _, present := body[key]; !present {
			t.Fatalf("envelope missing %q section: %v", key, body)
		}
	}

	// (a) Headline KPIs, summed exactly in SQL. Ordered 24000, received 23600,
	// variance = received − ordered = −400 (numeric), 1 delivery.
	for label, want := range map[string]string{
		"Ordered":  "24000.000",
		"Received": "23600.000",
	} {
		if got := summaryValue(body, label); got != want {
			t.Fatalf("delivery KPI %q = %q, want %q", label, got, want)
		}
	}
	if got := summaryValue(body, "Delivery variance"); !strings.HasPrefix(got, "-400") {
		t.Fatalf("delivery variance = %q, want -400 (received − ordered)", got)
	}
	if got := summaryValue(body, "Deliveries"); got != "1" {
		t.Fatalf("delivery count = %q, want 1", got)
	}
	// (b) cost KPI surfaces for a margin.view holder (the seeded admin).
	if got := summaryValue(body, "Fuel cost"); got == "" {
		t.Fatalf("Fuel cost KPI missing for a margin.view holder: %v", body["summary"])
	}

	chart, ok := body["chart_data"].(map[string]any)
	if !ok {
		t.Fatalf("chart_data is not an object: %v", body["chart_data"])
	}
	if chart["cost_shown"] != true {
		t.Fatalf("chart_data.cost_shown = %v, want true for a margin.view holder", chart["cost_shown"])
	}
	// The comparison carries the PMS product row.
	if cmp, ok := chart["comparison"].([]any); !ok || len(cmp) == 0 {
		t.Fatalf("chart_data.comparison = %v, want at least one product row", chart["comparison"])
	}
	// The supplier scorecard scored Acme.
	cards, ok := chart["scorecards"].([]any)
	if !ok || len(cards) == 0 {
		t.Fatalf("chart_data.scorecards = %v, want at least one supplier", chart["scorecards"])
	}
	card0, _ := cards[0].(map[string]any)
	if card0["supplier_name"] != "Acme Petroleum" {
		t.Fatalf("scorecard supplier = %v, want Acme Petroleum", card0["supplier_name"])
	}
	if _, present := card0["price_score"]; !present {
		t.Fatalf("scorecard omitted price_score for a margin.view holder: %v", card0)
	}
	// The pipeline carries the received PO.
	if pl, ok := chart["pipeline"].([]any); !ok || len(pl) == 0 {
		t.Fatalf("chart_data.pipeline = %v, want at least one PO-status row", chart["pipeline"])
	}
}

// TestReportsDelivery_AvgCostExcludesUncosted locks in the avg-cost/litre fix: a
// legacy delivery with a NULL landed_cost_total must NOT dilute the weighted mean.
// We seed one priced delivery (10,000 L @ 1,550,000 = 155/L) plus one un-priced
// legacy delivery (10,000 L, NULL cost) and assert avg cost/litre stays at 155/L
// (costed rows only), not 77.5/L (the diluted figure if uncosted litres counted).
func TestReportsDelivery_AvgCostExcludesUncosted(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	adminID := adminUserID(t, ctx, h.pool, h.ids.tenantID)

	var supplierID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO suppliers (tenant_id, code, name) VALUES ($1, 'DLV-SUP-AC', 'Costed Co') RETURNING id`,
		h.ids.tenantID).Scan(&supplierID); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
	// A priced delivery: 10,000 L at a landed cost of 1,550,000 (155/L exactly).
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO deliveries (
			tenant_id, tank_id, supplier_id, volume_litres,
			dip_before_litres, dip_after_litres, dip_variance_litres,
			received_by, landed_cost_total, landed_cost_per_litre, match_status
		) VALUES ($1, $2, $3, 10000, 1000, 11000, 0, $4, 1550000, 155.0000, 'matched')
	`, h.ids.tenantID, h.ids.tankPMS, supplierID, adminID); err != nil {
		t.Fatalf("seed priced delivery: %v", err)
	}
	// A legacy un-priced delivery: 10,000 L, NULL landed cost (NULL per-litre too).
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO deliveries (
			tenant_id, tank_id, supplier_id, volume_litres,
			dip_before_litres, dip_after_litres, dip_variance_litres,
			received_by, landed_cost_total, landed_cost_per_litre, match_status
		) VALUES ($1, $2, $3, 10000, 1000, 11000, 0, $4, NULL, NULL, 'unmatched')
	`, h.ids.tenantID, h.ids.tankPMS, supplierID, adminID); err != nil {
		t.Fatalf("seed legacy delivery: %v", err)
	}

	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	code, body := h.getJSON(t, "/api/v1/reports/delivery?station_id="+h.ids.station1.String()+"&period=this-month", admin)
	if code != http.StatusOK {
		t.Fatalf("delivery report = %d, want 200 (%v)", code, body)
	}
	// Avg cost / litre is the COSTED weighted mean (1,550,000 / 10,000 = 155),
	// NOT diluted by the 10,000 un-priced litres (which would give 77.5).
	if got := summaryValue(body, "Avg cost / litre"); !strings.HasPrefix(got, "155") {
		t.Fatalf("avg cost / litre = %q, want ~155 (costed rows only, undiluted by NULL-cost legacy deliveries)", got)
	}
	// The fuel-cost KPI still sums every priced row (1,550,000).
	if got := summaryValue(body, "Fuel cost"); !strings.HasPrefix(got, "1550000") {
		t.Fatalf("fuel cost = %q, want 1550000 (sum of priced landed cost)", got)
	}
}

func TestReportsDelivery_CostGatedAndPermission(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	adminID := adminUserID(t, ctx, h.pool, h.ids.tenantID)

	seedDeliveryFixture(t, ctx, h, adminID)

	// A supervisor scoped to station1 holds station.read (migration 0004) but NOT
	// margin.view, so supplier COST must be omitted and the cost-hidden data-quality
	// note raised.
	sup := freshSupervisor(t, ctx, h, tenantSlug)
	code, body := h.getJSON(t, "/api/v1/reports/delivery?station_id="+h.ids.station1.String(), sup)
	if code != http.StatusOK {
		t.Fatalf("supervisor delivery report = %d, want 200 (%v)", code, body)
	}
	if got := summaryValue(body, "Fuel cost"); got != "" {
		t.Fatalf("Fuel cost = %q, want omitted for a non-margin.view actor", got)
	}
	chart, _ := body["chart_data"].(map[string]any)
	if chart["cost_shown"] != false {
		t.Fatalf("chart_data.cost_shown = %v, want false for a non-margin.view actor", chart["cost_shown"])
	}
	// The delivery rows omit landed_cost entirely (not zeroed).
	if rows, ok := chart["deliveries"].([]any); ok {
		for _, r := range rows {
			row, _ := r.(map[string]any)
			if _, present := row["landed_cost"]; present {
				t.Fatalf("delivery row leaked a landed_cost field to a non-margin.view actor: %v", row)
			}
		}
	}
	// The scorecard omits the price dimension entirely (price_included:false,
	// price_score omitted).
	if cards, ok := chart["scorecards"].([]any); ok {
		for _, c := range cards {
			card, _ := c.(map[string]any)
			if card["price_included"] != false {
				t.Fatalf("scorecard price_included = %v, want false for a non-cost actor: %v", card["price_included"], card)
			}
			if _, present := card["price_score"]; present {
				t.Fatalf("scorecard leaked price_score to a non-cost actor: %v", card)
			}
		}
	}
	// The cost-hidden data-quality note is present.
	var hasCostNote bool
	if dq, ok := body["data_quality"].([]any); ok {
		for _, d := range dq {
			item, _ := d.(map[string]any)
			if msg, _ := item["message"].(string); strings.Contains(msg, "margin.view") {
				hasCostNote = true
			}
		}
	}
	if !hasCostNote {
		t.Fatalf("expected a cost-hidden data-quality note for a non-margin.view actor: %v", body["data_quality"])
	}

	// A freshly-created attendant holds no station.read: the report is 403.
	att := freshAttendant(t, ctx, h, tenantSlug)
	if code, _ := h.getJSON(t, "/api/v1/reports/delivery?station_id="+h.ids.station1.String(), att); code != http.StatusForbidden {
		t.Fatalf("attendant delivery report = %d, want 403", code)
	}
}
