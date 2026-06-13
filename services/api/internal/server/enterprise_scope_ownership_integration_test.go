package server_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestSRL4_GrantScopeRejectsCrossTenantScopeID covers SR-L4: GrantScope must
// validate that a non-tenant scope_id (station/company/...) resolves to a row
// owned by the granting tenant. A scope_id belonging to ANOTHER tenant is
// rejected with 400; a same-tenant scope_id (and a bogus one) behave as
// expected.
//
// We run with RLS enabled so the test exercises the real tenant-scoped request
// path; the application-level existence check inside GrantScope explicitly
// filters by the caller's tenant_id, so a cross-tenant id can never match.
func TestSRL4_GrantScopeRejectsCrossTenantScopeID(t *testing.T) {
	h, cleanup := setupHarnessRLS(t, true)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	// The caller's own company id (owned by h.ids.tenantID).
	var ownCompanyID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`SELECT company_id FROM stations WHERE tenant_id = $1 AND id = $2`,
		h.ids.tenantID, h.ids.station1).Scan(&ownCompanyID); err != nil {
		t.Fatalf("own company id: %v", err)
	}

	// Seed a SEPARATE tenant with its own company + station; clean it up after.
	otherTenant, otherCompany, otherStation := h.seedForeignTenant(t, ctx)
	// Purge the foreign tenant via the now-generic cleanupTenant (and assert zero
	// residual). This MUST run while the pool is still open: a t.Cleanup here would
	// fire AFTER the test's own `defer cleanup()` has already closed the pool
	// (t.Cleanup runs after all deferred calls return), making the purge a silent
	// no-op and leaking the whole foreign tenant tree. A defer registered AFTER
	// `defer cleanup()` runs LIFO — i.e. BEFORE cleanup() closes the pool — so the
	// purge actually lands; cleanupTenantNoResidual then proves nothing survived.
	defer cleanupTenantNoResidual(t, context.Background(), h.pool, otherTenant)

	// 1) A company scope_id owned by another tenant is rejected with 400.
	if code, body := h.invPostJSON(t, "/api/v1/enterprise/scope-grants", admin, map[string]any{
		"user_id": adminID.String(), "scope_type": "company", "scope_id": otherCompany.String(),
	}); code != http.StatusBadRequest {
		t.Fatalf("cross-tenant company scope grant = %d %v; want 400", code, body)
	}

	// 2) A station scope_id owned by another tenant is rejected with 400.
	if code, body := h.invPostJSON(t, "/api/v1/enterprise/scope-grants", admin, map[string]any{
		"user_id": adminID.String(), "scope_type": "station", "scope_id": otherStation.String(),
	}); code != http.StatusBadRequest {
		t.Fatalf("cross-tenant station scope grant = %d %v; want 400", code, body)
	}

	// 3) A wholly unknown scope_id is also rejected with 400.
	if code, body := h.invPostJSON(t, "/api/v1/enterprise/scope-grants", admin, map[string]any{
		"user_id": adminID.String(), "scope_type": "company", "scope_id": uuid.NewString(),
	}); code != http.StatusBadRequest {
		t.Fatalf("unknown company scope grant = %d %v; want 400", code, body)
	}

	// 4) A same-tenant company scope_id succeeds (201).
	if code, body := h.invPostJSON(t, "/api/v1/enterprise/scope-grants", admin, map[string]any{
		"user_id": adminID.String(), "scope_type": "company", "scope_id": ownCompanyID.String(),
	}); code != http.StatusCreated {
		t.Fatalf("same-tenant company scope grant = %d %v; want 201", code, body)
	}

	// 5) A same-tenant station scope_id succeeds (201).
	if code, body := h.invPostJSON(t, "/api/v1/enterprise/scope-grants", admin, map[string]any{
		"user_id": adminID.String(), "scope_type": "station", "scope_id": h.ids.station1.String(),
	}); code != http.StatusCreated {
		t.Fatalf("same-tenant station scope grant = %d %v; want 201", code, body)
	}

	// 6) A tenant scope (no scope_id) still succeeds (201) — the guard only
	// applies to non-tenant scopes.
	if code, body := h.invPostJSON(t, "/api/v1/enterprise/scope-grants", admin, map[string]any{
		"user_id": adminID.String(), "scope_type": "tenant",
	}); code != http.StatusCreated {
		t.Fatalf("tenant scope grant = %d %v; want 201", code, body)
	}
}

// seedForeignTenant inserts a second tenant with one company and one station,
// returning their ids. Used by SR-L4 to obtain scope_ids owned by a DIFFERENT
// tenant than the harness's caller.
func (h *harness) seedForeignTenant(t *testing.T, ctx context.Context) (tenantID, companyID, stationID uuid.UUID) {
	t.Helper()
	suffix := time.Now().UnixNano()
	q := func(dest *uuid.UUID, sql string, args ...any) {
		if err := h.pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seed foreign %q: %v", sql, err)
		}
	}
	q(&tenantID, `INSERT INTO tenants (name, slug) VALUES ('Other Co', $1) RETURNING id`,
		fmt.Sprintf("other-%d", suffix))
	q(&companyID, `INSERT INTO companies (tenant_id, name) VALUES ($1, 'Other Co') RETURNING id`, tenantID)
	q(&stationID, `INSERT INTO stations (tenant_id, company_id, name, code) VALUES ($1, $2, 'Other Station', 'OTH-01') RETURNING id`,
		tenantID, companyID)
	return tenantID, companyID, stationID
}
