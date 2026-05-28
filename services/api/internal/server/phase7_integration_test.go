package server_test

// DB-backed integration tests for Phase 7 — Finance & Accounting Control.
// Reuses the Phase 2 harness + Phase 4/6 helpers. Gated on TEST_DATABASE_URL +
// TEST_REDIS_URL.

import (
	"context"
	"net/http"
	"testing"
)

func TestPhase7_AccountingFoundation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	// Seed the default chart of accounts (16 accounts).
	code, seeded := h.invPostJSON(t, "/api/v1/accounts/seed-defaults", admin, map[string]any{})
	if code != http.StatusOK || seeded["created"].(float64) != 16 {
		t.Fatalf("seed chart: %d %v", code, seeded)
	}
	if code, acc := h.getJSON(t, "/api/v1/accounts", admin); code != http.StatusOK || acc["count"].(float64) != 16 {
		t.Fatalf("list accounts: %d count %v", code, acc["count"])
	}

	// Create a June period; an overlapping one is refused.
	code, period := h.invPostJSON(t, "/api/v1/accounting-periods", admin,
		map[string]any{"start_date": "2026-06-01", "end_date": "2026-06-30"})
	if code != http.StatusCreated {
		t.Fatalf("create period: %d %v", code, period)
	}
	periodID := period["id"].(string)
	if code, _ := h.invPostJSON(t, "/api/v1/accounting-periods", admin,
		map[string]any{"start_date": "2026-06-15", "end_date": "2026-07-15"}); code != http.StatusConflict {
		t.Fatalf("overlapping period: %d, want 409", code)
	}

	// A balanced adjustment posts; an unbalanced one is rejected.
	balanced := map[string]any{
		"entry_date": "2026-06-10", "memo": "opening cash",
		"lines": []map[string]any{
			{"system_key": "cash_on_hand", "debit": "1000", "credit": "0"},
			{"system_key": "sales_clearing", "debit": "0", "credit": "1000"},
		},
	}
	code, entry := h.invPostJSON(t, "/api/v1/journal-entries", admin, balanced)
	if code != http.StatusCreated || entry["status"] != "posted" || entry["total"] != "1000.00" {
		t.Fatalf("post adjustment: %d %v", code, entry)
	}
	entryID := entry["id"].(string)
	if code, got := h.getJSON(t, "/api/v1/journal-entries/"+entryID, admin); code != http.StatusOK {
		t.Fatalf("get entry after post: %d %v", code, got)
	}

	if code, _ := h.invPostJSON(t, "/api/v1/journal-entries", admin, map[string]any{
		"entry_date": "2026-06-10",
		"lines": []map[string]any{
			{"system_key": "cash_on_hand", "debit": "1000", "credit": "0"},
			{"system_key": "sales_clearing", "debit": "0", "credit": "500"},
		},
	}); code != http.StatusUnprocessableEntity {
		t.Fatalf("unbalanced entry: %d, want 422", code)
	}

	// Reverse the posted entry; the original becomes 'reversed'.
	code, rev := h.do(t, http.MethodPost, "/api/v1/journal-entries/"+entryID+"/reverse", admin, nil, "")
	if code != http.StatusCreated {
		t.Fatalf("reverse: %d %s", code, rev)
	}
	if code, orig := h.getJSON(t, "/api/v1/journal-entries/"+entryID, admin); code != http.StatusOK || orig["status"] != "reversed" {
		t.Fatalf("original after reverse = %v", orig)
	}

	// Re-reversing is rejected.
	if code, _ := h.do(t, http.MethodPost, "/api/v1/journal-entries/"+entryID+"/reverse", admin, nil, ""); code != http.StatusConflict {
		t.Fatalf("re-reverse: %d, want 409", code)
	}

	// Lock the period (open -> closing -> closed -> locked); posting into a
	// locked period is then refused.
	for _, action := range []string{"start-close", "close", "lock"} {
		if code, raw := h.do(t, http.MethodPost, "/api/v1/accounting-periods/"+periodID+"/"+action, admin, nil, ""); code != http.StatusOK {
			t.Fatalf("period %s: %d %s", action, code, raw)
		}
	}
	if code, _ := h.invPostJSON(t, "/api/v1/journal-entries", admin, balanced); code != http.StatusConflict {
		t.Fatalf("post into locked period: %d, want 409", code)
	}
}
