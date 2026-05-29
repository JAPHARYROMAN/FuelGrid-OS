package server_test

// DB-backed integration test for the Phase 6 revenue chain: pricing ->
// recognized sales (COGS/margin) on approval -> tender reconciliation ->
// credit customer + AR limit -> daily revenue close + lock. Reuses the Phase 2
// harness and the Phase 4 helpers (same package). Gated on TEST_DATABASE_URL +
// TEST_REDIS_URL.

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestPhase6_RevenueFlow(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`,
		h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("nozzle: %v", err)
	}

	// Open the PMS tank and seed a costed delivery movement (cost basis 2400/L).
	pms := "/api/v1/tanks/" + h.ids.tankPMS.String()
	if code, _ := h.invPostJSON(t, pms+"/opening-balance", admin, map[string]any{"litres": 30000}); code != http.StatusCreated {
		t.Fatalf("open: %d", code)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO stock_movements
		    (tenant_id, tank_id, movement_type, source_ref_type, litres, balance_after, recorded_by, landed_cost_per_litre, landed_cost_total)
		VALUES ($1, $2, 'delivery', 'delivery', 10000, 40000, $3, 2400, 24000000)
	`, h.ids.tenantID, h.ids.tankPMS, adminID); err != nil {
		t.Fatalf("seed costed delivery: %v", err)
	}

	// Set the PMS selling price to 2,950.
	station := "/api/v1/stations/" + h.ids.station1.String()
	if code, _ := h.invPostJSON(t, station+"/prices", admin,
		map[string]any{"product_id": h.ids.pmsProduct.String(), "unit_price": "2950"}); code != http.StatusCreated {
		t.Fatalf("set price: %d", code)
	}

	// Closed shift sold 4,200 L; approve -> recognizes revenue.
	day, shift := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-01", 4200)
	if code, raw := h.do(t, http.MethodPatch, "/api/v1/shifts/"+shift.String()+"/status", admin,
		bytes.NewReader([]byte(`{"status":"approved"}`)), "application/json"); code != http.StatusOK {
		t.Fatalf("approve: %d %s", code, raw)
	}

	// One recognized sale: gross 12,390,000; COGS 10,080,000; margin 2,310,000.
	code, body := h.getJSON(t, "/api/v1/shifts/"+shift.String()+"/sales", admin)
	items := body["items"].([]any)
	if code != http.StatusOK || len(items) != 1 {
		t.Fatalf("sales: status %d count %d", code, len(items))
	}
	sale := items[0].(map[string]any)
	if sale["gross_amount"] != "12390000.00" || sale["net_amount"] != "12390000.00" {
		t.Fatalf("sale gross/net = %v / %v", sale["gross_amount"], sale["net_amount"])
	}
	if sale["cogs_amount"] != "10080000.00" || sale["margin_amount"] != "2310000.00" {
		t.Fatalf("sale cogs/margin = %v / %v", sale["cogs_amount"], sale["margin_amount"])
	}

	// Cash tender matching revenue reconciles to zero variance.
	if code, _ := h.invPostJSON(t, "/api/v1/shifts/"+shift.String()+"/payments", admin,
		map[string]any{"tender_type": "cash", "amount": "12390000"}); code != http.StatusCreated {
		t.Fatalf("record cash: %d", code)
	}
	code, rec := h.getJSON(t, "/api/v1/shifts/"+shift.String()+"/payment-reconciliation", admin)
	if code != http.StatusOK || rec["variance"] != "0.00" || rec["over_threshold"].(bool) {
		t.Fatalf("reconciliation = %v", rec)
	}

	// Credit customer with a 5,000 limit; a 6,000 credit charge is refused, 4,000 accepted.
	code, cust := h.invPostJSON(t, "/api/v1/customers", admin,
		map[string]any{"code": "ACME", "name": "Acme Fleet", "credit_limit": "5000"})
	if code != http.StatusCreated {
		t.Fatalf("create customer: %d %v", code, cust)
	}
	custID := cust["id"].(string)
	if code, _ := h.invPostJSON(t, "/api/v1/shifts/"+shift.String()+"/payments", admin,
		map[string]any{"tender_type": "credit", "amount": "6000", "customer_id": custID}); code != http.StatusUnprocessableEntity {
		t.Fatalf("over-limit credit: status %d, want 422", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/shifts/"+shift.String()+"/payments", admin,
		map[string]any{"tender_type": "credit", "amount": "4000", "customer_id": custID}); code != http.StatusCreated {
		t.Fatalf("within-limit credit: status %d, want 201", code)
	}
	// FLEET-002: a customer on credit hold cannot be charged, even for an amount
	// within the limit — the hold is a hard stop on the real sale path.
	if code, _ := h.invPostJSON(t, "/api/v1/customers/"+custID+"/status", admin,
		map[string]any{"status": "on_hold"}); code != http.StatusOK {
		t.Fatalf("set customer on_hold: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/shifts/"+shift.String()+"/payments", admin,
		map[string]any{"tender_type": "credit", "amount": "100", "customer_id": custID}); code != http.StatusUnprocessableEntity {
		t.Fatalf("credit tender to on-hold customer: status %d, want 422", code)
	}
	code, stmt := h.getJSON(t, "/api/v1/customers/"+custID+"/statement", admin)
	if code != http.StatusOK || stmt["balance"] != "4000.00" {
		t.Fatalf("customer balance = %v", stmt["balance"])
	}

	// Daily revenue close + lock.
	code, rd := h.invPostJSON(t, station+"/revenue-days", admin, map[string]any{"operating_day_id": day.String()})
	if code != http.StatusOK {
		t.Fatalf("compute revenue day: %d %v", code, rd)
	}
	if rd["gross_revenue"] != "12390000.00" || rd["margin_total"] != "2310000.00" ||
		rd["cash_total"] != "12390000.00" || rd["credit_total"] != "4000.00" {
		t.Fatalf("revenue day = %v", rd)
	}
	rdID := rd["id"].(string)
	code, locked := h.do(t, http.MethodPost, "/api/v1/revenue-days/"+rdID+"/lock", admin, nil, "")
	if code != http.StatusOK {
		t.Fatalf("lock revenue day: %d %s", code, locked)
	}
	if code, _ := h.do(t, http.MethodPost, "/api/v1/revenue-days/"+rdID+"/lock", admin, nil, ""); code != http.StatusConflict {
		t.Fatalf("re-lock: status %d, want 409", code)
	}

	// Overview reflects the day's revenue.
	code, ov := h.getJSON(t, station+"/revenue-overview", admin)
	if code != http.StatusOK || ov["summary"] == nil {
		t.Fatalf("revenue overview = %v", ov)
	}
}

// TestPhase6_CreditTenderPostsToGL proves PAY-003: a credit tender posts to the
// general ledger (DR accounts_receivable / CR sales_clearing) in the same tx as
// the operational AR entry, so the operational and GL receivables reconcile —
// previously the credit tender wrote ar_entries only and touched no journal.
func TestPhase6_CreditTenderPostsToGL(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	// Configure accounting so the GL coupling fires: the chart of accounts plus
	// a period covering "now" (when the tender is recorded).
	if code, _ := h.invPostJSON(t, "/api/v1/accounts/seed-defaults", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("seed chart: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/accounting-periods", admin,
		map[string]any{"start_date": "2026-01-01", "end_date": "2026-12-31"}); code != http.StatusCreated {
		t.Fatalf("create period: %d", code)
	}

	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`,
		h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("nozzle: %v", err)
	}
	_, shift := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-15", 1000)

	code, cust := h.invPostJSON(t, "/api/v1/customers", admin,
		map[string]any{"code": "GLCO", "name": "GL Co", "credit_limit": "10000"})
	if code != http.StatusCreated {
		t.Fatalf("create customer: %d %v", code, cust)
	}
	custID := cust["id"].(string)

	// A 4,000 credit tender.
	if code, _ := h.invPostJSON(t, "/api/v1/shifts/"+shift.String()+"/payments", admin,
		map[string]any{"tender_type": "credit", "amount": "4000", "customer_id": custID}); code != http.StatusCreated {
		t.Fatalf("credit tender: %d", code)
	}

	// Operational AR (the customer statement) shows 4,000.
	if code, stmt := h.getJSON(t, "/api/v1/customers/"+custID+"/statement", admin); code != http.StatusOK || stmt["balance"] != "4000.00" {
		t.Fatalf("operational AR balance = %v", stmt["balance"])
	}

	// GL accounts_receivable (a debit-balance asset) now also shows 4,000 — the
	// ledgers are coupled.
	var arBal string
	if err := h.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(jl.debit - jl.credit), 0)::text
		FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
		WHERE jl.tenant_id = $1 AND a.system_key = 'accounts_receivable'
	`, h.ids.tenantID).Scan(&arBal); err != nil {
		t.Fatalf("AR GL balance: %v", err)
	}
	if arBal != "4000.00" {
		t.Fatalf("GL accounts_receivable = %s, want 4000.00 (coupled to operational AR)", arBal)
	}

	// sales_clearing (a credit-balance account) was credited 4,000 — the credit
	// portion of the sale settled into AR rather than leaving sales_clearing
	// unbalanced.
	var scBal string
	if err := h.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(jl.credit - jl.debit), 0)::text
		FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
		WHERE jl.tenant_id = $1 AND a.system_key = 'sales_clearing'
	`, h.ids.tenantID).Scan(&scBal); err != nil {
		t.Fatalf("sales_clearing GL balance: %v", err)
	}
	if scBal != "4000.00" {
		t.Fatalf("GL sales_clearing = %s, want 4000.00", scBal)
	}
}
