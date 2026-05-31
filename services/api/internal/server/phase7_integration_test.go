package server_test

// DB-backed integration tests for Phase 7 — Finance & Accounting Control.
// Reuses the Phase 2 harness + Phase 4/6 helpers. Gated on TEST_DATABASE_URL +
// TEST_REDIS_URL.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/accounting"
	"github.com/japharyroman/fuelgrid-os/internal/revenue"
)

func TestPhase7_AccountingFoundation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	// Seed the default chart of accounts (17 accounts).
	code, seeded := h.invPostJSON(t, "/api/v1/accounts/seed-defaults", admin, map[string]any{})
	if code != http.StatusOK || seeded["created"].(float64) != 18 {
		t.Fatalf("seed chart: %d %v", code, seeded)
	}
	if code, acc := h.getJSON(t, "/api/v1/accounts", admin); code != http.StatusOK || acc["count"].(float64) != 18 {
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

	// Seed a payable directly (the table is decoupled from Phase-5 by design),
	// but against a real supplier so the AP-ledger FKs (DB-001) hold.
	supplierID := uuid.New()
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO suppliers (id, tenant_id, code, name) VALUES ($1, $2, 'PAY-SUP-1', 'Payables Test Supplier')`,
		supplierID, h.ids.tenantID); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
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

// TestPhase7_PeriodCloseBlockedByChecklist covers ACCT-004: a period cannot be
// closed while the close checklist has unresolved blockers (here, a draft
// expense awaiting posting). Clearing the blocker lets the close proceed.
func TestPhase7_PeriodCloseBlockedByChecklist(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	var adminID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, h.ids.adminEmail).Scan(&adminID); err != nil {
		t.Fatalf("lookup admin id: %v", err)
	}

	code, period := h.invPostJSON(t, "/api/v1/accounting-periods", admin,
		map[string]any{"start_date": "2026-06-01", "end_date": "2026-06-30"})
	if code != http.StatusCreated {
		t.Fatalf("create period: %d %v", code, period)
	}
	periodID := period["id"].(string)
	if code, _ := h.do(t, http.MethodPost, "/api/v1/accounting-periods/"+periodID+"/start-close", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("start-close: %d", code)
	}

	// A draft expense is a close blocker.
	var expenseID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO expenses (tenant_id, amount, created_by) VALUES ($1, 100, $2) RETURNING id`,
		h.ids.tenantID, adminID).Scan(&expenseID); err != nil {
		t.Fatalf("seed draft expense: %v", err)
	}
	if code, _ := h.do(t, http.MethodPost, "/api/v1/accounting-periods/"+periodID+"/close", admin, nil, ""); code != http.StatusUnprocessableEntity {
		t.Fatalf("close with blocker = %d, want 422", code)
	}

	// Clear the blocker; the close now succeeds.
	if _, err := h.pool.Exec(ctx, `DELETE FROM expenses WHERE tenant_id = $1 AND id = $2`, h.ids.tenantID, expenseID); err != nil {
		t.Fatalf("clear expense: %v", err)
	}
	if code, _ := h.do(t, http.MethodPost, "/api/v1/accounting-periods/"+periodID+"/close", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("close after clearing blocker = %d, want 200", code)
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
	// Pay 2,000 of operating expense in cash: debit expense, credit cash. This
	// makes net income 3,000 (5,000 revenue − 2,000 expense), so equity that
	// omits net income would NOT equal assets − liabilities.
	if code, _ := h.invPostJSON(t, "/api/v1/journal-entries", admin, map[string]any{
		"entry_date": "2026-06-13", "memo": "operating expense",
		"lines": []map[string]any{
			{"system_key": "operating_expense", "debit": "2000", "credit": "0"},
			{"system_key": "cash_on_hand", "debit": "0", "credit": "2000"},
		},
	}); code != http.StatusCreated {
		t.Fatalf("post expense entry: %d", code)
	}

	// Trial balance balances; P&L shows revenue net of the expense.
	if code, tb := h.getJSON(t, "/api/v1/finance/reports/trial-balance?as_of=2026-06-30", admin); code != http.StatusOK || !tb["balanced"].(bool) {
		t.Fatalf("trial balance not balanced: %v", tb)
	}
	if code, pl := h.getJSON(t, "/api/v1/finance/reports/profit-loss?from=2026-06-01&to=2026-06-30", admin); code != http.StatusOK ||
		pl["revenue"] != "5000.00" || pl["expenses"] != "2000.00" || pl["net_profit"] != "3000.00" {
		t.Fatalf("profit-loss = %v", pl)
	}
	// Balance sheet: assets = 3,000 cash (5,000 in − 2,000 out). Equity must
	// fold in the 3,000 net income (retained earnings is 0), so the accounting
	// identity holds exactly: assets == liabilities + equity. The old code,
	// which only summed equity-type accounts, returned equity 0 and would fail
	// the balanced assertion below (3,000 != 0 + 0).
	code, bsh := h.getJSON(t, "/api/v1/finance/reports/balance-sheet?as_of=2026-06-30", admin)
	if code != http.StatusOK {
		t.Fatalf("balance sheet: %d %v", code, bsh)
	}
	if bsh["assets"] != "3000.00" || bsh["liabilities"] != "0.00" {
		t.Fatalf("balance sheet totals = %v", bsh)
	}
	if bsh["equity"] != "3000.00" || bsh["net_income"] != "3000.00" {
		t.Fatalf("equity must include net income, got equity=%v net_income=%v", bsh["equity"], bsh["net_income"])
	}
	if bsh["balanced"] != true {
		t.Fatalf("balance sheet not balanced: %v", bsh)
	}
	// Assert the identity directly on the decimal strings (cent-exact).
	assets := bsh["assets"].(string)
	liabilities := bsh["liabilities"].(string)
	equity := bsh["equity"].(string)
	if !sumDecimalEq(t, assets, liabilities, equity) {
		t.Fatalf("assets %s != liabilities %s + equity %s", assets, liabilities, equity)
	}
	if code, ov := h.getJSON(t, "/api/v1/finance/overview", admin); code != http.StatusOK || ov["balance_sheet"] == nil {
		t.Fatalf("finance overview = %v", ov)
	}
}

