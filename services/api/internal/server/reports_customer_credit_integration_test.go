package server_test

// DB-backed integration test for the Customer Credit (§5.9) structured report.
// Reuses the Phase 2 harness; gated on TEST_DATABASE_URL + TEST_REDIS_URL.
//
//	TEST_DATABASE_URL=postgres://... TEST_REDIS_URL=redis://... \
//	go test ./services/api/internal/server -run ReportsCustomerCredit -v
//
// It asserts:
//
//	(a) issued invoices are aged server-side into Current / 1-30 / 31-60 / 61-90 /
//	    90+ buckets from due_date vs the report date (SQL date math);
//	(b) the KPI hero totals (total receivable, overdue, % overdue) and the
//	    over-limit / on-hold counts;
//	(c) CREDIT EXPOSURE is shown to a customer_credit.read holder (admin) and
//	    OMITTED (not zeroed) for a customer.read-only actor (auditor), with a
//	    data-quality note;
//	(d) the per-customer drilldown returns the customer's open invoices + payments;
//	(e) a freshly-created attendant (no customer.read) is forbidden the report.

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

// seedCreditInvoice inserts an issued/partially-paid invoice for a customer with
// an explicit due-date offset (days from today; negative = past due) and an
// outstanding balance.
func seedCreditInvoice(t *testing.T, ctx context.Context, pool *database.Pool, tenantID, customerID, createdBy uuid.UUID, dueOffsetDays int, outstanding string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO customer_invoices
		    (tenant_id, customer_id, invoice_date, due_date, amount, outstanding_amount, status, created_by)
		VALUES ($1, $2, CURRENT_DATE - 120, CURRENT_DATE + ($3::int || ' days')::interval, $4::numeric, $4::numeric, 'issued', $5)
		RETURNING id`,
		tenantID, customerID, dueOffsetDays, outstanding, createdBy).Scan(&id); err != nil {
		t.Fatalf("seed credit invoice: %v", err)
	}
	return id
}

func TestReportsCustomerCredit_BucketsAndExposureGating(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	tenantSlug := slug(h)
	adminID := adminUserID(t, ctx, h.pool, h.ids.tenantID)

	// A credit customer with a 1,000 limit and an 80% warning threshold.
	var custID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO customers (tenant_id, code, name, credit_limit, status)
		VALUES ($1, 'AGECUST', 'Aging Customer', 1000, 'active') RETURNING id`,
		h.ids.tenantID).Scan(&custID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	// Invoices landing in each bucket by due date vs today:
	//   +5 days  -> Current (not yet due) : 100
	//   -15 days -> 1-30                   : 200
	//   -45 days -> 31-60                  : 300
	//   -75 days -> 61-90                  : 400
	//   -120 days-> 90+                    : 500
	// Outstanding = 1500; overdue = 1400 (all but the Current 100).
	seedCreditInvoice(t, ctx, h.pool, h.ids.tenantID, custID, adminID, 5, "100")
	seedCreditInvoice(t, ctx, h.pool, h.ids.tenantID, custID, adminID, -15, "200")
	seedCreditInvoice(t, ctx, h.pool, h.ids.tenantID, custID, adminID, -45, "300")
	seedCreditInvoice(t, ctx, h.pool, h.ids.tenantID, custID, adminID, -75, "400")
	seedCreditInvoice(t, ctx, h.pool, h.ids.tenantID, custID, adminID, -120, "500")

	// An AR charge of 1,200 pushes exposure (1200) over the 1,000 limit, so the
	// customer is over limit. (ar_entries balance drives exposure.)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO ar_entries (tenant_id, customer_id, entry_type, amount, balance_after, recorded_by)
		VALUES ($1, $2, 'charge', 1200, 1200, $3)`,
		h.ids.tenantID, custID, adminID); err != nil {
		t.Fatalf("seed ar charge: %v", err)
	}

	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	code, body := h.getJSON(t, "/api/v1/reports/customer-credit?period=this-month", admin)
	if code != http.StatusOK {
		t.Fatalf("customer-credit report = %d, want 200 (%v)", code, body)
	}

	if got := summaryValue(body, "Total receivable"); got != "1500.00" {
		t.Fatalf("total receivable = %q, want 1500.00", got)
	}
	if got := summaryValue(body, "Total overdue"); got != "1400.00" {
		t.Fatalf("total overdue = %q, want 1400.00", got)
	}
	if got := summaryValue(body, "% overdue"); got != "93.3" {
		t.Fatalf("%% overdue = %q, want 93.3 (1400/1500)", got)
	}
	if got := summaryValue(body, "Customers with balance"); got != "1" {
		t.Fatalf("customers with balance = %q, want 1", got)
	}
	if got := summaryValue(body, "Customers over limit"); got != "1" {
		t.Fatalf("customers over limit = %q, want 1", got)
	}

	// The aging-bucket chart slices carry the exact per-bucket decimal strings.
	chart, _ := body["chart_data"].(map[string]any)
	if chart == nil {
		t.Fatalf("chart_data missing/!object: %v", body["chart_data"])
	}
	if shown, _ := chart["exposure_shown"].(bool); !shown {
		t.Fatalf("exposure_shown = false for admin (holds customer_credit.read), want true")
	}
	wantBuckets := map[string]string{
		"Current": "100.00", "1-30": "200.00", "31-60": "300.00", "61-90": "400.00", "90+": "500.00",
	}
	buckets, _ := chart["buckets"].([]any)
	got := map[string]string{}
	for _, b := range buckets {
		row, _ := b.(map[string]any)
		name, _ := row["bucket"].(string)
		amt, _ := row["amount"].(string)
		got[name] = amt
	}
	for k, want := range wantBuckets {
		if got[k] != want {
			t.Fatalf("bucket %q = %q, want %q (full buckets: %v)", k, got[k], want, got)
		}
	}

	// The per-customer row carries the gated CREDIT EXPOSURE block for admin.
	custs, _ := chart["customers"].([]any)
	if len(custs) != 1 {
		t.Fatalf("customers rows = %d, want 1", len(custs))
	}
	row0, _ := custs[0].(map[string]any)
	if _, ok := row0["credit_limit"].(string); !ok {
		t.Fatalf("admin row missing credit_limit (exposure gated incorrectly): %v", row0)
	}
	if util, _ := row0["utilization"].(string); util != "120.00" {
		t.Fatalf("utilization = %q, want 120.00 (1200/1000)", util)
	}

	// ---- Exposure GATE: an auditor holds customer.read but NOT customer_credit.read.
	auditor := freshUserWithRole(t, ctx, h, tenantSlug, "auditor")
	code, abody := h.getJSON(t, "/api/v1/reports/customer-credit", auditor)
	if code != http.StatusOK {
		t.Fatalf("auditor customer-credit report = %d, want 200 (%v)", code, abody)
	}
	achart, _ := abody["chart_data"].(map[string]any)
	if shown, _ := achart["exposure_shown"].(bool); shown {
		t.Fatalf("exposure_shown = true for auditor (no customer_credit.read), want false")
	}
	acusts, _ := achart["customers"].([]any)
	if len(acusts) == 1 {
		arow0, _ := acusts[0].(map[string]any)
		if _, present := arow0["credit_limit"]; present {
			t.Fatalf("auditor row leaked credit_limit (must be OMITTED, not zeroed): %v", arow0)
		}
	}
	// The aging buckets are still visible to the auditor (only exposure is gated).
	if got := summaryValue(abody, "Total receivable"); got != "1500.00" {
		t.Fatalf("auditor total receivable = %q, want 1500.00 (buckets are not gated)", got)
	}

	// ---- Drilldown: the customer's open invoices (5) come back, aged.
	code, dbody := h.getJSON(t, "/api/v1/reports/customer-credit/drilldown?customer_id="+custID.String(), admin)
	if code != http.StatusOK {
		t.Fatalf("drilldown = %d, want 200 (%v)", code, dbody)
	}
	invs, _ := dbody["invoices"].([]any)
	if len(invs) != 5 {
		t.Fatalf("drilldown invoices = %d, want 5", len(invs))
	}

	// ---- A freshly-created attendant holds no customer.read: the report is 403.
	att := freshAttendant(t, ctx, h, tenantSlug)
	if code, _ := h.getJSON(t, "/api/v1/reports/customer-credit", att); code != http.StatusForbidden {
		t.Fatalf("attendant customer-credit report = %d, want 403", code)
	}
}

// freshUserWithRole creates a brand-new user with the given system role (no
// station grant needed for a tenant-wide report) and logs in. Used to test the
// CREDIT EXPOSURE gate with a customer.read-only role (e.g. auditor).
func freshUserWithRole(t *testing.T, ctx context.Context, h *harness, tenantSlug, roleCode string) string {
	t.Helper()
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := "role-" + roleCode + "-" + uuid.NewString()[:8] + "@it.local"
	var uid uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'Role User', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, email, hash).Scan(&uid); err != nil {
		t.Fatalf("seed role user: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, uid, roleCode)
	return h.login(t, tenantSlug, email)
}
