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

	// Seed the default chart of accounts (17 accounts).
	code, seeded := h.invPostJSON(t, "/api/v1/accounts/seed-defaults", admin, map[string]any{})
	if code != http.StatusOK || seeded["created"].(float64) != 17 {
		t.Fatalf("seed chart: %d %v", code, seeded)
	}
	if code, acc := h.getJSON(t, "/api/v1/accounts", admin); code != http.StatusOK || acc["count"].(float64) != 17 {
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

// TestPhase7_Receivables covers Category D: a customer invoice is issued to AR
// (debit AR, credit revenue), aged, and drawn down by an allocated customer
// payment (debit bank, credit AR). Over-allocation is refused; the books stay
// balanced throughout.
func TestPhase7_Receivables(t *testing.T) {
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

	// A finance customer.
	code, cust := h.invPostJSON(t, "/api/v1/customers", admin,
		map[string]any{"code": "FLEET", "name": "Fleet Co", "credit_limit": "0"})
	if code != http.StatusCreated {
		t.Fatalf("create customer: %d %v", code, cust)
	}
	custID := cust["id"].(string)

	// A draft invoice with two lines totals 5,000.
	code, inv := h.invPostJSON(t, "/api/v1/customer-invoices", admin, map[string]any{
		"customer_id": custID, "invoice_number": "INV-1", "invoice_date": "2026-06-10", "due_date": "2026-06-25",
		"lines": []map[string]any{
			{"description": "Diesel - June wk1", "amount": "3000"},
			{"description": "Diesel - June wk2", "amount": "2000"},
		},
	})
	if code != http.StatusCreated || inv["amount"] != "5000.00" || inv["status"] != "draft" {
		t.Fatalf("create invoice = %d %v", code, inv)
	}
	invID := inv["id"].(string)

	// Issuing posts to AR and shows in aging.
	code, issued := h.do(t, http.MethodPost, "/api/v1/customer-invoices/"+invID+"/issue", admin, nil, "")
	if code != http.StatusOK {
		t.Fatalf("issue = %d %s", code, issued)
	}
	if code, got := h.getJSON(t, "/api/v1/customer-invoices/"+invID, admin); code != http.StatusOK ||
		got["status"] != "issued" || got["journal_entry_id"] == nil {
		t.Fatalf("invoice after issue = %v", got)
	}
	if code, aging := h.getJSON(t, "/api/v1/customer-invoices-aging", admin); code != http.StatusOK ||
		len(aging["items"].([]any)) != 1 {
		t.Fatalf("invoice aging = %v", aging)
	}

	// A 2,000 payment leaves the invoice partially paid.
	code, _ = h.invPostJSON(t, "/api/v1/customer-payments", admin, map[string]any{
		"customer_id": custID, "payment_date": "2026-06-15", "method": "bank_transfer", "source_account_key": "bank",
		"allocations": []map[string]any{{"customer_invoice_id": invID, "amount": "2000"}},
	})
	if code != http.StatusCreated {
		t.Fatalf("first payment: %d", code)
	}
	if code, got := h.getJSON(t, "/api/v1/customer-invoices/"+invID, admin); code != http.StatusOK ||
		got["status"] != "partially_paid" || got["outstanding_amount"] != "3000.00" {
		t.Fatalf("invoice after partial payment = %v", got)
	}

	// Over-allocating the remaining 3,000 is refused.
	if code, _ := h.invPostJSON(t, "/api/v1/customer-payments", admin, map[string]any{
		"customer_id": custID, "payment_date": "2026-06-16", "method": "cash", "source_account_key": "cash_on_hand",
		"allocations": []map[string]any{{"customer_invoice_id": invID, "amount": "4000"}},
	}); code != http.StatusUnprocessableEntity {
		t.Fatalf("over-allocation: %d, want 422", code)
	}

	// Paying the remaining 3,000 settles the invoice.
	if code, _ := h.invPostJSON(t, "/api/v1/customer-payments", admin, map[string]any{
		"customer_id": custID, "payment_date": "2026-06-16", "method": "cash", "source_account_key": "cash_on_hand",
		"allocations": []map[string]any{{"customer_invoice_id": invID, "amount": "3000"}},
	}); code != http.StatusCreated {
		t.Fatalf("final payment: %d", code)
	}
	if code, got := h.getJSON(t, "/api/v1/customer-invoices/"+invID, admin); code != http.StatusOK ||
		got["status"] != "paid" || got["outstanding_amount"] != "0.00" {
		t.Fatalf("invoice after full payment = %v", got)
	}

	if code, tb := h.getJSON(t, "/api/v1/finance/reports/trial-balance?as_of=2026-06-30", admin); code != http.StatusOK || !tb["balanced"].(bool) {
		t.Fatalf("trial balance not balanced: %v", tb)
	}
}

// secondApprover creates a second active user (copying the admin's password
// hash so the shared test password logs it in) with the system_admin role and
// returns a session token. Used to satisfy separation-of-duties guards where
// the approver must differ from the creator.
func (h *harness) secondApprover(t *testing.T, ctx context.Context, slug string) string {
	t.Helper()
	const email = "approver@fuelgrid.local"
	var id string
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		SELECT tenant_id, $2, 'Second Approver', 'active', password_hash, now()
		FROM users WHERE tenant_id = $1 AND email = $3
		RETURNING id
	`, h.ids.tenantID, email, h.ids.adminEmail).Scan(&id); err != nil {
		t.Fatalf("create second approver: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id, tenant_id)
		SELECT $1, id, $2 FROM roles WHERE code = 'system_admin' AND is_system
	`, id, h.ids.tenantID); err != nil {
		t.Fatalf("grant second approver role: %v", err)
	}
	return h.login(t, slug, email)
}

