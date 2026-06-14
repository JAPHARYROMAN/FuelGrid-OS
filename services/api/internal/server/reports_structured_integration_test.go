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
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
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

// seedVarianceEvent seeds one over-tolerance tank reconciliation for station1 on
// a fresh operating day, with a closed shift staffed by the given attendant, so
// the Risk & Loss report's §5.11 pattern joins (event → shift → attendant) have
// real rows to count. variance is negative (a shortage/loss). Returns the day id.
func seedVarianceEvent(t *testing.T, ctx context.Context, h *harness, openedBy, attendant uuid.UUID, businessDate, shiftName string, variance float64) uuid.UUID {
	t.Helper()
	var dayID, shiftID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO operating_days (tenant_id, station_id, business_date, opened_by)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, h.ids.tenantID, h.ids.station1, businessDate, openedBy).Scan(&dayID); err != nil {
		t.Fatalf("seed operating day: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO shifts (tenant_id, station_id, operating_day_id, name, opened_by, status, closed_by, closed_at)
		VALUES ($1, $2, $3, $4, $5, 'closed', $5, now()) RETURNING id
	`, h.ids.tenantID, h.ids.station1, dayID, shiftName, openedBy).Scan(&shiftID); err != nil {
		t.Fatalf("seed shift: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO shift_attendants (shift_id, user_id, tenant_id, assigned_by)
		VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING
	`, shiftID, attendant, h.ids.tenantID, openedBy); err != nil {
		t.Fatalf("seed shift attendant: %v", err)
	}
	// An over-tolerance shortage: variance well beyond the 1% tolerance on a 5,000 L
	// book, status 'exception'. closing_physical = closing_book + variance.
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO tank_reconciliations
		    (tenant_id, tank_id, operating_day_id, opening_book, deliveries_total, sales_total,
		     adjustments_total, closing_book, closing_physical, variance_litres, variance_percent,
		     tolerance_percent, status)
		VALUES ($1, $2, $3, 5000, 0, 0, 0, 5000, 5000 + $4, $4, $4/5000*100, 1.0, 'exception')
	`, h.ids.tenantID, h.ids.tankPMS, dayID, variance); err != nil {
		t.Fatalf("seed reconciliation: %v", err)
	}
	return dayID
}

// TestReportsRiskLoss_EnvelopeAndPatterns asserts the §5.11 / §20.4 Risk & Loss
// report returns the signature envelope with the KPI hero, the deterministic
// chart payload (heatmap / trend / ranking / distribution / alert board /
// investigation timeline / patterns / rules) and the drillable table; that a
// recurring over-tolerance loss is surfaced; and that the sensitive loss VALUE is
// OMITTED for the operator (who lacks margin.view) but shown to the admin.
func TestReportsRiskLoss_EnvelopeAndPatterns(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	adminID, _, admin := h.adminContext(t, ctx)

	// A SUPERVISOR scoped to station1: holds reconciliation.read (so the report
	// succeeds) but NOT margin.view (so the loss VALUE must be omitted). The
	// station_manager operator the harness seeds DOES hold margin.view, so it is
	// the wrong actor for the value-gating assertion.
	hash, herr := password.New(password.DefaultParams, "").Hash(testPassword)
	if herr != nil {
		t.Fatalf("hash: %v", herr)
	}
	supEmail := fmt.Sprintf("sup-%d@it.local", time.Now().UnixNano())
	var supID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'IT Supervisor', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, supEmail, hash).Scan(&supID); err != nil {
		t.Fatalf("seed supervisor: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, supID, "supervisor")
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO user_station_access (user_id, station_id, tenant_id) VALUES ($1, $2, $3)`,
		supID, h.ids.station1, h.ids.tenantID); err != nil {
		t.Fatalf("seed supervisor station access: %v", err)
	}
	sup := h.login(t, tenantSlug, supEmail)

	// Three over-tolerance shortages on the same tank across days, two of them on
	// the EVENING shift staffed by the operator — a recurring, concentrated loss.
	seedVarianceEvent(t, ctx, h, adminID, h.ids.opID, "2026-05-20", "Evening", -300)
	seedVarianceEvent(t, ctx, h, adminID, h.ids.opID, "2026-05-21", "Evening", -250)
	seedVarianceEvent(t, ctx, h, adminID, adminID, "2026-05-22", "Morning", -200)

	// (1) Admin (holds reconciliation.read + margin.view) gets the full report.
	code, body := h.getJSON(t, "/api/v1/reports/risk-loss?station_id="+h.ids.station1.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("admin risk-loss report = %d, want 200", code)
	}
	meta, _ := body["metadata"].(map[string]any)
	if meta == nil || meta["report_key"] != "risk-loss" {
		t.Fatalf("metadata.report_key = %v, want risk-loss", body["metadata"])
	}
	for _, key := range []string{"data_quality", "summary", "insights", "recommended_actions", "table", "chart_data"} {
		if _, present := body[key]; !present {
			t.Fatalf("envelope missing %q section", key)
		}
	}
	// KPI hero: loss litres (3 shortages totalling 750 L) and the repeated-incident
	// count are present; the admin (margin.view) sees the loss VALUE.
	if v := summaryValue(body, "Total loss litres"); v != "750.000" {
		t.Fatalf("Total loss litres = %q, want 750.000", v)
	}
	if v := summaryValue(body, "Repeated-incident tanks"); v != "1" {
		t.Fatalf("Repeated-incident tanks = %q, want 1 (the tank breached on 3 days)", v)
	}
	if v := summaryValue(body, "Loss value"); v == "" {
		t.Fatalf("admin (margin.view) must see the Loss value KPI")
	}
	// chart_data carries every §5.11 visual section.
	chart, _ := body["chart_data"].(map[string]any)
	if chart == nil {
		t.Fatalf("chart_data is not an object: %v", body["chart_data"])
	}
	for _, key := range []string{"heatmap", "heat_types", "trend", "ranking", "distribution", "alert_board", "investigations", "patterns", "rules", "value_shown"} {
		if _, present := chart[key]; !present {
			t.Fatalf("chart_data missing %q section", key)
		}
	}
	if shown, _ := chart["value_shown"].(bool); !shown {
		t.Fatalf("value_shown must be true for the admin (margin.view)")
	}
	// The deterministic pattern intelligence found the Evening-shift concentration
	// (2 of 3 events). Patterns are traceable findings with a share %.
	patterns, _ := chart["patterns"].([]any)
	if len(patterns) == 0 {
		t.Fatalf("expected at least one §5.11 pattern finding, got none")
	}

	// (2) Supervisor holds reconciliation.read (scoped to station1) but NOT
	// margin.view — the report succeeds, but the loss VALUE is OMITTED (not zeroed).
	code, supBody := h.getJSON(t, "/api/v1/reports/risk-loss?station_id="+h.ids.station1.String(), sup)
	if code != http.StatusOK {
		t.Fatalf("supervisor risk-loss report (own station) = %d, want 200", code)
	}
	if v := summaryValue(supBody, "Loss value"); v != "" {
		t.Fatalf("supervisor (no margin.view) must NOT see the Loss value KPI, got %q", v)
	}
	supChart, _ := supBody["chart_data"].(map[string]any)
	if shown, _ := supChart["value_shown"].(bool); shown {
		t.Fatalf("value_shown must be false for the supervisor (no margin.view)")
	}
	// The loss LITRES are still fully shown to the non-margin holder.
	if v := summaryValue(supBody, "Total loss litres"); v != "750.000" {
		t.Fatalf("supervisor must still see loss litres in full, got %q", v)
	}
	// A data-quality note must explain the hidden value (omit-not-zero).
	if !dataQualityContains(supBody, "margin.view") {
		t.Fatalf("expected a data-quality note explaining the hidden loss value")
	}

	// (3) Station scoping: station2 is out of the supervisor's scope → 403.
	if code, _ := h.getJSON(t, "/api/v1/reports/risk-loss?station_id="+h.ids.station2.String(), sup); code != http.StatusForbidden {
		t.Fatalf("supervisor risk-loss report (out-of-scope station) = %d, want 403", code)
	}
}

// dataQualityContains reports whether any data_quality message contains substr.
func dataQualityContains(m map[string]any, substr string) bool {
	arr, ok := m["data_quality"].([]any)
	if !ok {
		return false
	}
	for _, it := range arr {
		row, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if msg, ok := row["message"].(string); ok && strings.Contains(msg, substr) {
			return true
		}
	}
	return false
}
