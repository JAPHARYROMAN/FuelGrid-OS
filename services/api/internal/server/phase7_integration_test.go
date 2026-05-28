package server_test

// DB-backed integration tests for Phase 7 — Finance & Accounting Control.
// Reuses the Phase 2 harness + Phase 4/6 helpers. Gated on TEST_DATABASE_URL +
// TEST_REDIS_URL.

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
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

func TestPhase7_Payables(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	if code, _ := h.invPostJSON(t, "/api/v1/accounts/seed-defaults", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("seed chart: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/accounting-periods", admin,
		map[string]any{"start_date": "2026-06-01", "end_date": "2026-06-30"}); code != http.StatusCreated {
		t.Fatalf("create period: %d", code)
	}

	// No approved Phase-5 invoices yet -> import is a no-op.
	if code, imp := h.invPostJSON(t, "/api/v1/payables/import", admin, map[string]any{}); code != http.StatusOK || imp["imported"].(float64) != 0 {
		t.Fatalf("import (empty): %d %v", code, imp)
	}

	// Seed a payable directly (the table is decoupled from Phase-5 by design).
	supplierID := uuid.New()
	var payableID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO payables (tenant_id, supplier_id, source_invoice_id, invoice_number, invoice_date, due_date, amount, outstanding_amount, status)
		VALUES ($1, $2, $3, 'INV-1', '2026-06-05', '2026-07-05', 100000, 100000, 'open') RETURNING id
	`, h.ids.tenantID, supplierID, uuid.New()).Scan(&payableID); err != nil {
		t.Fatalf("seed payable: %v", err)
	}

	if code, list := h.getJSON(t, "/api/v1/payables", admin); code != http.StatusOK || list["count"].(float64) != 1 {
		t.Fatalf("list payables: %d %v", code, list)
	}

	// Pay 40,000 against it -> outstanding 60,000, partially_paid, balanced journal.
	code, pay := h.invPostJSON(t, "/api/v1/supplier-payments", admin, map[string]any{
		"supplier_id": supplierID.String(), "payment_date": "2026-06-10", "method": "bank",
		"allocations": []map[string]any{{"payable_id": payableID.String(), "amount": "40000"}},
	})
	if code != http.StatusCreated {
		t.Fatalf("supplier payment: %d %v", code, pay)
	}
	jid := pay["journal_entry_id"].(string)
	if code, je := h.getJSON(t, "/api/v1/journal-entries/"+jid, admin); code != http.StatusOK || je["total"] != "40000.00" {
		t.Fatalf("payment journal: %d %v", code, je)
	}

	if code, list := h.getJSON(t, "/api/v1/payables", admin); code == http.StatusOK {
		p := list["items"].([]any)[0].(map[string]any)
		if p["outstanding_amount"] != "60000.00" || p["status"] != "partially_paid" {
			t.Fatalf("payable after payment = %v", p)
		}
	}
	if code, aging := h.getJSON(t, "/api/v1/ap-aging", admin); code != http.StatusOK || aging["items"].([]any)[0].(map[string]any)["outstanding"] != "60000.00" {
		t.Fatalf("ap aging = %v", aging)
	}

	// Over-allocation (70,000 > 60,000 outstanding) is rejected.
	if code, _ := h.invPostJSON(t, "/api/v1/supplier-payments", admin, map[string]any{
		"supplier_id": supplierID.String(), "payment_date": "2026-06-11", "method": "bank",
		"allocations": []map[string]any{{"payable_id": payableID.String(), "amount": "70000"}},
	}); code != http.StatusUnprocessableEntity {
		t.Fatalf("over-allocation: %d, want 422", code)
	}
}

func TestPhase7_Reports(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	if code, _ := h.invPostJSON(t, "/api/v1/accounts/seed-defaults", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("seed chart: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/accounting-periods", admin,
		map[string]any{"start_date": "2026-06-01", "end_date": "2026-06-30"}); code != http.StatusCreated {
		t.Fatalf("create period: %d", code)
	}
	// Recognize 5,000 of cash sales: debit cash, credit sales revenue.
	if code, _ := h.invPostJSON(t, "/api/v1/journal-entries", admin, map[string]any{
		"entry_date": "2026-06-12", "memo": "cash sale",
		"lines": []map[string]any{
			{"system_key": "cash_on_hand", "debit": "5000", "credit": "0"},
			{"system_key": "sales_revenue", "debit": "0", "credit": "5000"},
		},
	}); code != http.StatusCreated {
		t.Fatalf("post entry: %d", code)
	}

	// Trial balance balances; P&L shows the revenue; balance sheet shows cash.
	if code, tb := h.getJSON(t, "/api/v1/finance/reports/trial-balance?as_of=2026-06-30", admin); code != http.StatusOK || !tb["balanced"].(bool) {
		t.Fatalf("trial balance not balanced: %v", tb)
	}
	if code, pl := h.getJSON(t, "/api/v1/finance/reports/profit-loss?from=2026-06-01&to=2026-06-30", admin); code != http.StatusOK ||
		pl["revenue"] != "5000.00" || pl["net_profit"] != "5000.00" {
		t.Fatalf("profit-loss = %v", pl)
	}
	if code, bsh := h.getJSON(t, "/api/v1/finance/reports/balance-sheet?as_of=2026-06-30", admin); code != http.StatusOK || bsh["assets"] != "5000.00" {
		t.Fatalf("balance sheet = %v", bsh)
	}
	if code, ov := h.getJSON(t, "/api/v1/finance/overview", admin); code != http.StatusOK || ov["balance_sheet"] == nil {
		t.Fatalf("finance overview = %v", ov)
	}
}
