package server_test

// DB-backed integration test for the credit & cashflow (10.5) structured report.
// Reuses the Phase 2 harness; gated on TEST_DATABASE_URL + TEST_REDIS_URL.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5433/fuelgrid?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6379/0 \
//	go test ./services/api/internal/server -run ReportsCreditCashflow -v
//
// It asserts:
//
//	(a) tenders are summed by type in SQL (cash, mobile-money, credit, total);
//	(b) collections (a posted customer payment allocated to the station's invoice)
//	    and outstanding + overdue receivables are summed correctly;
//	(c) supplier payments (tenant-wide) and cash variance carry through; and the
//	    projected cash position = cash + mobile + collections − supplier payments;
//	(d) a freshly-created attendant (no revenue.read) is forbidden the report.

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// seedShiftOnToday creates (or reuses) an active operating day for the station on
// today's business date and returns a new approved shift id on it.
func seedShiftOnToday(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, stationID, adminID uuid.UUID) uuid.UUID {
	t.Helper()
	var dayID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT id FROM operating_days
		WHERE tenant_id = $1 AND station_id = $2 AND business_date = CURRENT_DATE AND status <> 'locked'
		LIMIT 1`, tenantID, stationID).Scan(&dayID); err != nil {
		if err := pool.QueryRow(ctx, `
			INSERT INTO operating_days (tenant_id, station_id, business_date, status, opened_by)
			VALUES ($1, $2, CURRENT_DATE, 'open', $3) RETURNING id`,
			tenantID, stationID, adminID).Scan(&dayID); err != nil {
			t.Fatalf("seed operating day: %v", err)
		}
	}
	var shiftID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO shifts (tenant_id, station_id, operating_day_id, name, status, opened_by)
		VALUES ($1, $2, $3, 'Day', 'approved', $4) RETURNING id`,
		tenantID, stationID, dayID, adminID).Scan(&shiftID); err != nil {
		t.Fatalf("seed shift: %v", err)
	}
	return shiftID
}

func seedTender(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, stationID, shiftID, receivedBy uuid.UUID, tender, amount string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO payments (tenant_id, station_id, shift_id, tender_type, amount, received_by, status)
		VALUES ($1, $2, $3, $4, $5::numeric, $6, 'recorded')`,
		tenantID, stationID, shiftID, tender, amount, receivedBy); err != nil {
		t.Fatalf("seed tender %s: %v", tender, err)
	}
}

func TestReportsCreditCashflow_TotalsAndScope(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	adminID := adminUserID(t, ctx, h.pool, h.ids.tenantID)

	shiftID := seedShiftOnToday(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, adminID)
	// Tenders: cash 1000, mobile 400, credit 600 -> total 2000.
	seedTender(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, shiftID, adminID, "cash", "1000")
	seedTender(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, shiftID, adminID, "mobile_money", "400")
	seedTender(t, ctx, h.pool, h.ids.tenantID, h.ids.station1, shiftID, adminID, "credit", "600")

	// A credit customer with an issued invoice on station1: amount 600,
	// outstanding 350 after a 250 collection; due yesterday so it is overdue.
	var custID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO customers (tenant_id, code, name, credit_limit, status)
		VALUES ($1, 'CASHCUST', 'Cashflow Customer', 100000, 'active') RETURNING id`,
		h.ids.tenantID).Scan(&custID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	var invID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO customer_invoices
		    (tenant_id, customer_id, invoice_date, due_date, amount, outstanding_amount, station_id, status, created_by)
		VALUES ($1, $2, CURRENT_DATE, CURRENT_DATE - 1, 600, 350, $3, 'partially_paid', $4) RETURNING id`,
		h.ids.tenantID, custID, h.ids.station1, adminID).Scan(&invID); err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
	var payID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO customer_payments (tenant_id, customer_id, payment_date, method, amount, allocated_amount, status, created_by)
		VALUES ($1, $2, CURRENT_DATE, 'cash', 250, 250, 'posted', $3) RETURNING id`,
		h.ids.tenantID, custID, adminID).Scan(&payID); err != nil {
		t.Fatalf("seed customer payment: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO customer_payment_allocations (tenant_id, customer_payment_id, customer_invoice_id, amount)
		VALUES ($1, $2, $3, 250)`,
		h.ids.tenantID, payID, invID); err != nil {
		t.Fatalf("seed allocation: %v", err)
	}

	// A posted supplier payment of 300 (tenant-wide).
	var supID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO suppliers (tenant_id, code, name, status)
		VALUES ($1, 'CFSUP', 'Cashflow Supplier', 'active') RETURNING id`,
		h.ids.tenantID).Scan(&supID); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO supplier_payments (tenant_id, supplier_id, payment_date, method, amount, status, created_by)
		VALUES ($1, $2, CURRENT_DATE, 'bank', 300, 'posted', $3)`,
		h.ids.tenantID, supID, adminID); err != nil {
		t.Fatalf("seed supplier payment: %v", err)
	}

	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	code, body := h.getJSON(t, "/api/v1/reports/credit-cashflow?station_id="+h.ids.station1.String()+"&period=this-month", admin)
	if code != http.StatusOK {
		t.Fatalf("credit-cashflow report = %d, want 200 (%v)", code, body)
	}
	if got := summaryValue(body, "Cash sales"); got != "1000.00" {
		t.Fatalf("cash sales = %q, want 1000.00", got)
	}
	if got := summaryValue(body, "Mobile-money sales"); got != "400.00" {
		t.Fatalf("mobile-money sales = %q, want 400.00", got)
	}
	if got := summaryValue(body, "Credit sales"); got != "600.00" {
		t.Fatalf("credit sales = %q, want 600.00", got)
	}
	if got := summaryValue(body, "Total tendered"); got != "2000.00" {
		t.Fatalf("total tendered = %q, want 2000.00", got)
	}
	if got := summaryValue(body, "Collections"); got != "250.00" {
		t.Fatalf("collections = %q, want 250.00", got)
	}
	if got := summaryValue(body, "Outstanding receivables"); got != "350.00" {
		t.Fatalf("outstanding = %q, want 350.00", got)
	}
	if got := summaryValue(body, "Overdue receivables"); got != "350.00" {
		t.Fatalf("overdue = %q, want 350.00 (due yesterday)", got)
	}
	if got := summaryValue(body, "Supplier payments (network)"); got != "300.00" {
		t.Fatalf("supplier payments = %q, want 300.00", got)
	}
	// Projected cash position = cash 1000 + mobile 400 + collections 250 − supplier 300 = 1350.
	if got := summaryValue(body, "Projected cash position"); got != "1350.00" {
		t.Fatalf("projected cash position = %q, want 1350.00", got)
	}

	// A freshly-created attendant holds no revenue.read: the report is 403.
	att := freshAttendant(t, ctx, h, tenantSlug)
	if code, _ := h.getJSON(t, "/api/v1/reports/credit-cashflow?station_id="+h.ids.station1.String(), att); code != http.StatusForbidden {
		t.Fatalf("attendant credit-cashflow report = %d, want 403", code)
	}
}
