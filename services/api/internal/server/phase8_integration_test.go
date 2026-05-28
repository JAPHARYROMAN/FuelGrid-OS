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

	"github.com/google/uuid"
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

// TestPhase8_FleetIdentity covers Category B: vehicles, drivers (with PIN), and
// fuel credentials with tokenized storage and a forecourt validation endpoint.
func TestPhase8_FleetIdentity(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	code, cust := h.invPostJSON(t, "/api/v1/customers", admin, map[string]any{"code": "HAULCO", "name": "Haul Co"})
	if code != http.StatusCreated {
		t.Fatalf("create customer: %d", code)
	}
	custID := cust["id"].(string)

	// Vehicle.
	code, veh := h.invPostJSON(t, "/api/v1/fleet/vehicles", admin, map[string]any{
		"customer_id": custID, "registration": "T123ABC", "vehicle_type": "truck",
		"default_product_id": h.ids.agoProduct.String(), "odometer_required": true,
	})
	if code != http.StatusCreated || veh["odometer_required"] != true {
		t.Fatalf("create vehicle = %d %v", code, veh)
	}
	vehID := veh["id"].(string)
	if code, _ := h.invPostJSON(t, "/api/v1/fleet/vehicles/"+vehID+"/status", admin, map[string]any{"status": "on_hold"}); code != http.StatusOK {
		t.Fatalf("vehicle status: %d", code)
	}

	// Driver with a PIN.
	code, drv := h.invPostJSON(t, "/api/v1/fleet/drivers", admin, map[string]any{
		"customer_id": custID, "name": "Sam Driver", "pin": "4821",
	})
	if code != http.StatusCreated || drv["has_pin"] != true {
		t.Fatalf("create driver = %d %v", code, drv)
	}
	drvID := drv["id"].(string)
	if code, _ := h.invPostJSON(t, "/api/v1/fleet/drivers/"+drvID+"/reset-pin", admin, map[string]any{"pin": "9999"}); code != http.StatusOK {
		t.Fatalf("reset pin: %d", code)
	}

	// Credential: issue a QR token, masked to its last 4.
	code, cred := h.invPostJSON(t, "/api/v1/fleet/credentials", admin, map[string]any{
		"customer_id": custID, "vehicle_id": vehID, "credential_type": "qr", "token": "QR-HAUL-1234",
	})
	if code != http.StatusCreated || cred["masked_label"] != "****1234" || cred["status"] != "active" {
		t.Fatalf("issue credential = %d %v", code, cred)
	}
	credID := cred["id"].(string)

	// Validation resolves the raw token to its customer context and is usable.
	code, val := h.invPostJSON(t, "/api/v1/fleet/credentials/validate", admin, map[string]any{"token": "QR-HAUL-1234"})
	if code != http.StatusOK || !val["usable"].(bool) || val["customer_name"] != "Haul Co" {
		t.Fatalf("validate credential = %d %v", code, val)
	}

	// Suspending the credential makes it unusable.
	if code, _ := h.invPostJSON(t, "/api/v1/fleet/credentials/"+credID+"/status", admin, map[string]any{"status": "suspended"}); code != http.StatusOK {
		t.Fatalf("suspend credential: %d", code)
	}
	if code, val := h.invPostJSON(t, "/api/v1/fleet/credentials/validate", admin, map[string]any{"token": "QR-HAUL-1234"}); code != http.StatusOK || val["usable"].(bool) {
		t.Fatalf("validate after suspend = %d %v", code, val)
	}

	// An unknown token is rejected.
	if code, _ := h.invPostJSON(t, "/api/v1/fleet/credentials/validate", admin, map[string]any{"token": "NOPE-0000"}); code != http.StatusNotFound {
		t.Fatalf("validate unknown: %d, want 404", code)
	}
}

// TestPhase8_Authorization covers Category C: the deterministic authorization
// decision (credit/hold/limit checks with explainable denials), approval that
// holds credit, single-use fulfillment, and override.
func TestPhase8_Authorization(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	station := h.ids.station1.String()
	code, cust := h.invPostJSON(t, "/api/v1/customers", admin, map[string]any{"code": "AUTHCO", "name": "Auth Co", "credit_limit": "10000"})
	if code != http.StatusCreated {
		t.Fatalf("create customer: %d", code)
	}
	custID := cust["id"].(string)

	// Within available credit (10,000): a 6,000 request is approved and holds credit.
	code, auth := h.invPostJSON(t, "/api/v1/fuel-authorizations", admin, map[string]any{
		"customer_id": custID, "station_id": station, "requested_amount": "6000",
	})
	if code != http.StatusCreated || auth["status"] != "approved" || auth["approved_amount"] != "6000.00" {
		t.Fatalf("approve authorization = %d %v", code, auth)
	}
	authID := auth["id"].(string)

	// Credit position now reflects the 6,000 hold: available 4,000.
	if code, pos := h.getJSON(t, "/api/v1/customers/"+custID+"/credit-position", admin); code != http.StatusOK || pos["available_credit"] != "4000.00" {
		t.Fatalf("credit position after hold = %v", pos)
	}

	// A second 6,000 request now exceeds available credit and is denied with a rule.
	code, denied := h.invPostJSON(t, "/api/v1/fuel-authorizations", admin, map[string]any{
		"customer_id": custID, "station_id": station, "requested_amount": "6000",
	})
	if code != http.StatusUnprocessableEntity || denied["rule_code"] != "insufficient_credit" {
		t.Fatalf("expected insufficient_credit denial = %d %v", code, denied)
	}

	// Override (held by the owner admin) forces an approval despite the shortfall.
	if code, _ := h.invPostJSON(t, "/api/v1/fuel-authorizations", admin, map[string]any{
		"customer_id": custID, "station_id": station, "requested_amount": "6000", "override": true,
	}); code != http.StatusCreated {
		t.Fatalf("override authorization: %d", code)
	}

	// Fulfilling the first authorization consumes it once; a second fulfill fails.
	saleRef := uuid.New().String()
	if code, _ := h.invPostJSON(t, "/api/v1/fuel-authorizations/"+authID+"/fulfill", admin, map[string]any{"consumed_by": saleRef}); code != http.StatusOK {
		t.Fatalf("fulfill authorization: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/fuel-authorizations/"+authID+"/fulfill", admin, map[string]any{"consumed_by": uuid.New().String()}); code != http.StatusConflict {
		t.Fatalf("double fulfill: %d, want 409", code)
	}

	// Placing the customer on hold then denies a fresh request.
	if code, _ := h.invPostJSON(t, "/api/v1/customers/"+custID+"/credit-hold", admin, map[string]any{"hold": true, "reason": "review"}); code != http.StatusOK {
		t.Fatalf("set hold: %d", code)
	}
	if code, denied := h.invPostJSON(t, "/api/v1/fuel-authorizations", admin, map[string]any{
		"customer_id": custID, "station_id": station, "requested_amount": "100",
	}); code != http.StatusUnprocessableEntity || denied["rule_code"] != "account_hold" {
		t.Fatalf("expected account_hold denial = %d %v", code, denied)
	}
}
