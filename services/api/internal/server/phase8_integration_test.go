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

	"github.com/japharyroman/fuelgrid-os/internal/fleet"
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
	// FLEET-007: PINs are argon2id-hashed (salted, slow), not single-round
	// SHA-256. The reset PIN verifies and the old one does not — proving the
	// hash round-trips. The harness pepper is empty, matching the server's repo.
	fleetRepo := fleet.New(h.pool, "")
	drvUUID := uuid.MustParse(drvID)
	if ok, err := fleetRepo.VerifyDriverPIN(context.Background(), h.ids.tenantID, drvUUID, "9999"); err != nil || !ok {
		t.Fatalf("argon2 PIN: correct PIN must verify (ok=%v err=%v)", ok, err)
	}
	if ok, _ := fleetRepo.VerifyDriverPIN(context.Background(), h.ids.tenantID, drvUUID, "4821"); ok {
		t.Fatal("argon2 PIN: the pre-reset PIN must not verify")
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
	adminID, _, admin := h.adminContext(t, ctx)

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

	// consumed_by must reference a real sale (W1-FLEET-FK): a random sale id is
	// rejected with 422 before any consumption happens.
	if code, _ := h.invPostJSON(t, "/api/v1/fuel-authorizations/"+authID+"/fulfill", admin, map[string]any{"consumed_by": uuid.New().String()}); code != http.StatusUnprocessableEntity {
		t.Fatalf("fulfill with unknown sale: %d, want 422", code)
	}

	// Fulfilling the first authorization with a real sale consumes it once; a
	// second fulfill (even with another valid sale) fails as already consumed.
	saleRef := seedSale(t, ctx, h, adminID, "2026-06-10").String()
	if code, _ := h.invPostJSON(t, "/api/v1/fuel-authorizations/"+authID+"/fulfill", admin, map[string]any{"consumed_by": saleRef}); code != http.StatusOK {
		t.Fatalf("fulfill authorization: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/fuel-authorizations/"+authID+"/fulfill", admin, map[string]any{"consumed_by": seedSale(t, ctx, h, adminID, "2026-06-11").String()}); code != http.StatusConflict {
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

// TestPhase8_OdometerConsumption covers Category D: odometer capture with
// monotonic validation and a per-vehicle fleet consumption report.
func TestPhase8_OdometerConsumption(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	code, cust := h.invPostJSON(t, "/api/v1/customers", admin, map[string]any{"code": "ODOCO", "name": "Odo Co", "credit_limit": "50000"})
	if code != http.StatusCreated {
		t.Fatalf("create customer: %d", code)
	}
	custID := cust["id"].(string)
	code, veh := h.invPostJSON(t, "/api/v1/fleet/vehicles", admin, map[string]any{
		"customer_id": custID, "registration": "ODO-1", "odometer_required": true,
	})
	if code != http.StatusCreated {
		t.Fatalf("create vehicle: %d", code)
	}
	vehID := veh["id"].(string)

	// First reading is valid; a lower reading flags a warning; override accepts it.
	if code, o := h.invPostJSON(t, "/api/v1/fleet/vehicles/"+vehID+"/odometer", admin, map[string]any{"reading": "10000"}); code != http.StatusCreated || o["validation_status"] != "valid" {
		t.Fatalf("first odometer = %d %v", code, o)
	}
	if code, o := h.invPostJSON(t, "/api/v1/fleet/vehicles/"+vehID+"/odometer", admin, map[string]any{"reading": "9000"}); code != http.StatusCreated || o["validation_status"] != "warning" {
		t.Fatalf("regressive odometer = %d %v", code, o)
	}
	if code, o := h.invPostJSON(t, "/api/v1/fleet/vehicles/"+vehID+"/odometer", admin, map[string]any{"reading": "10500"}); code != http.StatusCreated || o["validation_status"] != "valid" {
		t.Fatalf("advancing odometer = %d %v", code, o)
	}

	// A fulfilled authorization contributes to the consumption report.
	station := h.ids.station1.String()
	code, auth := h.invPostJSON(t, "/api/v1/fuel-authorizations", admin, map[string]any{
		"customer_id": custID, "vehicle_id": vehID, "station_id": station, "requested_amount": "4000",
	})
	if code != http.StatusCreated {
		t.Fatalf("authorization: %d %v", code, auth)
	}
	saleRef := seedSale(t, ctx, h, adminID, "2026-06-13").String()
	if code, _ := h.invPostJSON(t, "/api/v1/fuel-authorizations/"+auth["id"].(string)+"/fulfill", admin, map[string]any{"consumed_by": saleRef}); code != http.StatusOK {
		t.Fatalf("fulfill: %d", code)
	}

	code, rep := h.getJSON(t, "/api/v1/fleet/consumption?customer_id="+custID+"&from=2026-01-01&to=2026-12-31", admin)
	items, _ := rep["items"].([]any)
	if code != http.StatusOK || len(items) != 1 {
		t.Fatalf("consumption report = %d %v", code, rep)
	}
	row := items[0].(map[string]any)
	if row["fuelings"].(float64) != 1 || row["amount_total"] != "4000.00" {
		t.Fatalf("consumption row = %v", row)
	}
}

// TestPhase8_StatementsAndAlerts covers Category E: statement generation from
// the AR ledger and deterministic credit-alert scanning + lifecycle.
func TestPhase8_StatementsAndAlerts(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)

	// A customer with a small limit and a credit charge that exceeds it.
	code, cust := h.invPostJSON(t, "/api/v1/customers", admin, map[string]any{"code": "STMTCO", "name": "Stmt Co", "credit_limit": "1000"})
	if code != http.StatusCreated {
		t.Fatalf("create customer: %d", code)
	}
	custID := cust["id"].(string)

	// Seed an AR charge directly (a credit sale would do this in production).
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO ar_entries (tenant_id, customer_id, entry_type, amount, balance_after, recorded_by, recorded_at)
		VALUES ($1, $2, 'charge', 1500, 1500, $3, '2026-06-10')
	`, h.ids.tenantID, custID, mustAdminID(t, ctx, h)); err != nil {
		t.Fatalf("seed AR: %v", err)
	}

	// Generate + issue a June statement: closing balance 1,500.
	code, stmt := h.invPostJSON(t, "/api/v1/customers/"+custID+"/statements", admin,
		map[string]any{"period_start": "2026-06-01", "period_end": "2026-06-30"})
	if code != http.StatusCreated || stmt["closing_balance"] != "1500.00" || stmt["charges"] != "1500.00" {
		t.Fatalf("generate statement = %d %v", code, stmt)
	}
	if code, _ := h.do(t, http.MethodPost, "/api/v1/customer-statements/"+stmt["id"].(string)+"/issue", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("issue statement: %d", code)
	}

	// Scanning raises an over_limit alert (1,500 > 1,000).
	code, scan := h.invPostJSON(t, "/api/v1/credit-alerts/scan", admin, map[string]any{})
	if code != http.StatusOK || scan["created"].(float64) < 1 {
		t.Fatalf("scan alerts = %d %v", code, scan)
	}
	code, alerts := h.getJSON(t, "/api/v1/credit-alerts?status=open", admin)
	items, _ := alerts["items"].([]any)
	if code != http.StatusOK || len(items) < 1 {
		t.Fatalf("list alerts = %d %v", code, alerts)
	}
	alertID := items[0].(map[string]any)["id"].(string)
	// Acknowledge then resolve the alert.
	if code, _ := h.invPostJSON(t, "/api/v1/credit-alerts/"+alertID+"/acknowledge", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("acknowledge alert: %d", code)
	}
	if code, _ := h.invPostJSON(t, "/api/v1/credit-alerts/"+alertID+"/resolve", admin, map[string]any{"reason": "limit raised"}); code != http.StatusOK {
		t.Fatalf("resolve alert: %d", code)
	}
	// A re-scan does not duplicate the now-resolved alert type unless still open.
	if code, _ := h.invPostJSON(t, "/api/v1/credit-alerts/scan", admin, map[string]any{}); code != http.StatusOK {
		t.Fatalf("rescan: %d", code)
	}
}

// mustAdminID returns the seeded admin user id for direct SQL inserts.
func mustAdminID(t *testing.T, ctx context.Context, h *harness) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM users WHERE tenant_id = $1 AND email = $2`, h.ids.tenantID, h.ids.adminEmail).Scan(&id); err != nil {
		t.Fatalf("admin id: %v", err)
	}
	return id
}

// seedSale inserts a recognized Phase-6 sale and returns its id, for use as a
// valid consumed_by reference when fulfilling an authorization (W1-FLEET-FK adds
// a composite FK fuel_authorizations(tenant_id, consumed_by) -> sales). Each
// call uses a fresh day/shift so the (shift, nozzle) idempotency key is unique.
func seedSale(t *testing.T, ctx context.Context, h *harness, adminID uuid.UUID, businessDate string) uuid.UUID {
	t.Helper()
	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id = $1 AND tank_id = $2 LIMIT 1`,
		h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("nozzle: %v", err)
	}
	day, shift := seedClosedDayShift(t, ctx, h, adminID, nozzleID, businessDate, 2000)
	var saleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO sales (tenant_id, shift_id, station_id, operating_day_id, nozzle_id, product_id, tank_id,
		    litres, unit_price, gross_amount, tax_rate, tax_amount, net_amount, recorded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 2000, 2.95, 5900, 18, 900, 5000, $8)
		RETURNING id
	`, h.ids.tenantID, shift, h.ids.station1, day, nozzleID, h.ids.pmsProduct, h.ids.tankPMS, adminID).Scan(&saleID); err != nil {
		t.Fatalf("seed sale: %v", err)
	}
	return saleID
}

