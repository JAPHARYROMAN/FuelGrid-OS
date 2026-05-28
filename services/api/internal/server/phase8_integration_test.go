package server_test

// DB-backed integration tests for Phase 8 — Customer Credit & Fleet Fuel OS.
// Reuses the Phase 2 harness + Phase 4/6 helpers. Gated on TEST_DATABASE_URL +
// TEST_REDIS_URL.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// jsonBody marshals v into an io.Reader for harness PUT/PATCH requests.
func jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

// TestPhase8_CreditFoundation covers Category A: the evolved customer master,
// account-status lifecycle, contacts, credit profile + real-time credit
// position, manual holds, and customer price agreements.
func TestPhase8_CreditFoundation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	// Create a fleet customer with the new master fields and a 100,000 limit.
	code, cust := h.invPostJSON(t, "/api/v1/customers", admin, map[string]any{
		"code": "FLEETCO", "name": "Fleet Co", "legal_name": "Fleet Company Ltd",
		"tax_id": "TIN-123", "account_type": "fleet", "credit_limit": "100000",
	})
	if code != http.StatusCreated || cust["account_type"] != "fleet" || cust["legal_name"] != "Fleet Company Ltd" {
		t.Fatalf("create customer = %d %v", code, cust)
	}
	custID := cust["id"].(string)

	// Status lifecycle: place the account on hold.
	if code, st := h.invPostJSON(t, "/api/v1/customers/"+custID+"/status", admin, map[string]any{"status": "on_hold"}); code != http.StatusOK || st["status"] != "on_hold" {
		t.Fatalf("set status = %d %v", code, st)
	}

	// Contacts.
	if code, _ := h.invPostJSON(t, "/api/v1/customers/"+custID+"/contacts", admin, map[string]any{
		"name": "Jane Ops", "role": "fleet_manager", "email": "jane@fleetco.test",
	}); code != http.StatusCreated {
		t.Fatalf("create contact: %d", code)
	}
	if code, contacts := h.getJSON(t, "/api/v1/customers/"+custID+"/contacts", admin); code != http.StatusOK || contacts["count"].(float64) != 1 {
		t.Fatalf("list contacts = %v", contacts)
	}

	// Credit profile.
	if code, prof := h.do(t, http.MethodPut, "/api/v1/customers/"+custID+"/credit-profile", admin,
		jsonBody(map[string]any{"payment_terms_days": 30, "warning_threshold_pct": "75", "risk_category": "watch"}), "application/json"); code != http.StatusOK {
		t.Fatalf("upsert credit profile = %d %s", code, prof)
	}

	// Credit position: limit 100,000, no AR, but on_hold so hold=true.
	code, pos := h.getJSON(t, "/api/v1/customers/"+custID+"/credit-position", admin)
	if code != http.StatusOK || pos["credit_limit"] != "100000.00" || pos["available_credit"] != "100000.00" ||
		pos["status"] != "on_hold" || !pos["hold"].(bool) || pos["over_limit"].(bool) {
		t.Fatalf("credit position = %v", pos)
	}

	// Manual hold toggle.
	if code, _ := h.invPostJSON(t, "/api/v1/customers/"+custID+"/credit-hold", admin, map[string]any{"hold": true, "reason": "overdue review"}); code != http.StatusOK {
		t.Fatalf("set hold: %d", code)
	}

	// Price agreement: a fixed PMS price of 2,800, approved and activated.
	code, agr := h.invPostJSON(t, "/api/v1/customer-price-agreements", admin, map[string]any{
		"customer_id": custID, "product_id": h.ids.pmsProduct.String(),
		"price_type": "fixed", "fixed_price": "2800", "effective_from": "2026-06-01",
	})
	if code != http.StatusCreated || agr["status"] != "draft" {
		t.Fatalf("create agreement = %d %v", code, agr)
	}
	agrID := agr["id"].(string)
	if code, _ := h.do(t, http.MethodPost, "/api/v1/customer-price-agreements/"+agrID+"/approve", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("approve agreement: %d", code)
	}
	if code, act := h.do(t, http.MethodPost, "/api/v1/customer-price-agreements/"+agrID+"/activate", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("activate agreement: %d %s", code, act)
	}
	if code, list := h.getJSON(t, "/api/v1/customer-price-agreements?customer_id="+custID, admin); code != http.StatusOK || list["count"].(float64) != 1 {
		t.Fatalf("list agreements = %v", list)
	}
}
