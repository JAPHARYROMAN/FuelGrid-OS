package server_test

// DB-backed integration tests for the structured, permission-aware report API
// (REPORTS-STRUCTURED). Reuses the Phase 2 harness; gated on TEST_DATABASE_URL +
// TEST_REDIS_URL.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5433/fuelgrid?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6379/0 \
//	go test ./services/api/internal/server -run ReportsStructured -v
//
// Two guarantees are asserted:
//
//	(a) the reconciliation report is tenant-scoped — an actor from a second tenant
//	    cannot read the first tenant's station report (404, station not found);
//	(b) each report endpoint enforces its permission — the seeded operator
//	    (station_manager, which holds reconciliation.read but NOT finance.read)
//	    can read the reconciliation report (200) but is forbidden the cash
//	    reconciliation report (403).

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestReportsStationClose_EnvelopeShape asserts the Daily Station Close report
// returns the signature envelope: a permission-gated 200 for an actor holding
// revenue.read, with the close summary surfacing the expected KPI labels and the
// always-present (even if empty) data_quality + summary slices. With no revenue
// day seeded for the station, the additive tender_mix is omitted (omitempty) and
// the close reads as an honest empty-but-valid envelope — exactly the partial
// state the report is required to render.
func TestReportsStationClose_EnvelopeShape(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)

	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	// Admin holds revenue.read tenant-wide: the close report must succeed.
	code, body := h.getJSON(t, "/api/v1/reports/station-close?station_id="+h.ids.station1.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("admin station-close report = %d, want 200", code)
	}

	// metadata.report_key identifies the report.
	meta, ok := body["metadata"].(map[string]any)
	if !ok || meta["report_key"] != "station-close" {
		t.Fatalf("metadata.report_key = %v, want station-close", body["metadata"])
	}

	// The canonical envelope slices are always present (never null), so the wire
	// shape is stable for the typed SDK + the page.
	for _, key := range []string{"data_quality", "summary", "insights", "recommended_actions", "table"} {
		if _, present := body[key]; !present {
			t.Fatalf("envelope missing %q section: %v", key, body)
		}
	}

	// The summary always carries the close's approval status (the headline state)
	// — even with no revenue day, where it reads "no_data".
	summary, ok := body["summary"].([]any)
	if !ok || len(summary) == 0 {
		t.Fatalf("summary = %v, want a non-empty headline summary", body["summary"])
	}
	var hasApproval bool
	for _, m := range summary {
		if row, ok := m.(map[string]any); ok && row["label"] == "Approval status" {
			hasApproval = true
		}
	}
	if !hasApproval {
		t.Fatalf("summary is missing the Approval status metric: %v", summary)
	}

	// With no revenue day seeded, the additive tender_mix is omitted (omitempty),
	// proving the field is genuinely optional and the report degrades honestly.
	if _, present := body["tender_mix"]; present {
		t.Fatalf("tender_mix should be omitted when no revenue day exists: %v", body["tender_mix"])
	}
}

// TestReportsReconciliation_EnvelopeShape asserts the Inventory Reconciliation
// report returns the signature §20.3 envelope: a permission-gated 200 for an
// actor holding reconciliation.read, with the KPI-hero summary carrying the
// blueprint headline labels (total variance litres, variance %, over-tolerance
// tank count, tanks reconciled) and the always-present envelope slices. With no
// tanks reconciled for the station's active day, the report degrades honestly —
// the four base KPIs are still present and a data-quality warning explains the
// empty state (so the report never reads as final when it has no data).
func TestReportsReconciliation_EnvelopeShape(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)

	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	code, body := h.getJSON(t, "/api/v1/reports/inventory/reconciliation?station_id="+h.ids.station1.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("admin reconciliation report = %d, want 200", code)
	}

	meta, ok := body["metadata"].(map[string]any)
	if !ok || meta["report_key"] != "inventory-reconciliation" {
		t.Fatalf("metadata.report_key = %v, want inventory-reconciliation", body["metadata"])
	}

	// The canonical envelope slices are always present (never null).
	for _, key := range []string{"data_quality", "summary", "insights", "recommended_actions", "table", "chart_data", "drilldown", "export_options"} {
		if _, present := body[key]; !present {
			t.Fatalf("envelope missing %q section: %v", key, body)
		}
	}

	// The KPI hero carries the blueprint §20.3 headline labels.
	summary, ok := body["summary"].([]any)
	if !ok || len(summary) == 0 {
		t.Fatalf("summary = %v, want a non-empty KPI hero", body["summary"])
	}
	got := map[string]bool{}
	for _, m := range summary {
		if row, ok := m.(map[string]any); ok {
			if label, ok := row["label"].(string); ok {
				got[label] = true
			}
		}
	}
	for _, want := range []string{"Total variance", "Variance %", "Over-tolerance tanks", "Tanks reconciled"} {
		if !got[want] {
			t.Fatalf("KPI hero is missing the %q metric: %v", want, summary)
		}
	}

	// With no tanks reconciled, the report raises a data-quality warning rather
	// than reading as a clean, final reconciliation.
	dq, ok := body["data_quality"].([]any)
	if !ok || len(dq) == 0 {
		t.Fatalf("data_quality = %v, want an empty-day warning", body["data_quality"])
	}
}

