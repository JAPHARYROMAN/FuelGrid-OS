package server_test

// DB-backed integration tests for the Phase 4 stock ledger (Stage 1). They
// reuse the Phase 2 harness (setupHarness / seedTenant), post movements
// straight onto the ledger via the inventory repo, and assert the read
// surface — ledger ordering, derived book balance, reversal, and the
// station-scoped inventory.read gate — over the real HTTP API.
//
// Gated on TEST_DATABASE_URL + TEST_REDIS_URL like the Phase 2 suite; skips
// when either is unset.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/inventory"
)

// adminContext logs in as the tenant-wide admin and returns its user id, the
// tenant slug, and a bearer token — the shared preamble for Phase 4 tests.
func (h *harness) adminContext(t *testing.T, ctx context.Context) (adminID uuid.UUID, slug, token string) {
	t.Helper()
	if err := h.pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, h.ids.adminEmail).Scan(&adminID); err != nil {
		t.Fatalf("lookup admin id: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `SELECT slug FROM tenants WHERE id = $1`, h.ids.tenantID).Scan(&slug); err != nil {
		t.Fatalf("lookup slug: %v", err)
	}
	return adminID, slug, h.login(t, slug, h.ids.adminEmail)
}

// invPostJSON POSTs a JSON body and decodes the response. Named distinctly
// from other suites' helpers so this file stays self-contained.
func (h *harness) invPostJSON(t *testing.T, path, token string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	code, out := h.do(t, http.MethodPost, path, token, bytes.NewReader(raw), "application/json")
	var m map[string]any
	if len(out) > 0 {
		_ = json.Unmarshal(out, &m)
	}
	return code, m
}

func TestPhase4_StockLedger(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()

	ctx := context.Background()
	repo := inventory.New(h.pool)

	var adminID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, h.ids.adminEmail).Scan(&adminID); err != nil {
		t.Fatalf("lookup admin id: %v", err)
	}

	// Seed an opening + delivery + sales movement on the PMS tank in one tx.
	// Expected book balance: 20000 + 10000 - 4200 = 25800.
	var deliveryID uuid.UUID
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	post := func(mvType string, litres float64) *inventory.Movement {
		m, err := repo.PostMovement(ctx, tx, h.ids.tenantID, inventory.PostInput{
			TankID: h.ids.tankPMS, MovementType: mvType, Litres: litres, RecordedBy: adminID,
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("post %s: %v", mvType, err)
		}
		return m
	}
	post(inventory.TypeOpening, 20000)
	delivery := post(inventory.TypeDelivery, 10000)
	deliveryID = delivery.ID
	post(inventory.TypeSales, -4200)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// The tenant slug is "ittest-<suffix>"; recover it from the DB.
	var slug string
	if err := h.pool.QueryRow(ctx, `SELECT slug FROM tenants WHERE id = $1`, h.ids.tenantID).Scan(&slug); err != nil {
		t.Fatalf("lookup slug: %v", err)
	}
	admin := h.login(t, slug, h.ids.adminEmail)
	op := h.login(t, slug, h.ids.opEmail)

	// Book balance reflects the derived sum.
	code, body := h.getJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/book-balance", admin)
	if code != http.StatusOK {
		t.Fatalf("book-balance: status %d: %v", code, body)
	}
	if got := body["book_balance"].(float64); got != 25800 {
		t.Fatalf("book_balance = %v, want 25800", got)
	}

	// Ledger lists the three movements in append order with running balances.
	code, body = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/ledger", admin)
	if code != http.StatusOK {
		t.Fatalf("ledger: status %d: %v", code, body)
	}
	items := body["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("ledger count = %d, want 3", len(items))
	}
	wantTypes := []string{"opening", "delivery", "sales"}
	wantBalances := []float64{20000, 30000, 25800}
	for i, raw := range items {
		it := raw.(map[string]any)
		if it["movement_type"] != wantTypes[i] {
			t.Fatalf("item %d type = %v, want %s", i, it["movement_type"], wantTypes[i])
		}
		if bal := it["balance_after"].(float64); bal != wantBalances[i] {
			t.Fatalf("item %d balance_after = %v, want %v", i, bal, wantBalances[i])
		}
	}

	// Reverse the delivery: a contra entry nets it to zero, balance drops to
	// 15800, and the ledger grows to four rows (original now 'reversed').
	tx, err = h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin reverse: %v", err)
	}
	contra, err := repo.ReverseMovement(ctx, tx, h.ids.tenantID, deliveryID, adminID, nil)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("reverse: %v", err)
	}
	if contra.Litres != -10000 {
		_ = tx.Rollback(ctx)
		t.Fatalf("contra litres = %v, want -10000", contra.Litres)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit reverse: %v", err)
	}

	// Reversing again is rejected.
	tx, err = h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin reverse2: %v", err)
	}
	_, err = repo.ReverseMovement(ctx, tx, h.ids.tenantID, deliveryID, adminID, nil)
	_ = tx.Rollback(ctx)
	if err != inventory.ErrAlreadyReversed {
		t.Fatalf("second reverse err = %v, want ErrAlreadyReversed", err)
	}

	code, body = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/book-balance", admin)
	if code != http.StatusOK {
		t.Fatalf("book-balance after reverse: status %d", code)
	}
	if got := body["book_balance"].(float64); got != 15800 {
		t.Fatalf("book_balance after reverse = %v, want 15800", got)
	}
	code, body = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/ledger", admin)
	if code != http.StatusOK {
		t.Fatalf("ledger after reverse: status %d", code)
	}
	if items := body["items"].([]any); len(items) != 4 {
		t.Fatalf("ledger count after reverse = %d, want 4", len(items))
	}

	// inventory.read is station-scoped: the station1-restricted operator may
	// read the PMS tank (station1) but not the station2 tank.
	code, _ = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/book-balance", op)
	if code != http.StatusOK {
		t.Fatalf("operator read in-scope tank: status %d, want 200", code)
	}
	code, _ = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankMSA.String()+"/book-balance", op)
	if code != http.StatusForbidden {
		t.Fatalf("operator read out-of-scope tank: status %d, want 403", code)
	}
}