// TestPhase8_AuthorizationLimitsAndExpiry proves the authorization engine
// enforces windowed limits beyond per-transaction (FLEET-003/004) and that an
// expired hold stops counting toward both exposure and those limits (FLEET-005).
func TestPhase8_AuthorizationLimitsAndExpiry(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	_, _, admin := h.adminContext(t, ctx)
	station := h.ids.station1.String()

	// A high credit limit, so the *daily* limit — not credit — is the binding cap.
	code, cust := h.invPostJSON(t, "/api/v1/customers", admin,
		map[string]any{"code": "LIMCO", "name": "Lim Co", "credit_limit": "100000"})
	if code != http.StatusCreated {
		t.Fatalf("create customer: %d", code)
	}
	custID := cust["id"].(string)

	// A 5,000-per-day limit on this customer.
	if code, _ := h.invPostJSON(t, "/api/v1/fuel-limits", admin,
		map[string]any{"customer_id": custID, "period": "day", "max_amount": "5000"}); code != http.StatusCreated {
		t.Fatalf("create daily limit: %d", code)
	}

	// First 3,000 is within the daily cap.
	if code, a := h.invPostJSON(t, "/api/v1/fuel-authorizations", admin,
		map[string]any{"customer_id": custID, "station_id": station, "requested_amount": "3000"}); code != http.StatusCreated || a["status"] != "approved" {
		t.Fatalf("first auth = %d %v", code, a)
	}

	// A second 3,000 would total 6,000 > 5,000/day. Credit is ample, so the
	// daily limit is the binding rule — previously day/week/month limits were
	// inert and this would have been approved.
	code, denied := h.invPostJSON(t, "/api/v1/fuel-authorizations", admin,
		map[string]any{"customer_id": custID, "station_id": station, "requested_amount": "3000"})
	if code != http.StatusUnprocessableEntity || denied["rule_code"] != "daily_limit" {
		t.Fatalf("expected daily_limit denial = %d %v", code, denied)
	}

	// Expire the outstanding hold by hand (simulate the 1-hour TTL lapsing).
	if _, err := h.pool.Exec(ctx, `
		UPDATE fuel_authorizations SET expiry_at = now() - interval '1 hour'
		WHERE tenant_id = $1 AND customer_id = $2 AND status = 'approved'
	`, h.ids.tenantID, custID); err != nil {
		t.Fatalf("expire hold: %v", err)
	}

	// Exposure no longer counts the expired hold: full credit is available.
	if code, pos := h.getJSON(t, "/api/v1/customers/"+custID+"/credit-position", admin); code != http.StatusOK || pos["available_credit"] != "100000.00" {
		t.Fatalf("available credit after expiry = %v", pos)
	}

	// And a fresh 3,000 is approved again: requesting it auto-expires the stale
	// hold, dropping it from the daily total, so 3,000 < 5,000/day.
	if code, a := h.invPostJSON(t, "/api/v1/fuel-authorizations", admin,
		map[string]any{"customer_id": custID, "station_id": station, "requested_amount": "3000"}); code != http.StatusCreated || a["status"] != "approved" {
		t.Fatalf("post-expiry auth = %d %v", code, a)
	}
}