// TestReportsCashReconciliation_EnvelopeShape asserts the Cash Reconciliation
// report returns the signature §20.5 envelope: a permission-gated 200 for an
// actor holding finance.read, with the KPI-hero summary carrying the blueprint
// headline labels (expected / submitted / deposited cash, net variance, the
// shortage/excess variance status) and the always-present envelope slices. With
// no reconciliation seeded for the station, the report degrades honestly — the
// base KPIs are still present, the settlement-status board still renders its
// medium chips in chart_data, and a data-quality warning explains the empty
// state (so the report never reads as final when it has no data).
func TestReportsCashReconciliation_EnvelopeShape(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)

	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	code, body := h.getJSON(t, "/api/v1/reports/cash-reconciliation?station_id="+h.ids.station1.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("admin cash-reconciliation report = %d, want 200", code)
	}

	meta, ok := body["metadata"].(map[string]any)
	if !ok || meta["report_key"] != "cash-reconciliation" {
		t.Fatalf("metadata.report_key = %v, want cash-reconciliation", body["metadata"])
	}

	// The canonical envelope slices are always present (never null).
	for _, key := range []string{"data_quality", "summary", "insights", "recommended_actions", "table", "chart_data", "drilldown", "export_options"} {
		if _, present := body[key]; !present {
			t.Fatalf("envelope missing %q section: %v", key, body)
		}
	}

	// The KPI hero carries the blueprint §20.5 headline labels.
	summary, ok := body["summary"].([]any)
	if !ok || len(summary) == 0 {
		t.Fatalf("summary = %v, want a non-empty KPI hero", body["summary"])
	}
	got := map[string]bool{}
	for _, m := range summary {
		if row, ok := m.(map[string]any); ok {
			if label, ok := row["label"].(string); ok {
				got[label] = true
			}
		}
	}
	for _, want := range []string{"Expected cash", "Submitted cash", "Deposited cash", "Net variance", "Variance status"} {
		if !got[want] {
			t.Fatalf("KPI hero is missing the %q metric: %v", want, summary)
		}
	}

	// The settlement-status board rides in chart_data.settlement — a chip per
	// medium (cash / mobile-money / card / bank deposit), each carrying a text
	// status (never colour-alone) so the front-end StatusBoard reads accessibly.
	chart, ok := body["chart_data"].(map[string]any)
	if !ok {
		t.Fatalf("chart_data = %v, want a {flow, settlement} object", body["chart_data"])
	}
	board, ok := chart["settlement"].([]any)
	if !ok || len(board) != 4 {
		t.Fatalf("chart_data.settlement = %v, want 4 medium chips", chart["settlement"])
	}
	keys := map[string]bool{}
	for _, c := range board {
		chip, ok := c.(map[string]any)
		if !ok {
			t.Fatalf("settlement chip is not an object: %v", c)
		}
		if chip["status"] == nil || chip["status"] == "" {
			t.Fatalf("settlement chip is missing a text status (colour must not be the only signal): %v", chip)
		}
		if k, ok := chip["key"].(string); ok {
			keys[k] = true
		}
	}
	for _, want := range []string{"cash", "mobile_money", "card", "bank_deposit"} {
		if !keys[want] {
			t.Fatalf("settlement board is missing the %q medium chip: %v", want, board)
		}
	}

	// With no reconciliation recorded, the report raises a data-quality warning
	// rather than reading as a clean, final cash position.
	dq, ok := body["data_quality"].([]any)
	if !ok || len(dq) == 0 {
		t.Fatalf("data_quality = %v, want an empty-state warning", body["data_quality"])
	}
}