func TestPhase4_OpeningBalance(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()

	ctx := context.Background()
	repo := inventory.New(h.pool)
	adminID, slug, admin := h.adminContext(t, ctx)
	op := h.login(t, slug, h.ids.opEmail)

	// A delivery cannot post before the tank has an opening balance.
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, err = repo.PostMovement(ctx, tx, h.ids.tenantID, inventory.PostInput{
		TankID: h.ids.tankAGO, MovementType: inventory.TypeDelivery, Litres: 5000, RecordedBy: adminID,
	})
	_ = tx.Rollback(ctx)
	if err != inventory.ErrNoOpeningBalance {
		t.Fatalf("delivery before opening err = %v, want ErrNoOpeningBalance", err)
	}

	// Manual opening balance.
	code, body := h.invPostJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/opening-balance", admin,
		map[string]any{"litres": 8000})
	if code != http.StatusCreated {
		t.Fatalf("set opening: status %d: %v", code, body)
	}
	if body["movement_type"] != "opening" || body["litres"].(float64) != 8000 {
		t.Fatalf("opening movement = %v", body)
	}

	code, body = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/book-balance", admin)
	if code != http.StatusOK || body["book_balance"].(float64) != 8000 {
		t.Fatalf("book balance after opening = %v (status %d)", body["book_balance"], code)
	}

	// Now a delivery posts cleanly: balance rises to 13000.
	tx, err = h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin delivery: %v", err)
	}
	if _, err := repo.PostMovement(ctx, tx, h.ids.tenantID, inventory.PostInput{
		TankID: h.ids.tankAGO, MovementType: inventory.TypeDelivery, Litres: 5000, RecordedBy: adminID,
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("delivery after opening: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit delivery: %v", err)
	}
	code, body = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/book-balance", admin)
	if code != http.StatusOK || body["book_balance"].(float64) != 13000 {
		t.Fatalf("book balance after delivery = %v", body["book_balance"])
	}

	// Re-setting the opening is rejected.
	code, _ = h.invPostJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/opening-balance", admin,
		map[string]any{"litres": 1000})
	if code != http.StatusConflict {
		t.Fatalf("re-set opening: status %d, want 409", code)
	}

	// from_dip: seed a first dip on the PMS tank, then open from it.
	const dipVolume = 17500.0
	seedFirstDip(t, ctx, h, adminID, h.ids.tankPMS, dipVolume)
	code, body = h.invPostJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/opening-balance", admin,
		map[string]any{"from_dip": true})
	if code != http.StatusCreated {
		t.Fatalf("set opening from dip: status %d: %v", code, body)
	}
	if body["litres"].(float64) != dipVolume {
		t.Fatalf("opening from dip litres = %v, want %v", body["litres"], dipVolume)
	}

	// from_dip with no dip is a 422.
	code, _ = h.invPostJSON(t, "/api/v1/tanks/"+h.ids.tankMSA.String()+"/opening-balance", admin,
		map[string]any{"from_dip": true})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("from_dip without dip: status %d, want 422", code)
	}

	// The station1-scoped operator cannot open a station2 tank.
	code, _ = h.invPostJSON(t, "/api/v1/tanks/"+h.ids.tankMSA.String()+"/opening-balance", op,
		map[string]any{"litres": 500})
	if code != http.StatusForbidden {
		t.Fatalf("operator open out-of-scope tank: status %d, want 403", code)
	}
}

