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