// sumDecimalEq reports whether assets == liabilities + equity, comparing the
// numeric(14,2) decimal strings to the cent without float arithmetic.
func sumDecimalEq(t *testing.T, assets, liabilities, equity string) bool {
	t.Helper()
	return decimalCents(t, assets) == decimalCents(t, liabilities)+decimalCents(t, equity)
}

// decimalCents parses a "-?digits.dd" money string into integer cents.
func decimalCents(t *testing.T, s string) int64 {
	t.Helper()
	neg := false
	if len(s) > 0 && s[0] == '-' {
		neg = true
		s = s[1:]
	}
	whole, frac := s, "00"
	if dot := indexByte(s, '.'); dot >= 0 {
		whole, frac = s[:dot], s[dot+1:]
	}
	for len(frac) < 2 {
		frac += "0"
	}
	frac = frac[:2]
	var cents int64
	for _, c := range whole {
		if c < '0' || c > '9' {
			t.Fatalf("bad money string %q", s)
		}
		cents = cents*10 + int64(c-'0')
	}
	cents *= 100
	cents += int64(frac[0]-'0')*10 + int64(frac[1]-'0')
	if neg {
		cents = -cents
	}
	return cents
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// TestPhase7_CashAndBanking covers Category B: a station day's cash is
// reconciled against Phase-6 expected cash (variance to over/short), grouped
// into a bank deposit (cash -> clearing -> bank), and a bank statement is
// imported and a fee line posted. Throughout, the trial balance stays balanced.
func TestPhase7_CashAndBanking(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, slug, admin := h.adminContext(t, ctx)

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

	// Separation of duties: the submitter (admin) cannot approve their own recon.
	if code, _ := h.do(t, http.MethodPost, "/api/v1/cash-reconciliations/"+crID+"/approve", admin, nil, ""); code != http.StatusForbidden {
		t.Fatalf("self-approve recon should be 403, got %d", code)
	}
	// Approve by a different user posts a balanced entry and finalizes it.
	approver := h.secondApprover(t, ctx, slug)
	if code, raw := h.do(t, http.MethodPost, "/api/v1/cash-reconciliations/"+crID+"/approve", approver, nil, ""); code != http.StatusOK {
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

// TestPhase7_JournalBalanceTrigger proves the DB-level double-entry guard
// (migration 0064 / audit ACCT-001): a directly-written unbalanced journal
// entry must be rejected at COMMIT by the deferred constraint trigger, even
// though it bypasses the Go-layer balance check.
func TestPhase7_JournalBalanceTrigger(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	if code, _ := h.invPostJSON(t, "/api/v1/accounts/seed-defaults", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("seed chart: %d", code)
	}
	code, period := h.invPostJSON(t, "/api/v1/accounting-periods", admin,
		map[string]any{"start_date": "2026-09-01", "end_date": "2026-09-30"})
	if code != http.StatusCreated {
		t.Fatalf("create period: %d", code)
	}
	periodID := period["id"].(string)

	var accountID string
	if err := h.pool.QueryRow(ctx, `SELECT id FROM accounts WHERE tenant_id = $1 LIMIT 1`, h.ids.tenantID).Scan(&accountID); err != nil {
		t.Fatalf("account: %v", err)
	}

	// Write an unbalanced entry directly (debit 100, no offsetting credit),
	// bypassing the Go balance check. The deferred trigger must abort COMMIT.
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	var entryID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO journal_entries (tenant_id, period_id, entry_date, source_type, posted_by)
		VALUES ($1, $2, '2026-09-15', 'adjustment', $3) RETURNING id
	`, h.ids.tenantID, periodID, adminID).Scan(&entryID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert entry: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO journal_lines (tenant_id, journal_entry_id, account_id, debit, credit)
		VALUES ($1, $2, $3, 100.00, 0)
	`, h.ids.tenantID, entryID, accountID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert line: %v", err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("expected COMMIT to fail: an unbalanced journal entry was accepted by the database")
	}
}

// TestPhase7_ShiftRevenueJournal proves PAY-013: an approved shift's recognized
// sales post a balanced GL revenue entry (DR sales_clearing / CR sales_revenue /
// CR output_vat), revenue reaches the P&L, and re-posting is idempotent. This is
// the posting the RevenueRecognized outbox consumer drives in production.
func TestPhase7_ShiftRevenueJournal(t *testing.T) {
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
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id = $1 AND tank_id = $2 LIMIT 1`,
		h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("nozzle: %v", err)
	}
	day, shift := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-12", 2000)

	// A recognized sale: 2,000 L @ 2.95 = 5,900 gross; 18% tax-inclusive -> net
	// 5,000, tax 900.
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO sales (tenant_id, shift_id, station_id, operating_day_id, nozzle_id, product_id, tank_id,
		    litres, unit_price, gross_amount, tax_rate, tax_amount, net_amount, recorded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 2000, 2.95, 5900, 18, 900, 5000, $8)
	`, h.ids.tenantID, shift, h.ids.station1, day, nozzleID, h.ids.pmsProduct, h.ids.tankPMS, adminID); err != nil {
		t.Fatalf("seed sale: %v", err)
	}

	acct := accounting.New(h.pool)
	rev := revenue.New(h.pool)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	entry, posted, err := rev.PostShiftRevenueJournal(ctx, tx, acct, h.ids.tenantID, shift, adminID)
	if err != nil || !posted {
		_ = tx.Rollback(ctx)
		t.Fatalf("post revenue journal: posted=%v err=%v", posted, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// The entry's lines: DR sales_clearing 5900, CR sales_revenue 5000, CR output_vat 900.
	rows, err := h.pool.Query(ctx, `
		SELECT a.system_key, jl.debit::text, jl.credit::text
		FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
		WHERE jl.tenant_id = $1 AND jl.journal_entry_id = $2
	`, h.ids.tenantID, entry.ID)
	if err != nil {
		t.Fatalf("lines: %v", err)
	}
	defer rows.Close()
	got := map[string][2]string{}
	for rows.Next() {
		var k, d, c string
		if err := rows.Scan(&k, &d, &c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[k] = [2]string{d, c}
	}
	if got["sales_clearing"] != [2]string{"5900.00", "0.00"} {
		t.Fatalf("sales_clearing = %v, want DR 5900.00", got["sales_clearing"])
	}
	if got["sales_revenue"] != [2]string{"0.00", "5000.00"} {
		t.Fatalf("sales_revenue = %v, want CR 5000.00", got["sales_revenue"])
	}
	if got["output_vat"] != [2]string{"0.00", "900.00"} {
		t.Fatalf("output_vat = %v, want CR 900.00", got["output_vat"])
	}

	// Revenue now reaches the P&L.
	if code, pl := h.getJSON(t, "/api/v1/finance/reports/profit-loss?from=2026-06-01&to=2026-06-30", admin); code != http.StatusOK ||
		pl["revenue"] != "5000.00" {
		t.Fatalf("p&l revenue = %v (status %d)", pl, code)
	}

	// Idempotent: a redelivered event must not double-post.
	tx2, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin2: %v", err)
	}
	_, posted2, err := rev.PostShiftRevenueJournal(ctx, tx2, acct, h.ids.tenantID, shift, adminID)
	_ = tx2.Rollback(ctx)
	if err != nil || posted2 {
		t.Fatalf("second post must be a no-op: posted=%v err=%v", posted2, err)
	}
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

	// adjustment and transfer must each post a balanced journal entry (audit
	// ACCT-012): the float balance moves AND the GL sees it. Before the fix both
	// returned a null journal_entry_id and silently broke double entry.
	code, adj := h.invPostJSON(t, "/api/v1/petty-cash-floats/"+floatID+"/transactions", admin,
		map[string]any{"txn_type": "adjustment", "amount": "500", "date": "2026-06-11", "description": "found cash"})
	if code != http.StatusCreated || adj["balance_after"] != "8900.00" || adj["journal_entry_id"] == nil {
		t.Fatalf("adjustment must post a journal entry: %d %v", code, adj)
	}
	code, xfer := h.invPostJSON(t, "/api/v1/petty-cash-floats/"+floatID+"/transactions", admin,
		map[string]any{"txn_type": "transfer", "amount": "1000", "date": "2026-06-12", "description": "return to bank"})
	if code != http.StatusCreated || xfer["balance_after"] != "7900.00" || xfer["journal_entry_id"] == nil {
		t.Fatalf("transfer must post a journal entry: %d %v", code, xfer)
	}

	// The trial balance still balances after the adjustment + transfer, proving
	// both posted balanced entries.
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

// TestPhase7_JournalImmutability proves the posted general ledger is append-only
// (audit ACCT — "nothing prevents a later UPDATE journal_lines SET debit = ..."):
// once an entry and its lines are written, no UPDATE or DELETE can rewrite
// financial history. Corrections must be posted as reversing entries — the one
// permitted transition. The 0065 triggers enforce this at the database, beneath
// every code path, so a bug or a direct write cannot silently corrupt reports.
func TestPhase7_JournalImmutability(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	if code, _ := h.invPostJSON(t, "/api/v1/accounts/seed-defaults", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("seed chart: %d", code)
	}
	code, period := h.invPostJSON(t, "/api/v1/accounting-periods", admin,
		map[string]any{"start_date": "2026-10-01", "end_date": "2026-10-31"})
	if code != http.StatusCreated {
		t.Fatalf("create period: %d", code)
	}
	periodID := period["id"].(string)

	var acctID string
	if err := h.pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE tenant_id = $1 ORDER BY code LIMIT 1`, h.ids.tenantID).Scan(&acctID); err != nil {
		t.Fatalf("account: %v", err)
	}

	// Post a balanced entry directly: DR 100 / CR 100. The 0064 balance trigger
	// accepts it at COMMIT; the 0065 immutability triggers now guard it.
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	var entryID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO journal_entries (tenant_id, period_id, entry_date, source_type, memo, posted_by)
		VALUES ($1, $2, '2026-10-15', 'adjustment', 'original', $3) RETURNING id
	`, h.ids.tenantID, periodID, adminID).Scan(&entryID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert entry: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO journal_lines (tenant_id, journal_entry_id, account_id, debit, credit)
		VALUES ($1, $2, $3, 100.00, 0), ($1, $2, $3, 0, 100.00)
	`, h.ids.tenantID, entryID, acctID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert lines: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit balanced entry: %v", err)
	}

	// Every mutation of posted ledger data must be refused at the database. The
	// error must come from our triggers (not some incidental failure), so assert
	// the message.
	mustBlock := func(label, want, sql string, args ...any) {
		t.Helper()
		if _, err := h.pool.Exec(ctx, sql, args...); err == nil {
			t.Fatalf("%s: expected the immutability trigger to refuse the write, got nil error", label)
		} else if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error = %q, want it to contain %q", label, err.Error(), want)
		}
	}
	mustBlock("UPDATE journal_lines", "immutable",
		`UPDATE journal_lines SET debit = 200 WHERE tenant_id = $1 AND journal_entry_id = $2`, h.ids.tenantID, entryID)
	mustBlock("DELETE journal_lines", "append-only",
		`DELETE FROM journal_lines WHERE tenant_id = $1 AND journal_entry_id = $2`, h.ids.tenantID, entryID)
	mustBlock("UPDATE journal_entries", "immutable",
		`UPDATE journal_entries SET memo = 'tampered' WHERE tenant_id = $1 AND id = $2`, h.ids.tenantID, entryID)
	mustBlock("DELETE journal_entries", "append-only",
		`DELETE FROM journal_entries WHERE tenant_id = $1 AND id = $2`, h.ids.tenantID, entryID)

	// The ledger is untouched after the rejected writes.
	var debitTotal, memo string
	if err := h.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(jl.debit), 0)::text, max(je.memo)
		FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
		WHERE je.tenant_id = $1 AND je.id = $2
	`, h.ids.tenantID, entryID).Scan(&debitTotal, &memo); err != nil {
		t.Fatalf("reread entry: %v", err)
	}
	if debitTotal != "100.00" || memo != "original" {
		t.Fatalf("after blocked writes: debit=%s memo=%q, want 100.00 / \"original\"", debitTotal, memo)
	}

	// The sanctioned correction path — a reversing entry — still works, and it
	// flips the original to 'reversed' (the one permitted UPDATE).
	if code, raw := h.do(t, http.MethodPost, "/api/v1/journal-entries/"+entryID+"/reverse", admin, nil, ""); code != http.StatusCreated {
		t.Fatalf("reverse: %d %s", code, raw)
	}
	var status string
	if err := h.pool.QueryRow(ctx,
		`SELECT status FROM journal_entries WHERE tenant_id = $1 AND id = $2`, h.ids.tenantID, entryID).Scan(&status); err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != "reversed" {
		t.Fatalf("original entry status = %s, want reversed", status)
	}
}

// TestPhase7_AuditLogImmutable proves the audit trail is strictly append-only at
// the database (migration 0070): an audit_logs row can never be UPDATE-d and
// cannot be DELETE-d directly. An audit log that can be edited after the fact is
// worthless for forensics; the 0070 trigger enforces immutability beneath every
// code path, so a bug or a direct write cannot silently rewrite the record of
// who did what. (The only sanctioned DELETE is whole-tenant teardown, which sets
// the app.allow_ledger_delete escape hatch — exercised by the test cleanup.)
func TestPhase7_AuditLogImmutable(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, _ := h.adminContext(t, ctx)

	// Seed an audit row directly (this is how every sensitive action is logged).
	var logID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO audit_logs (tenant_id, actor_id, action, entity_type, entity_id, reason)
		VALUES ($1, $2, 'product.created', 'product', 'PMS-1', 'original') RETURNING id
	`, h.ids.tenantID, adminID).Scan(&logID); err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	// Every mutation of a logged action must be refused at the database. Assert
	// the error comes from our trigger (not some incidental failure).
	mustBlock := func(label, want, sql string, args ...any) {
		t.Helper()
		if _, err := h.pool.Exec(ctx, sql, args...); err == nil {
			t.Fatalf("%s: expected the immutability trigger to refuse the write, got nil error", label)
		} else if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error = %q, want it to contain %q", label, err.Error(), want)
		}
	}
	// No UPDATE is ever permitted — an audit entry is never amended.
	mustBlock("UPDATE audit_logs", "immutable",
		`UPDATE audit_logs SET reason = 'tampered' WHERE tenant_id = $1 AND id = $2`, h.ids.tenantID, logID)
	// Direct delete is rejected (no app.allow_ledger_delete on this connection).
	mustBlock("DELETE audit_logs", "append-only",
		`DELETE FROM audit_logs WHERE tenant_id = $1 AND id = $2`, h.ids.tenantID, logID)

	// The audit row is untouched after the rejected writes.
	var reason string
	if err := h.pool.QueryRow(ctx,
		`SELECT reason FROM audit_logs WHERE tenant_id = $1 AND id = $2`, h.ids.tenantID, logID).Scan(&reason); err != nil {
		t.Fatalf("reread audit log: %v", err)
	}
	if reason != "original" {
		t.Fatalf("after blocked writes: reason=%q, want \"original\"", reason)
	}
}
