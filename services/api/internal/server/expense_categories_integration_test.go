package server_test

import (
	"context"
	"net/http"
	"testing"
)

// TestExpenseCategory_ManageLifecycle proves the categories management surface
// (Feature 8.1): create with an accounting mapping + approval threshold, edit
// both, and activate/deactivate.
func TestExpenseCategory_ManageLifecycle(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	// Create with an explicit account mapping and approval threshold.
	code, body := h.invPostJSON(t, "/api/v1/expense-categories", admin, map[string]any{
		"name": "Utilities", "account_key": "utilities_expense", "approval_threshold": "5000",
	})
	if code != http.StatusCreated {
		t.Fatalf("create category: status %d: %v", code, body)
	}
	if body["account_key"].(string) != "utilities_expense" ||
		body["approval_threshold"].(string) != "5000.00" ||
		body["status"].(string) != "active" {
		t.Fatalf("created category = %v", body)
	}
	catID := body["id"].(string)

	// A duplicate name (case-insensitive) is a 409.
	if code, _ := h.invPostJSON(t, "/api/v1/expense-categories", admin, map[string]any{
		"name": "utilities",
	}); code != http.StatusConflict {
		t.Fatalf("duplicate name: status %d, want 409", code)
	}

	// Edit the mapping + threshold.
	code, body = h.patchJSON(t, "/api/v1/expense-categories/"+catID, admin,
		`{"name":"Utilities & Power","account_key":"power_expense","approval_threshold":"7500.50"}`)
	if code != http.StatusOK {
		t.Fatalf("update category: status %d: %v", code, body)
	}
	if body["name"].(string) != "Utilities & Power" ||
		body["account_key"].(string) != "power_expense" ||
		body["approval_threshold"].(string) != "7500.50" {
		t.Fatalf("updated category = %v", body)
	}

	// Deactivate (archive), then re-activate.
	code, body = h.invPostJSON(t, "/api/v1/expense-categories/"+catID+"/status", admin,
		map[string]any{"status": "archived"})
	if code != http.StatusOK || body["status"].(string) != "archived" {
		t.Fatalf("archive category: status %d: %v", code, body)
	}
	code, body = h.invPostJSON(t, "/api/v1/expense-categories/"+catID+"/status", admin,
		map[string]any{"status": "active"})
	if code != http.StatusOK || body["status"].(string) != "active" {
		t.Fatalf("activate category: status %d: %v", code, body)
	}

	// The list reflects the latest state.
	code, list := h.getJSON(t, "/api/v1/expense-categories", admin)
	if code != http.StatusOK {
		t.Fatalf("list categories: status %d: %v", code, list)
	}
	items, _ := list["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("expected at least one category, got %v", list)
	}
}

// TestExpenseCategory_Validation rejects a blank name, a negative threshold,
// and an unknown status.
func TestExpenseCategory_Validation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	if code, _ := h.invPostJSON(t, "/api/v1/expense-categories", admin, map[string]any{
		"name": "   ",
	}); code != http.StatusBadRequest {
		t.Fatalf("blank name: status %d, want 400", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/expense-categories", admin, map[string]any{
		"name": "Bad", "approval_threshold": "-1",
	}); code != http.StatusBadRequest {
		t.Fatalf("negative threshold: status %d, want 400", code)
	}

	code, body := h.invPostJSON(t, "/api/v1/expense-categories", admin, map[string]any{"name": "OK"})
	if code != http.StatusCreated {
		t.Fatalf("create: %d: %v", code, body)
	}
	catID := body["id"].(string)
	if code, _ := h.invPostJSON(t, "/api/v1/expense-categories/"+catID+"/status", admin,
		map[string]any{"status": "bogus"}); code != http.StatusBadRequest {
		t.Fatalf("bad status: status %d, want 400", code)
	}
}

// TestExpenseCategory_AttendantForbidden proves a freshly-created attendant
// cannot manage categories (403 on create), and the not-found path (404 on a
// missing category update by a privileged user).
func TestExpenseCategory_AttendantForbidden(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, admin := h.adminContext(t, ctx)

	const email = "cat-attendant@fuelgrid.local"
	seedAttendant(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, email)
	att := h.login(t, slug, email)

	if code, _ := h.invPostJSON(t, "/api/v1/expense-categories", att, map[string]any{
		"name": "Sneaky",
	}); code != http.StatusForbidden {
		t.Fatalf("attendant create category: status %d, want 403", code)
	}

	// A privileged user updating a missing category gets a 404.
	if code, _ := h.patchJSON(t, "/api/v1/expense-categories/00000000-0000-0000-0000-000000000000",
		admin, `{"name":"X"}`); code != http.StatusNotFound {
		t.Fatalf("update missing category: status %d, want 404", code)
	}
}
