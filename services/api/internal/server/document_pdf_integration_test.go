package server_test

// DB-backed integration coverage for the list-document PDF endpoints (DOC-PDF):
// GET /api/v1/customers.pdf, /suppliers.pdf, /products.pdf. Reuses the Phase 2
// harness; gated on TEST_DATABASE_URL + TEST_REDIS_URL like the rest of the
// suite, so `go test ./...` stays green without infra.
//
//	TEST_DATABASE_URL=postgres://fuelgrid:fuelgrid@localhost:5433/fuelgrid?sslmode=disable \
//	TEST_REDIS_URL=redis://localhost:6379/0 \
//	go test ./services/api/internal/server -run DocumentPDF -v

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
)

// TestDocumentPDF_ExportsAndAuthorizes proves each list-document endpoint
// returns a non-empty application/pdf body (with the %PDF magic) for an actor
// holding the mirrored read permission, and 403 for an actor who lacks it.
// The seeded admin (system_admin) holds every permission; the seeded operator
// (station_manager) holds station.read but NOT customer.read /
// purchase_order.read — so products.pdf is allowed for the operator while
// customers.pdf / suppliers.pdf are forbidden.
func TestDocumentPDF_ExportsAndAuthorizes(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	tenantSlug := slug(h)
	admin := h.login(t, tenantSlug, h.ids.adminEmail)
	operator := h.login(t, tenantSlug, h.ids.opEmail)

	ctx := context.Background()
	// Seed one customer and one supplier (the harness already seeds products).
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO customers (tenant_id, code, name, contact_name, contact_phone, credit_limit, status)
		VALUES ($1, 'CUST-1', 'Acme Logistics', 'Jane Doe', '+254700000000', 500000.00, 'active')`,
		h.ids.tenantID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO suppliers (tenant_id, code, name, contact_name, contact_email, payment_terms_days, status)
		VALUES ($1, 'SUP-1', 'Coastal Petroleum', 'John Smith', 'sales@coastal.example', 30, 'active')`,
		h.ids.tenantID); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}

	cases := []struct {
		name         string
		path         string
		allowedToken string
		deniedToken  string // empty => no 403 case for this endpoint
	}{
		{"customers", "/api/v1/customers.pdf", admin, operator},
		{"suppliers", "/api/v1/suppliers.pdf", admin, operator},
		{"products", "/api/v1/products.pdf", admin, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name+"_ok", func(t *testing.T) {
			code, body, ct := h.getRawWithType(t, tc.path, tc.allowedToken)
			if code != http.StatusOK {
				t.Fatalf("%s status = %d (want 200): %s", tc.path, code, body)
			}
			if ct != "application/pdf" {
				t.Fatalf("%s Content-Type = %q (want application/pdf)", tc.path, ct)
			}
			if len(body) == 0 {
				t.Fatalf("%s body is empty", tc.path)
			}
			if !bytes.HasPrefix(body, []byte("%PDF-")) {
				t.Fatalf("%s body is not a PDF (bad magic): %q", tc.path, body[:min(8, len(body))])
			}
		})

		if tc.deniedToken != "" {
			t.Run(tc.name+"_forbidden", func(t *testing.T) {
				code, body := h.do(t, http.MethodGet, tc.path, tc.deniedToken, nil, "")
				if code != http.StatusForbidden {
					t.Fatalf("%s as operator status = %d (want 403): %s", tc.path, code, body)
				}
			})
		}
	}
}

// getRawWithType issues an authenticated GET and returns the status, raw body,
// and Content-Type header — needed to assert the PDF content type the JSON
// helper discards.
func (h *harness) getRawWithType(t *testing.T, path, token string) (int, []byte, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.baseURL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, resp.Header.Get("Content-Type")
}
