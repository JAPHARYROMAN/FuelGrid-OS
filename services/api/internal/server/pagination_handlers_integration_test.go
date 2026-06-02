package server_test

// DB-backed integration coverage for the W6-REL-HANDLERS pagination work: the
// list endpoints that gained the shared parsePage helper + writePaged envelope.
// Reuses the Phase 2 harness; gated on TEST_DATABASE_URL + TEST_REDIS_URL.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5433/fuelgrid?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6379/0 \
//	go test ./services/api/internal/server -run Pagination -v

import (
	"context"
	"net/http"
	"testing"
)

// TestPagination_ListEmployeesEnvelope proves a newly-paged handler
// (handleListEmployees) returns the {items,count,limit,offset,has_more}
// envelope and that limit/offset walk the result set without overlap.
func TestPagination_ListEmployeesEnvelope(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	ctx := context.Background()
	// Seed three employees at station1 (names chosen so the full_name ASC,
	// id ASC order is deterministic: Aaa, Bbb, Ccc).
	for _, name := range []string{"Aaa Attendant", "Bbb Attendant", "Ccc Attendant"} {
		if _, err := h.pool.Exec(ctx, `
			INSERT INTO employees (tenant_id, station_id, full_name, role)
			VALUES ($1, $2, $3, 'pump_attendant')`,
			h.ids.tenantID, h.ids.station1, name); err != nil {
			t.Fatalf("seed employee %q: %v", name, err)
		}
	}

	base := "/api/v1/stations/" + h.ids.station1.String() + "/employees"

	// First page: limit=2 -> two items, has_more true (a third row exists).
	code, page1 := h.getJSON(t, base+"?limit=2&offset=0", admin)
	if code != http.StatusOK {
		t.Fatalf("page1 status = %d (want 200)", code)
	}
	if got := countOf(page1); got != 2 {
		t.Fatalf("page1 count = %d (want 2)", got)
	}
	if hm, _ := page1["has_more"].(bool); !hm {
		t.Fatalf("page1 has_more = %v (want true)", page1["has_more"])
	}
	if lim, _ := page1["limit"].(float64); int(lim) != 2 {
		t.Fatalf("page1 limit = %v (want 2)", page1["limit"])
	}
	if off, _ := page1["offset"].(float64); int(off) != 0 {
		t.Fatalf("page1 offset = %v (want 0)", page1["offset"])
	}

	// Second page: offset=2 -> the remaining single item, has_more false.
	code, page2 := h.getJSON(t, base+"?limit=2&offset=2", admin)
	if code != http.StatusOK {
		t.Fatalf("page2 status = %d (want 200)", code)
	}
	if got := countOf(page2); got != 1 {
		t.Fatalf("page2 count = %d (want 1)", got)
	}
	if hm, _ := page2["has_more"].(bool); hm {
		t.Fatalf("page2 has_more = %v (want false)", page2["has_more"])
	}

	// The two windows must not overlap (stable ordering across pages).
	first := page1["items"].([]any)
	second := page2["items"].([]any)
	firstNames := map[string]bool{}
	for _, it := range first {
		firstNames[it.(map[string]any)["full_name"].(string)] = true
	}
	for _, it := range second {
		name := it.(map[string]any)["full_name"].(string)
		if firstNames[name] {
			t.Fatalf("employee %q appeared on both pages — unstable paging", name)
		}
	}

	// A garbage limit is rejected by the shared helper with a 400.
	if code, _ := h.getJSON(t, base+"?limit=abc", admin); code != http.StatusBadRequest {
		t.Fatalf("garbage limit status = %d (want 400)", code)
	}
}
