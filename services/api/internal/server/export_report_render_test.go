package server

import (
	"strings"
	"testing"
)

// TestReportSpecFor_KeyToPermissionMapping pins the async export registry's
// key -> permission contract (Reports Center Phase 13, finding H3). The handler
// prefers the async path whenever reportSpecFor returns ok, so this mapping is
// load-bearing for BOTH the data served AND the permission enforced. A
// regression here (e.g. re-adding "inventory"/"delivery" to the reconciliation
// case) would silently change the data + authorization for back-compat callers.
func TestReportSpecFor_KeyToPermissionMapping(t *testing.T) {
	// Keys the worker DOES render, with their canonical permission.
	known := map[string]string{
		"revenue":                  "revenue.read",
		"station-close":            "revenue.read",
		"sales":                    "revenue.read",
		"reconciliation":           "reconciliation.read",
		"inventory-reconciliation": "reconciliation.read",
		"financials":               "finance.read",
		"ar-aging":                 "customer.read",
		"customer-aging":           "customer.read",
		"receivables":              "customer.read",
	}
	for key, wantPerm := range known {
		spec, ok := reportSpecFor(key)
		if !ok {
			t.Fatalf("reportSpecFor(%q) ok=false, want a spec", key)
		}
		if spec.perm != wantPerm {
			t.Fatalf("reportSpecFor(%q).perm = %q, want %q", key, spec.perm, wantPerm)
		}
	}

	// H3 PIN: "inventory" and "delivery" must NOT resolve to an async spec — they
	// deliberately fall through to the legacy receipt path so they keep mapping to
	// the inventory snapshot under inventory.read, NOT reconciliation data under
	// reconciliation.read. If this ever returns ok=true again the regression is
	// back (an inventory.read-only user would 403 and a reconciliation.read-only
	// user could pull reconciliation data via the inventory key).
	for _, key := range []string{"inventory", "delivery"} {
		if spec, ok := reportSpecFor(key); ok {
			t.Fatalf("reportSpecFor(%q) ok=true (perm=%q) — must fall through to the legacy path, not the reconciliation spec", key, spec.perm)
		}
	}
}

// TestBuildExportURL_InventoryDeliveryUnchanged pins the legacy fall-through the
// H3 fix relies on: with "inventory"/"delivery" removed from reportSpecFor, the
// handler routes them through buildExportURL, which must still map them to the
// station INVENTORY snapshot (inventory.csv, gated inventory.read at its route) —
// the pre-existing data + permission — never the reconciliation export.
func TestBuildExportURL_InventoryDeliveryUnchanged(t *testing.T) {
	const station = "11111111-1111-1111-1111-111111111111"
	filters := map[string]string{"station_id": station}

	// inventory CSV -> the inventory snapshot, NOT reconciliation.
	url, ok := buildExportURL(exportReportRequest{ReportKey: "inventory", Format: "csv", Filters: filters})
	if !ok {
		t.Fatalf("buildExportURL(inventory, csv) ok=false, want the inventory snapshot URL")
	}
	if !strings.Contains(url, "/reports/inventory.csv") {
		t.Fatalf("buildExportURL(inventory, csv) = %q, want the inventory.csv snapshot path", url)
	}
	if strings.Contains(url, "reconciliation") {
		t.Fatalf("buildExportURL(inventory, csv) = %q must NOT route to reconciliation (H3 regression)", url)
	}

	// delivery CSV reuses the inventory snapshot too (delivery facts post into the
	// same ledger) — still the inventory surface, not reconciliation.
	durl, ok := buildExportURL(exportReportRequest{ReportKey: "delivery", Format: "csv", Filters: filters})
	if !ok {
		t.Fatalf("buildExportURL(delivery, csv) ok=false, want the inventory snapshot URL")
	}
	if !strings.Contains(durl, "/reports/inventory.csv") {
		t.Fatalf("buildExportURL(delivery, csv) = %q, want the inventory.csv snapshot path", durl)
	}
}