func TestReportsStructured_TenantScopingAndPermissions(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)

	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	op := h.login(t, tenantSlug, h.ids.opEmail)

	// (b) Permission enforcement -------------------------------------------------

	// Operator holds reconciliation.read and is scoped to station1: the
	// reconciliation report for station1 must succeed.
	if code, _ := h.getJSON(t, "/api/v1/reports/inventory/reconciliation?station_id="+h.ids.station1.String(), op); code != http.StatusOK {
		t.Fatalf("operator reconciliation report (own station) = %d, want 200", code)
	}
	// Operator does NOT hold finance.read: the cash reconciliation report must be
	// forbidden regardless of station scope.
	if code, _ := h.getJSON(t, "/api/v1/reports/cash-reconciliation?station_id="+h.ids.station1.String(), op); code != http.StatusForbidden {
		t.Fatalf("operator cash reconciliation report = %d, want 403 (no finance.read)", code)
	}
	// Operator is scoped to station1, so station2 (in scope of the tenant but not
	// the operator) must be forbidden by the in-handler authorizeStation check.
	if code, _ := h.getJSON(t, "/api/v1/reports/inventory/reconciliation?station_id="+h.ids.station2.String(), op); code != http.StatusForbidden {
		t.Fatalf("operator reconciliation report (out-of-scope station) = %d, want 403", code)
	}
	// Admin holds finance.read tenant-wide: the cash report must succeed.
	if code, _ := h.getJSON(t, "/api/v1/reports/cash-reconciliation?station_id="+h.ids.station1.String(), admin); code != http.StatusOK {
		t.Fatalf("admin cash reconciliation report = %d, want 200", code)
	}

	// (a) Tenant scoping ---------------------------------------------------------

	// Seed a fully separate tenant and log in as its admin. Requesting the FIRST
	// tenant's station must 404 — the station is invisible across the tenant
	// boundary (the repo scopes by tenant_id), proving no cross-tenant leakage.
	other := seedTenant(t, ctx, h.pool)
	defer cleanupTenant(ctx, h.pool, other.tenantID)
	var otherSlug string
	if err := h.pool.QueryRow(ctx, `SELECT slug FROM tenants WHERE id = $1`, other.tenantID).Scan(&otherSlug); err != nil {
		t.Fatalf("other tenant slug: %v", err)
	}
	otherAdmin := h.login(t, otherSlug, other.adminEmail)

	if code, _ := h.getJSON(t, "/api/v1/reports/inventory/reconciliation?station_id="+h.ids.station1.String(), otherAdmin); code != http.StatusNotFound {
		t.Fatalf("cross-tenant reconciliation report = %d, want 404 (station not found in caller's tenant)", code)
	}
	// And the second tenant's admin can read its OWN station's report (200),
	// confirming the 404 above was scoping — not a blanket failure.
	if code, _ := h.getJSON(t, "/api/v1/reports/inventory/reconciliation?station_id="+other.station1.String(), otherAdmin); code != http.StatusOK {
		t.Fatalf("other-tenant admin own-station reconciliation report = %d, want 200", code)
	}
}

