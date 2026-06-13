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