// TestPhase7_ExpensesAndPettyCash covers Category E: an expense flows
// draft -> submitted -> approved -> posted (debit expense, credit cash), and a
// petty-cash float is topped up, spent against, guarded from overdraw, and
// reconciled with the variance posting to cash over/short.
func TestPhase7_ExpensesAndPettyCash(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, slug, admin := h.adminContext(t, ctx)

	if code, _ := h.invPostJSON(t, "/api/v1/accounts/seed-defaults", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("seed chart: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/accounting-periods", admin,
		map[string]any{"start_date": "2026-06-01", "end_date": "2026-06-30"}); code != http.StatusCreated {
		t.Fatalf("create period: %d", code)
	}

	station := h.ids.station1.String()

	// --- Expense: draft -> submitted -> approved -> posted ---
	code, exp := h.invPostJSON(t, "/api/v1/expenses", admin, map[string]any{
		"station_id": station, "payee": "City Power", "expense_date": "2026-06-08",
		"amount": "1200", "payment_mode": "cash", "reference": "UTIL-06",
	})
	if code != http.StatusCreated || exp["status"] != "draft" || exp["amount"] != "1200.00" {
		t.Fatalf("create expense = %d %v", code, exp)
	}
	expID := exp["id"].(string)
	// Posting before approval is refused.
	if code, _ := h.do(t, http.MethodPost, "/api/v1/expenses/"+expID+"/post", admin, nil, ""); code != http.StatusConflict {
		t.Fatalf("post before approve: %d, want 409", code)
	}
	// Submit by the creator.
	if code, raw := h.do(t, http.MethodPost, "/api/v1/expenses/"+expID+"/submit", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("expense submit: %d %s", code, raw)
	}
	// Separation of duties: the creator cannot approve their own expense.
	if code, _ := h.do(t, http.MethodPost, "/api/v1/expenses/"+expID+"/approve", admin, nil, ""); code != http.StatusForbidden {
		t.Fatalf("self-approve should be 403, got %d", code)
	}
	// A different user with approval rights can approve it.
	approver := h.secondApprover(t, ctx, slug)
	if code, raw := h.do(t, http.MethodPost, "/api/v1/expenses/"+expID+"/approve", approver, nil, ""); code != http.StatusOK {
		t.Fatalf("expense approve by second user: %d %s", code, raw)
	}
	// Post by the creator (posting is not an approval step).
	if code, raw := h.do(t, http.MethodPost, "/api/v1/expenses/"+expID+"/post", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("expense post: %d %s", code, raw)
	}
	if code, got := h.getJSON(t, "/api/v1/expenses/"+expID, admin); code != http.StatusOK ||
		got["status"] != "posted" || got["journal_entry_id"] == nil {
		t.Fatalf("expense after post = %v", got)
	}

	// --- Petty cash: top up, spend, overdraw guard, reconcile ---
	code, fl := h.invPostJSON(t, "/api/v1/petty-cash-floats", admin,
		map[string]any{"station_id": station, "name": "Front desk float"})
	if code != http.StatusCreated {
		t.Fatalf("create float = %d %v", code, fl)
	}
	floatID := fl["id"].(string)

	// Top up 10,000.
	code, top := h.invPostJSON(t, "/api/v1/petty-cash-floats/"+floatID+"/transactions", admin,
		map[string]any{"txn_type": "topup", "amount": "10000", "date": "2026-06-01"})
	if code != http.StatusCreated || top["balance_after"] != "10000.00" {
		t.Fatalf("topup = %d %v", code, top)
	}
	// Spend 1,500 on supplies.
	code, spend := h.invPostJSON(t, "/api/v1/petty-cash-floats/"+floatID+"/transactions", admin,
		map[string]any{"txn_type": "spend", "amount": "1500", "date": "2026-06-05", "description": "Cleaning supplies"})
	if code != http.StatusCreated || spend["balance_after"] != "8500.00" {
		t.Fatalf("spend = %d %v", code, spend)
	}
	// Overdrawing is refused without an override.
	if code, _ := h.invPostJSON(t, "/api/v1/petty-cash-floats/"+floatID+"/transactions", admin,
		map[string]any{"txn_type": "spend", "amount": "100000", "date": "2026-06-06"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("overdraw: %d, want 422", code)
	}
	// Reconcile against a count of 8,400: a 100 shortfall to cash over/short.
	code, rec := h.invPostJSON(t, "/api/v1/petty-cash-floats/"+floatID+"/reconcile", admin,
		map[string]any{"counted_cash": "8400", "date": "2026-06-10"})
	if code != http.StatusOK || rec["variance"] != "-100.00" {
		t.Fatalf("reconcile = %d %v", code, rec)
	}
	if code, got := h.getJSON(t, "/api/v1/petty-cash-floats/"+floatID, admin); code != http.StatusOK || got["balance"] != "8400.00" {
		t.Fatalf("float after reconcile = %v", got)
	}

	if code, tb := h.getJSON(t, "/api/v1/finance/reports/trial-balance?as_of=2026-06-30", admin); code != http.StatusOK || !tb["balanced"].(bool) {
		t.Fatalf("trial balance not balanced: %v", tb)
	}
}

// TestPhase7_ExportsAndClose covers Stage 14 (accounting exports) and the
// Stage 16 close checklist: a trial-balance export over a locked period is
// final and reproducible (same checksum on re-run), and the close checklist
// reports no blockers for a clean tenant.
func TestPhase7_ExportsAndClose(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	if code, _ := h.invPostJSON(t, "/api/v1/accounts/seed-defaults", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("seed chart: %d", code)
	}
	code, period := h.invPostJSON(t, "/api/v1/accounting-periods", admin,
		map[string]any{"start_date": "2026-06-01", "end_date": "2026-06-30"})
	if code != http.StatusCreated {
		t.Fatalf("create period: %d", code)
	}
	periodID := period["id"].(string)

	// Post a balanced cash sale, then lock the period.
	if code, _ := h.invPostJSON(t, "/api/v1/journal-entries", admin, map[string]any{
		"entry_date": "2026-06-12", "memo": "cash sale",
		"lines": []map[string]any{
			{"system_key": "cash_on_hand", "debit": "5000", "credit": "0"},
			{"system_key": "sales_revenue", "debit": "0", "credit": "5000"},
		},
	}); code != http.StatusCreated {
		t.Fatalf("post entry: %d", code)
	}
	for _, action := range []string{"start-close", "close", "lock"} {
		if code, raw := h.do(t, http.MethodPost, "/api/v1/accounting-periods/"+periodID+"/"+action, admin, nil, ""); code != http.StatusOK {
			t.Fatalf("period %s: %d %s", action, code, raw)
		}
	}

	// Export the trial balance for the locked period: final + reproducible.
	code, exp1 := h.invPostJSON(t, "/api/v1/finance/exports/trial-balance?as_of=2026-06-30", admin, map[string]any{})
	if code != http.StatusCreated || exp1["provisional"].(bool) || exp1["row_count"].(float64) != 2 {
		t.Fatalf("export 1 = %d %v", code, exp1)
	}
	code, exp2 := h.invPostJSON(t, "/api/v1/finance/exports/trial-balance?as_of=2026-06-30", admin, map[string]any{})
	if code != http.StatusCreated || exp1["checksum"] != exp2["checksum"] {
		t.Fatalf("re-export checksum mismatch: %v vs %v", exp1["checksum"], exp2["checksum"])
	}
	// Both runs are recorded.
	if code, runs := h.getJSON(t, "/api/v1/finance/exports", admin); code != http.StatusOK || runs["count"].(float64) != 2 {
		t.Fatalf("export runs = %v", runs)
	}

	// A journal export over an unlocked range would be provisional; here the
	// only period is locked, so the close checklist reports no blockers.
	if code, cl := h.getJSON(t, "/api/v1/finance/close-checklist", admin); code != http.StatusOK || !cl["can_close"].(bool) {
		t.Fatalf("close checklist = %d %v", code, cl)
	}
}
