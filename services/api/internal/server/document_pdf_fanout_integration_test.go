package server_test

// DB-backed integration coverage for the Phase 2b document PDF endpoints
// (DOC-PDF fan-out): the purchase-orders / deliveries / expenses / customer-aging
// / supplier-balances / journal list documents plus the single purchase-order
// and single customer-invoice record documents. Mirrors
// TestDocumentPDF_ExportsAndAuthorizes: each endpoint returns a non-empty
// application/pdf (with the %PDF magic) for an authorized actor, and 403 for a
// freshly-created attendant that holds none of the read permissions these
// documents gate on. Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the rest.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5433/fuelgrid?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6379/0 \
//	go test ./services/api/internal/server -run DocumentPDFFanout -v

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

func TestDocumentPDFFanout_ExportsAndAuthorizes(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)

	ctx := context.Background()

	// Resolve the admin user id (for PO.raised_by).
	var adminID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, h.ids.adminEmail).Scan(&adminID); err != nil {
		t.Fatalf("admin id: %v", err)
	}

	// A minimal attendant (with a station grant) holds none of purchase_order.read,
	// inventory.read, finance.read, journal.read, or payable.read — the
	// deterministic 403 actor for every Phase 2b document.
	hash, err := password.New(password.DefaultParams, "").Hash(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	attEmail := fmt.Sprintf("doc2b-att-%d@it.local", time.Now().UnixNano())
	var attID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, full_name, status, password_hash, password_changed_at)
		 VALUES ($1, $2, 'Doc2b Attendant', 'active', $3, now()) RETURNING id`,
		h.ids.tenantID, attEmail, hash).Scan(&attID); err != nil {
		t.Fatalf("seed attendant: %v", err)
	}
	grantRole(t, ctx, h.pool, h.ids.tenantID, attID, "attendant")
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO user_station_access (user_id, station_id, tenant_id) VALUES ($1, $2, $3)`,
		attID, h.ids.station1, h.ids.tenantID); err != nil {
		t.Fatalf("station access: %v", err)
	}
	attendant := h.login(t, tenantSlug, attEmail)

	// Seed one supplier + one purchase order with a line (for the single-PO doc),
	// and one issued-shaped customer invoice with a line (for the invoice doc).
	var supplierID, poID, customerID, invoiceID uuid.UUID
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO suppliers (tenant_id, code, name, payment_terms_days, status)
		 VALUES ($1, 'DOC2B-SUP', 'Doc2b Petroleum', 30, 'active') RETURNING id`,
		h.ids.tenantID).Scan(&supplierID); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO purchase_orders (tenant_id, station_id, supplier_id, status, raised_by)
		VALUES ($1, $2, $3, 'submitted', $4) RETURNING id
	`, h.ids.tenantID, h.ids.station1, supplierID, adminID).Scan(&poID); err != nil {
		t.Fatalf("seed po: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO purchase_order_lines (tenant_id, purchase_order_id, product_id, ordered_litres, unit_price, received_litres)
		VALUES ($1, $2, $3, 10000.000, 2950.00, 0.000)
	`, h.ids.tenantID, poID, h.ids.pmsProduct); err != nil {
		t.Fatalf("seed po line: %v", err)
	}
	if err := h.pool.QueryRow(ctx,
		`INSERT INTO customers (tenant_id, code, name, credit_limit, status)
		 VALUES ($1, 'DOC2B-CUST', 'Doc2b Logistics', 500000.00, 'active') RETURNING id`,
		h.ids.tenantID).Scan(&customerID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO customer_invoices
		    (tenant_id, customer_id, invoice_number, invoice_date, amount, outstanding_amount, source_type, status, created_by)
		VALUES ($1, $2, 'INV-DOC2B-1', now(), 1500.00, 1500.00, 'manual', 'draft', $3) RETURNING id
	`, h.ids.tenantID, customerID, adminID).Scan(&invoiceID); err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO customer_invoice_lines (tenant_id, customer_invoice_id, description, amount, revenue_account_key)
		VALUES ($1, $2, 'Fuel supply', 1500.00, 'sales_revenue')
	`, h.ids.tenantID, invoiceID); err != nil {
		t.Fatalf("seed invoice line: %v", err)
	}

	cases := []struct {
		name string
		path string
	}{
		{"purchase_orders", "/api/v1/purchase-orders.pdf"},
		{"purchase_order_single", "/api/v1/purchase-orders/" + poID.String() + ".pdf"},
		{"deliveries", "/api/v1/stations/" + h.ids.station1.String() + "/deliveries.pdf"},
		{"expenses", "/api/v1/expenses.pdf"},
		{"customer_aging", "/api/v1/customer-aging.pdf"},
		{"supplier_balances", "/api/v1/supplier-balances.pdf"},
		{"journal_entries", "/api/v1/journal-entries.pdf"},
		{"customer_invoice_single", "/api/v1/customer-invoices/" + invoiceID.String() + ".pdf"},
	}

	for _, tc := range cases {
		t.Run(tc.name+"_ok", func(t *testing.T) {
			code, body, ct := h.getRawWithType(t, tc.path, admin)
			if code != http.StatusOK {
				t.Fatalf("%s status = %d (want 200): %s", tc.path, code, body)
			}
			if ct != "application/pdf" {
				t.Fatalf("%s Content-Type = %q (want application/pdf)", tc.path, ct)
			}
			if !bytes.HasPrefix(body, []byte("%PDF-")) {
				t.Fatalf("%s body is not a PDF (bad magic): %q", tc.path, body[:min(8, len(body))])
			}
		})
		t.Run(tc.name+"_forbidden", func(t *testing.T) {
			code, body := h.do(t, http.MethodGet, tc.path, attendant, nil, "")
			if code != http.StatusForbidden {
				t.Fatalf("%s as attendant status = %d (want 403): %s", tc.path, code, body)
			}
		})
	}
}
