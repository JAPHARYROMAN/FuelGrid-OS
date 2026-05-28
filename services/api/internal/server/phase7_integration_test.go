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

// TestPhase7_CashAndBanking covers Category B: a station day's cash is
// reconciled against Phase-6 expected cash (variance to over/short), grouped
// into a bank deposit (cash -> clearing -> bank), and a bank statement is
// imported and a fee line posted. Throughout, the trial balance stays balanced.
func TestPhase7_CashAndBanking(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	if code, _ := h.invPostJSON(t, "/api/v1/accounts/seed-defaults", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("seed chart: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/accounting-periods", admin,
		map[string]any{"start_date": "2026-06-01", "end_date": "2026-06-30"}); code != http.StatusCreated {
		t.Fatalf("create period: %d", code)
	}

	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`,
		h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("nozzle: %v", err)
	}

	// A closed shift with 50,000 of cash collected (Phase-6 tender).
	day, shift := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-05", 1000)
	if code, _ := h.invPostJSON(t, "/api/v1/shifts/"+shift.String()+"/payments", admin,
		map[string]any{"tender_type": "cash", "amount": "50000"}); code != http.StatusCreated {
		t.Fatalf("record cash: %d", code)
	}

	station := "/api/v1/stations/" + h.ids.station1.String()

	// Create the reconciliation: expected cash = 50,000 (sourced from tenders).
	code, cr := h.invPostJSON(t, station+"/cash-reconciliations", admin, map[string]any{"operating_day_id": day.String()})
	if code != http.StatusCreated || cr["expected_cash"] != "50000.00" {
		t.Fatalf("create cash recon = %d %v", code, cr)
	}
	crID := cr["id"].(string)
	// A second reconciliation for the same day is refused.
	if code, _ := h.invPostJSON(t, station+"/cash-reconciliations", admin, map[string]any{"operating_day_id": day.String()}); code != http.StatusConflict {
		t.Fatalf("duplicate reconciliation: %d, want 409", code)
	}

	// Submit a 49,500 count: a 500 short variance.
	code, sub := h.invPostJSON(t, "/api/v1/cash-reconciliations/"+crID+"/submit", admin, map[string]any{"counted_cash": "49500"})
	if code != http.StatusOK || sub["variance"] != "-500.00" || sub["status"] != "submitted" {
		t.Fatalf("submit = %d %v", code, sub)
	}

	// Approve posts a balanced entry and finalizes the reconciliation.
	if code, raw := h.do(t, http.MethodPost, "/api/v1/cash-reconciliations/"+crID+"/approve", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("approve = %d %s", code, raw)
	}
	if code, got := h.getJSON(t, "/api/v1/cash-reconciliations/"+crID, admin); code != http.StatusOK ||
		got["status"] != "posted" || got["journal_entry_id"] == nil {
		t.Fatalf("reconciliation after approve = %v", got)
	}

	// Open a bank account and deposit the counted cash.
	code, ba := h.invPostJSON(t, "/api/v1/bank-accounts", admin,
		map[string]any{"name": "Main Operating", "account_number": "0123456789"})
	if code != http.StatusCreated {
		t.Fatalf("bank account = %d %v", code, ba)
	}
	baID := ba["id"].(string)

	code, dep := h.invPostJSON(t, "/api/v1/bank-deposits", admin, map[string]any{
		"station_id": h.ids.station1.String(), "bank_account_id": baID,
		"slip_number": "SLP-1", "expected_bank_date": "2026-06-06",
		"lines": []map[string]any{{"cash_reconciliation_id": crID, "amount": "49500"}},
	})
	if code != http.StatusCreated {
		t.Fatalf("deposit = %d %v", code, dep)
	}
	depID := dep["id"].(string)

	// Prepare (cash -> clearing) then confirm (clearing -> bank).
	if code, raw := h.do(t, http.MethodPost, "/api/v1/bank-deposits/"+depID+"/prepare", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("prepare = %d %s", code, raw)
	}
	code, conf := h.invPostJSON(t, "/api/v1/bank-deposits/"+depID+"/confirm", admin,
		map[string]any{"actual_bank_date": "2026-06-07", "reference": "BANKREF-9"})
	if code != http.StatusOK || conf["status"] != "posted" || conf["amount"] != "49500.00" {
		t.Fatalf("confirm = %d %v", code, conf)
	}

	// The same reconciliation cannot be deposited twice.
	if code, _ := h.invPostJSON(t, "/api/v1/bank-deposits", admin, map[string]any{
		"station_id": h.ids.station1.String(), "bank_account_id": baID,
		"lines": []map[string]any{{"cash_reconciliation_id": crID, "amount": "49500"}},
	}); code != http.StatusConflict {
		t.Fatalf("double deposit: %d, want 409", code)
	}

	// Import a statement with one bank-fee line; a re-import is rejected.
	statement := map[string]any{
		"bank_account_id": baID, "statement_start": "2026-06-01", "statement_end": "2026-06-30",
		"lines": []map[string]any{{"txn_date": "2026-06-07", "amount": "-200", "description": "Monthly fee"}},
	}
	if code, imp := h.invPostJSON(t, "/api/v1/bank-statements/import", admin, statement); code != http.StatusCreated || imp["lines"].(float64) != 1 {
		t.Fatalf("import = %d %v", code, imp)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/bank-statements/import", admin, statement); code != http.StatusConflict {
		t.Fatalf("re-import: %d, want 409", code)
	}

	// The unmatched line can be posted as a bank fee.
	code, lines := h.getJSON(t, "/api/v1/bank-statement-lines?bank_account_id="+baID, admin)
	arr, _ := lines["items"].([]any)
	if code != http.StatusOK || len(arr) != 1 {
		t.Fatalf("list lines = %d %v", code, lines)
	}
	lineID := arr[0].(map[string]any)["id"].(string)
	if code, raw := h.do(t, http.MethodPost, "/api/v1/bank-statement-lines/"+lineID+"/bank-fee", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("bank fee = %d %s", code, raw)
	}

	// Despite cash short and a posted bank fee, the books still balance.
	if code, tb := h.getJSON(t, "/api/v1/finance/reports/trial-balance?as_of=2026-06-30", admin); code != http.StatusOK || !tb["balanced"].(bool) {
		t.Fatalf("trial balance not balanced: %v", tb)
	}
}