// TestReportsCashReconciliation_Figures seeds a DRAFT reconciliation (expected
// cash seeded, drawer not yet counted → counted 0 / variance 0) alongside a
// SUBMITTED one (a real counted shortage) and asserts the headline aggregates
// reconcile with the shortage/excess status. The draft's seeded expected must
// NOT inflate the totals (otherwise Net variance reads as a huge shortage while
// the status stays balanced) — so only the counted recon drives expected /
// submitted / net variance / shortage, and the three figures agree.
func TestReportsCashReconciliation_Figures(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`,
		h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("nozzle: %v", err)
	}

	// Day A: a SUBMITTED recon with a real 5,000 shortage (counted 595k < 600k).
	dayA, _ := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-05-01", 1000)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO cash_reconciliations (tenant_id, station_id, operating_day_id, expected_cash, counted_cash, variance, status, created_by)
		VALUES ($1, $2, $3, 600000, 595000, -5000, 'submitted', $4)
	`, h.ids.tenantID, h.ids.station1, dayA, adminID); err != nil {
		t.Fatalf("seed submitted recon: %v", err)
	}
	// Day B: a DRAFT recon — expected seeded (400k) but drawer not counted, so
	// counted/variance default to 0. It must stay OUT of the headline totals.
	dayB, _ := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-05-02", 1000)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO cash_reconciliations (tenant_id, station_id, operating_day_id, expected_cash, counted_cash, variance, status, created_by)
		VALUES ($1, $2, $3, 400000, 0, 0, 'draft', $4)
	`, h.ids.tenantID, h.ids.station1, dayB, adminID); err != nil {
		t.Fatalf("seed draft recon: %v", err)
	}

	code, body := h.getJSON(t, "/api/v1/reports/cash-reconciliation?station_id="+h.ids.station1.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("cash report = %d, want 200", code)
	}

	// Only the counted recon feeds the totals: expected 600k, submitted 595k,
	// net variance −5,000 — NOT 595,000−1,000,000 (which folding the draft in
	// would yield), and NOT a balanced status.
	for label, want := range map[string]string{
		"Expected cash":   "600000.00",
		"Submitted cash":  "595000.00",
		"Net variance":    "-5000.00",
		"Total shortage":  "5000.00",
		"Total excess":    "0.00",
		"Variance status": "Shortage",
	} {
		if got := summaryValue(body, label); got != want {
			t.Fatalf("cash KPI %q = %q, want %q (draft recon must not inflate the totals)", label, got, want)
		}
	}
}

// TestReportsStationClose_DayAlignedCash seeds a revenue day plus the SAME day's
// submitted cash reconciliation, and a LATER (newest) recon for a different day,
// then asserts the close headline reconciles the headline day against ITS OWN
// recon — never the newest recon's variance bolted onto the headline day's
// tender. Submitted cash must equal the day's counted drawer (not tender ± a
// cross-day variance), and the cash variance is that day's own variance.
func TestReportsStationClose_DayAlignedCash(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`,
		h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("nozzle: %v", err)
	}

	// The HEADLINE day (newest business_date) with its own recon: expected 500k,
	// counted 480k, variance −20,000.
	headDay, _ := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-05-10", 1000)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO revenue_days (tenant_id, station_id, operating_day_id, business_date, gross_revenue, net_revenue, cash_total, tender_total, cash_variance, status)
		VALUES ($1, $2, $3, '2026-05-10', 500000, 500000, 500000, 500000, 0, 'draft')
	`, h.ids.tenantID, h.ids.station1, headDay); err != nil {
		t.Fatalf("seed revenue day: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO cash_reconciliations (tenant_id, station_id, operating_day_id, expected_cash, counted_cash, variance, status, created_by)
		VALUES ($1, $2, $3, 500000, 480000, -20000, 'submitted', $4)
	`, h.ids.tenantID, h.ids.station1, headDay, adminID); err != nil {
		t.Fatalf("seed head-day recon: %v", err)
	}
	// A NEWER recon (by created_at) for a DIFFERENT, EARLIER business day with a
	// very different variance — the global-latest. The close must IGNORE it for
	// the headline day's cash position.
	otherDay, _ := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-05-09", 1000)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO cash_reconciliations (tenant_id, station_id, operating_day_id, expected_cash, counted_cash, variance, status, created_by, created_at)
		VALUES ($1, $2, $3, 700000, 707000, 7000, 'submitted', $4, now() + interval '1 hour')
	`, h.ids.tenantID, h.ids.station1, otherDay, adminID); err != nil {
		t.Fatalf("seed other-day recon: %v", err)
	}

	code, body := h.getJSON(t, "/api/v1/reports/station-close?station_id="+h.ids.station1.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("station-close report = %d, want 200", code)
	}

	// Submitted cash = the HEADLINE day's counted drawer (480,000), and the cash
	// variance is the headline day's own −20,000 — NOT 500,000 + 7,000 (the
	// newest recon's cross-day variance) and NOT the newest recon's figures.
	if v := summaryValue(body, "Submitted cash"); v != "480000.00" {
		t.Fatalf("Submitted cash = %q, want 480000.00 (the headline day's counted drawer, not a cross-day blend)", v)
	}
	if v := summaryValue(body, "Cash variance"); v != "-20000.00" {
		t.Fatalf("Cash variance = %q, want -20000.00 (the headline day's own variance, not the newest recon's +7000)", v)
	}
	if v := summaryValue(body, "Expected cash"); v != "500000.00" {
		t.Fatalf("Expected cash = %q, want 500000.00 (the headline day's recon expected)", v)
	}
}