func TestPhase4_Delivery(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()

	ctx := context.Background()
	_, slug, admin := h.adminContext(t, ctx)
	op := h.login(t, slug, h.ids.opEmail)

	tank := "/api/v1/tanks/" + h.ids.tankAGO.String()

	// Open the tank, then receive 10,000 L with no dips.
	if code, _ := h.invPostJSON(t, tank+"/opening-balance", admin, map[string]any{"litres": 8000}); code != http.StatusCreated {
		t.Fatalf("set opening: status %d", code)
	}
	code, body := h.invPostJSON(t, tank+"/deliveries", admin,
		map[string]any{"volume_litres": 10000, "supplier_ref": "Oryx"})
	if code != http.StatusCreated {
		t.Fatalf("receive delivery: status %d: %v", code, body)
	}
	if body["dip_mismatch"].(bool) {
		t.Fatalf("no-dip delivery should not flag a mismatch")
	}
	mv := body["movement"].(map[string]any)
	if mv["movement_type"] != "delivery" || mv["litres"].(float64) != 10000 || mv["balance_after"].(float64) != 18000 {
		t.Fatalf("delivery movement = %v", mv)
	}

	// Book balance rose by the delivered volume.
	if code, b := h.getJSON(t, tank+"/book-balance", admin); code != http.StatusOK || b["book_balance"].(float64) != 18000 {
		t.Fatalf("book balance after delivery = %v (status %d)", b["book_balance"], code)
	}

	// Declared volume that disagrees with the dip change is flagged (AGO's
	// loss tolerance is 0, so any gap mismatches).
	code, body = h.invPostJSON(t, tank+"/deliveries", admin, map[string]any{
		"volume_litres": 10000, "dip_before_litres": 18000, "dip_after_litres": 27500,
	})
	if code != http.StatusCreated {
		t.Fatalf("receive delivery w/ dips: status %d: %v", code, body)
	}
	if !body["dip_mismatch"].(bool) {
		t.Fatalf("declared 10000 vs dip delta 9500 should flag a mismatch")
	}
	if v := body["delivery"].(map[string]any)["dip_variance_litres"].(float64); v != 500 {
		t.Fatalf("dip_variance_litres = %v, want 500", v)
	}

	// Matching dips do not flag.
	code, body = h.invPostJSON(t, tank+"/deliveries", admin, map[string]any{
		"volume_litres": 10000, "dip_before_litres": 27500, "dip_after_litres": 37500,
	})
	if code != http.StatusCreated || body["dip_mismatch"].(bool) {
		t.Fatalf("matching dips should not flag (status %d, mismatch %v)", code, body["dip_mismatch"])
	}

	// Tank and station listings both return the three deliveries.
	if code, b := h.getJSON(t, tank+"/deliveries", admin); code != http.StatusOK || b["count"].(float64) != 3 {
		t.Fatalf("tank deliveries count = %v (status %d)", b["count"], code)
	}
	if code, b := h.getJSON(t, "/api/v1/stations/"+h.ids.station1.String()+"/deliveries", admin); code != http.StatusOK || b["count"].(float64) != 3 {
		t.Fatalf("station deliveries count = %v (status %d)", b["count"], code)
	}

	// A delivery into a tank with no opening balance is rejected.
	msaTank := "/api/v1/tanks/" + h.ids.tankMSA.String() + "/deliveries"
	if code, _ := h.invPostJSON(t, msaTank, admin, map[string]any{"volume_litres": 5000}); code != http.StatusConflict {
		t.Fatalf("delivery before opening: status %d, want 409", code)
	}

	// The station1-scoped operator cannot receive into a station2 tank.
	if code, _ := h.invPostJSON(t, msaTank, op, map[string]any{"volume_litres": 5000}); code != http.StatusForbidden {
		t.Fatalf("operator cross-station delivery: status %d, want 403", code)
	}
}

// seedFirstDip inserts the operating-day -> shift -> chart -> dip chain needed
// to give a tank a first dip reading at the given volume.
func seedFirstDip(t *testing.T, ctx context.Context, h *harness, recordedBy, tankID uuid.UUID, volume float64) {
	t.Helper()
	var dayID, shiftID, chartID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO operating_days (tenant_id, station_id, business_date, opened_by)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, h.ids.tenantID, h.ids.station1, time.Now().Format("2006-01-02"), recordedBy).Scan(&dayID); err != nil {
		t.Fatalf("seed operating day: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO shifts (tenant_id, station_id, operating_day_id, name, opened_by)
		VALUES ($1, $2, $3, 'Day', $4) RETURNING id
	`, h.ids.tenantID, h.ids.station1, dayID, recordedBy).Scan(&shiftID); err != nil {
		t.Fatalf("seed shift: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO tank_calibration_charts (tenant_id, tank_id, name)
		VALUES ($1, $2, 'Chart') RETURNING id
	`, h.ids.tenantID, tankID).Scan(&chartID); err != nil {
		t.Fatalf("seed chart: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO tank_dip_readings (tenant_id, shift_id, tank_id, reading_type, dip_mm, volume_litres, chart_id, recorded_by)
		VALUES ($1, $2, $3, 'opening', 1500, $4, $5, $6)
	`, h.ids.tenantID, shiftID, tankID, volume, chartID, recordedBy); err != nil {
		t.Fatalf("seed dip: %v", err)
	}
}
