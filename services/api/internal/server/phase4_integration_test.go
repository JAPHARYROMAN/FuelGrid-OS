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
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/inventory"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
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
	post := func(mvType string, litres string) *inventory.Movement {
		m, err := repo.PostMovement(ctx, tx, h.ids.tenantID, inventory.PostInput{
			TankID: h.ids.tankPMS, MovementType: mvType, Litres: litres, RecordedBy: adminID,
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("post %s: %v", mvType, err)
		}
		return m
	}
	post(inventory.TypeOpening, "20000")
	delivery := post(inventory.TypeDelivery, "10000")
	deliveryID = delivery.ID
	post(inventory.TypeSales, "-4200")
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
	if got := body["book_balance"].(string); got != "25800.000" {
		t.Fatalf("book_balance = %v, want 25800.000", got)
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
	wantBalances := []string{"20000.000", "30000.000", "25800.000"}
	for i, raw := range items {
		it := raw.(map[string]any)
		if it["movement_type"] != wantTypes[i] {
			t.Fatalf("item %d type = %v, want %s", i, it["movement_type"], wantTypes[i])
		}
		if bal := it["balance_after"].(string); bal != wantBalances[i] {
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
	if contra.Litres != "-10000.000" {
		_ = tx.Rollback(ctx)
		t.Fatalf("contra litres = %v, want -10000.000", contra.Litres)
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
	if !errors.Is(err, inventory.ErrAlreadyReversed) {
		t.Fatalf("second reverse err = %v, want ErrAlreadyReversed", err)
	}

	code, body = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/book-balance", admin)
	if code != http.StatusOK {
		t.Fatalf("book-balance after reverse: status %d", code)
	}
	if got := body["book_balance"].(string); got != "15800.000" {
		t.Fatalf("book_balance after reverse = %v, want 15800.000", got)
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

// TestPhase4_StockMovementImmutable covers INV-002: the stock ledger is
// append-only at the database. A posted movement's litres cannot be UPDATE-d
// and the row cannot be DELETE-d directly; the only permitted mutation is the
// posted->reversed annotation that ReverseMovement performs.
func TestPhase4_StockMovementImmutable(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	repo := inventory.New(h.pool)

	var adminID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, h.ids.adminEmail).Scan(&adminID); err != nil {
		t.Fatalf("lookup admin id: %v", err)
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := repo.PostMovement(ctx, tx, h.ids.tenantID, inventory.PostInput{
		TankID: h.ids.tankPMS, MovementType: inventory.TypeOpening, Litres: "15000", RecordedBy: adminID,
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("post opening: %v", err)
	}
	delivery, err := repo.PostMovement(ctx, tx, h.ids.tenantID, inventory.PostInput{
		TankID: h.ids.tankPMS, MovementType: inventory.TypeDelivery, Litres: "5000", RecordedBy: adminID,
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("post delivery: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Direct litres rewrite is rejected by the immutability trigger.
	if _, err := h.pool.Exec(ctx,
		`UPDATE stock_movements SET litres = litres + 1 WHERE id = $1`, delivery.ID); err == nil {
		t.Fatal("expected UPDATE of posted movement litres to be rejected")
	}
	// Direct delete is rejected (no app.allow_ledger_delete on this conn).
	if _, err := h.pool.Exec(ctx,
		`DELETE FROM stock_movements WHERE id = $1`, delivery.ID); err == nil {
		t.Fatal("expected DELETE of posted movement to be rejected")
	}

	// The legitimate correction path — ReverseMovement's posted->reversed
	// annotation plus a contra entry — still succeeds through the trigger.
	rtx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin reverse: %v", err)
	}
	if _, err := repo.ReverseMovement(ctx, rtx, h.ids.tenantID, delivery.ID, adminID, nil); err != nil {
		_ = rtx.Rollback(ctx)
		t.Fatalf("reverse movement: %v", err)
	}
	if err := rtx.Commit(ctx); err != nil {
		t.Fatalf("commit reverse: %v", err)
	}
}

// TestPhase4_TankDeleteBlockedByLedger covers ORG-03: a tank that has been
// opened carries stock-ledger history and can't be deleted (it would orphan
// the ledger). tankMSA has no nozzles, so this isolates the ledger guard from
// the live-nozzle guard.
func TestPhase4_TankDeleteBlockedByLedger(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	repo := inventory.New(h.pool)

	var adminID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, h.ids.adminEmail).Scan(&adminID); err != nil {
		t.Fatalf("lookup admin id: %v", err)
	}
	var slug string
	if err := h.pool.QueryRow(ctx, `SELECT slug FROM tenants WHERE id = $1`, h.ids.tenantID).Scan(&slug); err != nil {
		t.Fatalf("lookup slug: %v", err)
	}
	admin := h.login(t, slug, h.ids.adminEmail)

	// Open tankMSA (no nozzles feed it), giving it ledger history.
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := repo.PostMovement(ctx, tx, h.ids.tenantID, inventory.PostInput{
		TankID: h.ids.tankMSA, MovementType: inventory.TypeOpening, Litres: "5000", RecordedBy: adminID,
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("post opening: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if code, _ := h.do(t, http.MethodDelete, "/api/v1/tanks/"+h.ids.tankMSA.String(), admin, nil, ""); code != http.StatusConflict {
		t.Fatalf("delete tank with ledger = %d, want 409", code)
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
		TankID: h.ids.tankAGO, MovementType: inventory.TypeDelivery, Litres: "5000", RecordedBy: adminID,
	})
	_ = tx.Rollback(ctx)
	if !errors.Is(err, inventory.ErrNoOpeningBalance) {
		t.Fatalf("delivery before opening err = %v, want ErrNoOpeningBalance", err)
	}

	// Manual opening balance.
	code, body := h.invPostJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/opening-balance", admin,
		map[string]any{"litres": 8000})
	if code != http.StatusCreated {
		t.Fatalf("set opening: status %d: %v", code, body)
	}
	if body["movement_type"] != "opening" || body["litres"].(string) != "8000.000" {
		t.Fatalf("opening movement = %v", body)
	}

	code, body = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/book-balance", admin)
	if code != http.StatusOK || body["book_balance"].(string) != "8000.000" {
		t.Fatalf("book balance after opening = %v (status %d)", body["book_balance"], code)
	}

	// Now a delivery posts cleanly: balance rises to 13000.
	tx, err = h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin delivery: %v", err)
	}
	if _, err := repo.PostMovement(ctx, tx, h.ids.tenantID, inventory.PostInput{
		TankID: h.ids.tankAGO, MovementType: inventory.TypeDelivery, Litres: "5000", RecordedBy: adminID,
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("delivery after opening: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit delivery: %v", err)
	}
	code, body = h.getJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/book-balance", admin)
	if code != http.StatusOK || body["book_balance"].(string) != "13000.000" {
		t.Fatalf("book balance after delivery = %v", body["book_balance"])
	}

	// Re-setting the opening is rejected with 409 (INV-010 / FIN-6): a tank
	// may carry AT MOST ONE posted opening, enforced by the partial unique
	// index uq_stock_mvt_one_opening (migration 0072). A second opening would
	// otherwise double-count in reconciliation.
	code, _ = h.invPostJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/opening-balance", admin,
		map[string]any{"litres": 1000})
	if code != http.StatusConflict {
		t.Fatalf("re-set opening: status %d, want 409", code)
	}
	// And exactly one genuine opening row survives the rejected second attempt
	// (the loser's INSERT must not have landed).
	var openings int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM stock_movements
		WHERE tenant_id = $1 AND tank_id = $2 AND movement_type = 'opening'
		  AND status = 'posted'
		  AND (source_ref_type IS NULL OR source_ref_type <> 'correction')
	`, h.ids.tenantID, h.ids.tankAGO).Scan(&openings); err != nil {
		t.Fatalf("count openings: %v", err)
	}
	if openings != 1 {
		t.Fatalf("opening rows after rejected re-set = %d, want exactly 1", openings)
	}

	// from_dip: seed a first dip on the PMS tank, then open from it.
	const dipVolume = 17500.0
	seedFirstDip(t, ctx, h, adminID, h.ids.tankPMS, dipVolume)
	code, body = h.invPostJSON(t, "/api/v1/tanks/"+h.ids.tankPMS.String()+"/opening-balance", admin,
		map[string]any{"from_dip": true})
	if code != http.StatusCreated {
		t.Fatalf("set opening from dip: status %d: %v", code, body)
	}
	if body["litres"].(string) != "17500.000" {
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
	if mv["movement_type"] != "delivery" || mv["litres"].(string) != "10000.000" || mv["balance_after"].(string) != "18000.000" {
		t.Fatalf("delivery movement = %v", mv)
	}

	// Book balance rose by the delivered volume.
	if code, b := h.getJSON(t, tank+"/book-balance", admin); code != http.StatusOK || b["book_balance"].(string) != "18000.000" {
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

func TestPhase4_SalesOnApproval(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()

	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)
	invRepo := inventory.New(h.pool)
	opsRepo := operations.New(h.pool)

	pmsTank := "/api/v1/tanks/" + h.ids.tankPMS.String()

	// Open the PMS tank to 30,000 L.
	if code, _ := h.invPostJSON(t, pmsTank+"/opening-balance", admin, map[string]any{"litres": 30000}); code != http.StatusCreated {
		t.Fatalf("set opening: %d", code)
	}

	// Seed a closed shift that sold 4,200 L through the PMS nozzle.
	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id = $1 AND tank_id = $2 LIMIT 1`,
		h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("lookup nozzle: %v", err)
	}
	_, shiftID := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-05-20", 4200)

	// Aggregation rolls the close line up to the tank.
	rows, err := opsRepo.LitresSoldPerTankForShift(ctx, h.ids.tenantID, shiftID)
	if err != nil {
		t.Fatalf("aggregate sales: %v", err)
	}
	if len(rows) != 1 || rows[0].TankID != h.ids.tankPMS || rows[0].LitresSold != 4200 {
		t.Fatalf("per-tank sales = %v, want [{tankPMS 4200}]", rows)
	}

	// Approving the shift posts a -4,200 sales movement and drops book stock.
	code, raw := h.do(t, http.MethodPatch, "/api/v1/shifts/"+shiftID.String()+"/status", admin,
		bytes.NewReader([]byte(`{"status":"approved"}`)), "application/json")
	if code != http.StatusOK {
		t.Fatalf("approve shift: status %d: %s", code, raw)
	}

	code, body := h.getJSON(t, pmsTank+"/book-balance", admin)
	if code != http.StatusOK || body["book_balance"].(string) != "25800.000" {
		t.Fatalf("book balance after sales = %v (status %d), want 25800.000", body["book_balance"], code)
	}

	// The ledger carries the sales movement keyed to the shift.
	_, ledger := h.getJSON(t, pmsTank+"/ledger", admin)
	var sale map[string]any
	for _, it := range ledger["items"].([]any) {
		m := it.(map[string]any)
		if m["movement_type"] == "sales" {
			sale = m
		}
	}
	if sale == nil {
		t.Fatalf("no sales movement in ledger: %v", ledger["items"])
	}
	if sale["litres"].(string) != "-4200.000" || sale["source_ref_type"] != "shift" || sale["source_ref_id"] != shiftID.String() {
		t.Fatalf("sales movement = %v", sale)
	}

	// Idempotency: re-posting the same shift's sales is a no-op, balance holds.
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	posted, skipped, err := invRepo.PostSalesForShift(ctx, tx, h.ids.tenantID, shiftID, adminID,
		[]inventory.SaleLine{{TankID: h.ids.tankPMS, LitresSold: 4200}})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("re-post sales: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(posted) != 0 || len(skipped) != 0 {
		t.Fatalf("re-post should be a no-op, got posted=%d skipped=%d", len(posted), len(skipped))
	}
	if code, b := h.getJSON(t, pmsTank+"/book-balance", admin); b["book_balance"].(string) != "25800.000" {
		t.Fatalf("book balance after re-post = %v (status %d), want 25800.000 (no double-count)", b["book_balance"], code)
	}

	// Skip: a tank with no opening balance is skipped, not posted.
	tx, err = h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	posted, skipped, err = invRepo.PostSalesForShift(ctx, tx, h.ids.tenantID, uuid.New(), adminID,
		[]inventory.SaleLine{{TankID: h.ids.tankAGO, LitresSold: 100}})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("post sales unopened: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(posted) != 0 || len(skipped) != 1 || skipped[0] != h.ids.tankAGO {
		t.Fatalf("unopened tank should be skipped, got posted=%d skipped=%v", len(posted), skipped)
	}
	// An empty ledger sums to COALESCE(NULL, 0)::text = "0" (the integer-literal
	// default carries no scale), distinct from a numeric(14,3) sum's "N.000".
	if _, b := h.getJSON(t, "/api/v1/tanks/"+h.ids.tankAGO.String()+"/book-balance", admin); b["book_balance"].(string) != "0" {
		t.Fatalf("unopened tank balance = %v, want \"0\"", b["book_balance"])
	}
}

func TestPhase4_Reconciliation(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	// Give PMS a 1% loss tolerance so we can exercise both sides of the band.
	if _, err := h.pool.Exec(ctx, `UPDATE products SET loss_tolerance_percent = 1.0 WHERE id = $1`, h.ids.pmsProduct); err != nil {
		t.Fatalf("set tolerance: %v", err)
	}
	pms := "/api/v1/tanks/" + h.ids.tankPMS.String()
	if code, _ := h.invPostJSON(t, pms+"/opening-balance", admin, map[string]any{"litres": 30000}); code != http.StatusCreated {
		t.Fatalf("open: %d", code)
	}

	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`,
		h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("nozzle: %v", err)
	}
	chartID := seedChart(t, ctx, h, h.ids.tankPMS)

	// --- Day 1: sells 4,200 L; physical comes in 800 L short (over 1% tol). ---
	day1, shift1 := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-05-21", 4200)
	seedClosingDip(t, ctx, h, shift1, h.ids.tankPMS, chartID, adminID, 25000)

	// Guard: reconciliation can't run before the day's shifts are approved.
	if code, _ := h.invPostJSON(t, pms+"/reconciliations", admin, map[string]any{"operating_day_id": day1.String()}); code != http.StatusConflict {
		t.Fatalf("reconcile before approval: %d, want 409", code)
	}
	// Approve shift1 -> posts -4,200 sales -> book 25,800.
	if code, raw := h.do(t, http.MethodPatch, "/api/v1/shifts/"+shift1.String()+"/status", admin,
		bytes.NewReader([]byte(`{"status":"approved"}`)), "application/json"); code != http.StatusOK {
		t.Fatalf("approve shift1: %d %s", code, raw)
	}

	// Preview computes book vs physical without persisting. Figures are exact
	// decimal STRINGS (numeric(14,3) ::text), never JSON floats.
	if code, prev := h.getJSON(t, pms+"/reconciliation-preview?operating_day_id="+day1.String(), admin); code != http.StatusOK ||
		prev["closing_book"].(string) != "25800.000" || !prev["over_tolerance"].(bool) {
		t.Fatalf("preview = %v (status %d)", prev, code)
	}

	// Persist: over tolerance (variance 800 > 258) -> exception.
	code, body := h.invPostJSON(t, pms+"/reconciliations", admin, map[string]any{"operating_day_id": day1.String()})
	if code != http.StatusCreated {
		t.Fatalf("persist recon: %d %v", code, body)
	}
	if body["status"] != "exception" || !body["over_tolerance"].(bool) ||
		body["opening_book"].(string) != "30000.000" || body["sales_total"].(string) != "4200.000" ||
		body["closing_book"].(string) != "25800.000" || body["closing_physical"].(string) != "25000.000" ||
		body["variance_litres"].(string) != "800.000" {
		t.Fatalf("day1 figures = %v", body)
	}
	reconID := body["id"].(string)

	// Seal is blocked while over tolerance.
	if code, _ := h.do(t, http.MethodPost, "/api/v1/reconciliations/"+reconID+"/seal", admin, nil, ""); code != http.StatusConflict {
		t.Fatalf("seal over tolerance: %d, want 409", code)
	}

	// A justified -800 adjustment brings book to physical, within tolerance.
	code, body = h.invPostJSON(t, "/api/v1/reconciliations/"+reconID+"/adjustments", admin,
		map[string]any{"litres": -800, "reason": "tank leak write-off"})
	if code != http.StatusOK {
		t.Fatalf("adjust: %d %v", code, body)
	}
	if body["status"] != "draft" || body["over_tolerance"].(bool) ||
		body["adjustments_total"].(string) != "-800.000" || body["closing_book"].(string) != "25000.000" ||
		body["variance_litres"].(string) != "0.000" {
		t.Fatalf("after adjustment = %v", body)
	}

	// Seal freezes the reconciliation.
	code, raw := h.do(t, http.MethodPost, "/api/v1/reconciliations/"+reconID+"/seal", admin, nil, "")
	if code != http.StatusOK {
		t.Fatalf("seal: %d %s", code, raw)
	}
	var sealed map[string]any
	_ = json.Unmarshal(raw, &sealed)
	if sealed["status"] != "sealed" {
		t.Fatalf("sealed status = %v", sealed["status"])
	}
	// Ledger is reconciled to physical.
	if _, b := h.getJSON(t, pms+"/book-balance", admin); b["book_balance"].(string) != "25000.000" {
		t.Fatalf("book balance after seal = %v, want 25000.000", b["book_balance"])
	}
	// Re-sealing is rejected.
	if code, _ := h.do(t, http.MethodPost, "/api/v1/reconciliations/"+reconID+"/seal", admin, nil, ""); code != http.StatusConflict {
		t.Fatalf("re-seal: %d, want 409", code)
	}

	// --- Day 2: balance-forward — opening book must equal day 1's physical. ---
	day2, shift2 := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-05-22", 1000)
	seedClosingDip(t, ctx, h, shift2, h.ids.tankPMS, chartID, adminID, 24000)
	if code, raw := h.do(t, http.MethodPatch, "/api/v1/shifts/"+shift2.String()+"/status", admin,
		bytes.NewReader([]byte(`{"status":"approved"}`)), "application/json"); code != http.StatusOK {
		t.Fatalf("approve shift2: %d %s", code, raw)
	}
	code, body = h.invPostJSON(t, pms+"/reconciliations", admin, map[string]any{"operating_day_id": day2.String()})
	if code != http.StatusCreated {
		t.Fatalf("persist day2 recon: %d %v", code, body)
	}
	if body["opening_book"].(string) != "25000.000" {
		t.Fatalf("day2 opening_book = %v, want 25000.000 (balance-forward from day1 physical)", body["opening_book"])
	}
	if body["closing_book"].(string) != "24000.000" || body["variance_litres"].(string) != "0.000" || body["status"] != "draft" {
		t.Fatalf("day2 figures = %v", body)
	}
}

// TestPhase4_Reconciliation_DecimalExactness is the INV-001 proof: it drives a
// reconciliation whose figures expose float64 drift, and asserts the EXACT
// decimal strings the SQL-numeric path produces.
//
// The vector: book = 20000.000 L (open 30000, sell 10000), physical dip =
// 19999.830 L, a 0.170 L variance. variance_percent is 0.170 / 20000 * 100 =
// exactly 0.00085 %, which numeric(10,4) rounds half-away-from-zero to 0.0009.
//
// Under the OLD Go float64 path the dip scans back as 19999.830000000002, the
// variance is 0.16999999999825377, and the percent is 0.00084999999... which
// numeric(10,4) rounds DOWN to 0.0008. So asserting variance_percent == "0.0009"
// FAILS if the math is done in float64 and PASSES with SQL numeric. The test
// then seals (write-off −0.170, exact) and asserts day 2's opening book carries
// forward as exactly 19999.830 — the cascade the float residue would corrupt.
func TestPhase4_Reconciliation_DecimalExactness(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	// 1% loss tolerance: the 0.170 L variance (band 200 L) is well within it, so
	// the reconciliation is sealable.
	if _, err := h.pool.Exec(ctx, `UPDATE products SET loss_tolerance_percent = 1.0 WHERE id = $1`, h.ids.pmsProduct); err != nil {
		t.Fatalf("set tolerance: %v", err)
	}
	pms := "/api/v1/tanks/" + h.ids.tankPMS.String()
	if code, _ := h.invPostJSON(t, pms+"/opening-balance", admin, map[string]any{"litres": 30000}); code != http.StatusCreated {
		t.Fatalf("open: %d", code)
	}

	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`,
		h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("nozzle: %v", err)
	}
	chartID := seedChart(t, ctx, h, h.ids.tankPMS)

	// --- Day 1: sell 10,000 L -> book 20000.000; physical 19999.830 (0.170 short). ---
	day1, shift1 := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-01", 10000)
	seedClosingDip(t, ctx, h, shift1, h.ids.tankPMS, chartID, adminID, 19999.830)
	if code, raw := h.do(t, http.MethodPatch, "/api/v1/shifts/"+shift1.String()+"/status", admin,
		bytes.NewReader([]byte(`{"status":"approved"}`)), "application/json"); code != http.StatusOK {
		t.Fatalf("approve shift1: %d %s", code, raw)
	}

	code, body := h.invPostJSON(t, pms+"/reconciliations", admin, map[string]any{"operating_day_id": day1.String()})
	if code != http.StatusCreated {
		t.Fatalf("persist recon: %d %v", code, body)
	}
	// The figures must be the EXACT SQL-numeric decimals. variance_percent is the
	// float-distinguishing assertion: float64 yields 0.0008, SQL numeric 0.0009.
	if got := body["closing_book"].(string); got != "20000.000" {
		t.Fatalf("closing_book = %q, want 20000.000", got)
	}
	if got := body["closing_physical"].(string); got != "19999.830" {
		t.Fatalf("closing_physical = %q, want 19999.830", got)
	}
	if got := body["variance_litres"].(string); got != "0.170" {
		t.Fatalf("variance_litres = %q, want 0.170 (float64 yields 0.16999999999825377)", got)
	}
	if got := body["variance_percent"].(string); got != "0.0009" {
		t.Fatalf("variance_percent = %q, want 0.0009 — float64 rounds 0.00084999... DOWN to 0.0008", got)
	}
	if body["over_tolerance"].(bool) || body["status"] != "draft" {
		t.Fatalf("0.170 L variance must be within 1%% tolerance: %v", body)
	}
	reconID := body["id"].(string)

	// Seal: the write-off (physical − book = −0.170, exact) lands the ledger on
	// 19999.830 exactly, with no float residue to carry into day 2.
	if code, raw := h.do(t, http.MethodPost, "/api/v1/reconciliations/"+reconID+"/seal", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("seal: %d %s", code, raw)
	}
	// The ledger book balance after seal must be the exact physical figure. Read
	// it as exact numeric text so a float residue would be visible.
	var bookText string
	if err := h.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(litres),0)::text FROM stock_movements WHERE tenant_id=$1 AND tank_id=$2
	`, h.ids.tenantID, h.ids.tankPMS).Scan(&bookText); err != nil {
		t.Fatalf("ledger sum: %v", err)
	}
	if bookText != "19999.830" {
		t.Fatalf("post-seal ledger sum = %q, want 19999.830 (write-off must be exactly -0.170)", bookText)
	}

	// --- Day 2: opening book must carry forward as exactly 19999.830. ---
	day2, shift2 := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-06-02", 0)
	seedClosingDip(t, ctx, h, shift2, h.ids.tankPMS, chartID, adminID, 19999.830)
	if code, raw := h.do(t, http.MethodPatch, "/api/v1/shifts/"+shift2.String()+"/status", admin,
		bytes.NewReader([]byte(`{"status":"approved"}`)), "application/json"); code != http.StatusOK {
		t.Fatalf("approve shift2: %d %s", code, raw)
	}
	code, body = h.invPostJSON(t, pms+"/reconciliations", admin, map[string]any{"operating_day_id": day2.String()})
	if code != http.StatusCreated {
		t.Fatalf("persist day2 recon: %d %v", code, body)
	}
	if got := body["opening_book"].(string); got != "19999.830" {
		t.Fatalf("day2 opening_book = %q, want 19999.830 (exact balance-forward from sealed physical)", got)
	}
	if got := body["closing_book"].(string); got != "19999.830" {
		t.Fatalf("day2 closing_book = %q, want 19999.830", got)
	}
	if got := body["variance_litres"].(string); got != "0.000" || body["variance_percent"].(string) != "0.0000" {
		t.Fatalf("day2 variance must be exactly zero: variance=%q pct=%q", got, body["variance_percent"])
	}
}

func TestPhase4_Overviews(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, admin := h.adminContext(t, ctx)

	// PMS gets a 5% tolerance so the seeded variance is within tolerance.
	if _, err := h.pool.Exec(ctx, `UPDATE products SET loss_tolerance_percent = 5.0 WHERE id = $1`, h.ids.pmsProduct); err != nil {
		t.Fatalf("set tolerance: %v", err)
	}
	pms := "/api/v1/tanks/" + h.ids.tankPMS.String()
	if code, _ := h.invPostJSON(t, pms+"/opening-balance", admin, map[string]any{"litres": 30000}); code != http.StatusCreated {
		t.Fatalf("open: %d", code)
	}
	var nozzleID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM nozzles WHERE tenant_id=$1 AND tank_id=$2 LIMIT 1`,
		h.ids.tenantID, h.ids.tankPMS).Scan(&nozzleID); err != nil {
		t.Fatalf("nozzle: %v", err)
	}
	chartID := seedChart(t, ctx, h, h.ids.tankPMS)

	// A day selling 4,200 L; physical 25,000 (800 L variance, within 5%). Approve,
	// reconcile, and seal so the overviews have a sealed reconciliation to report.
	day, shift := seedClosedDayShift(t, ctx, h, adminID, nozzleID, "2026-05-25", 4200)
	seedClosingDip(t, ctx, h, shift, h.ids.tankPMS, chartID, adminID, 25000)
	if code, raw := h.do(t, http.MethodPatch, "/api/v1/shifts/"+shift.String()+"/status", admin,
		bytes.NewReader([]byte(`{"status":"approved"}`)), "application/json"); code != http.StatusOK {
		t.Fatalf("approve: %d %s", code, raw)
	}
	code, recon := h.invPostJSON(t, pms+"/reconciliations", admin, map[string]any{"operating_day_id": day.String()})
	if code != http.StatusCreated || recon["status"] != "draft" {
		t.Fatalf("persist recon: %d %v", code, recon)
	}
	if code, raw := h.do(t, http.MethodPost, "/api/v1/reconciliations/"+recon["id"].(string)+"/seal", admin, nil, ""); code != http.StatusOK {
		t.Fatalf("seal: %d %s", code, raw)
	}

	// --- inventory-overview ---
	code, inv := h.getJSON(t, "/api/v1/stations/"+h.ids.station1.String()+"/inventory-overview", admin)
	if code != http.StatusOK {
		t.Fatalf("inventory-overview: %d %v", code, inv)
	}
	pmsInv := findTankEntry(t, inv["tanks"].([]any), h.ids.tankPMS.String())
	if pmsInv["book_balance"].(string) != "25000.000" {
		t.Fatalf("inventory book_balance = %v, want 25000.000 (post-seal)", pmsInv["book_balance"])
	}
	if pmsInv["latest_physical"].(string) != "25000.000" {
		t.Fatalf("inventory latest_physical = %v, want 25000.000", pmsInv["latest_physical"])
	}
	lastRecon := pmsInv["last_reconciliation"].(map[string]any)
	if lastRecon["over_tolerance"].(bool) {
		t.Fatalf("last reconciliation should be within tolerance")
	}

	// --- reconciliation-overview ---
	code, rec := h.getJSON(t, "/api/v1/stations/"+h.ids.station1.String()+"/reconciliation-overview", admin)
	if code != http.StatusOK {
		t.Fatalf("reconciliation-overview: %d %v", code, rec)
	}
	if !rec["all_shifts_approved"].(bool) {
		t.Fatalf("all_shifts_approved should be true")
	}
	pmsRec := findTankEntry(t, rec["tanks"].([]any), h.ids.tankPMS.String())
	if pmsRec["reconciliation"] == nil {
		t.Fatalf("PMS tank should carry a reconciliation")
	}
	if pmsRec["reconciliation"].(map[string]any)["status"] != "sealed" {
		t.Fatalf("PMS reconciliation status = %v, want sealed", pmsRec["reconciliation"])
	}
}

// findTankEntry returns the overview tank entry whose nested tank.id matches.
func findTankEntry(t *testing.T, tanks []any, tankID string) map[string]any {
	t.Helper()
	for _, raw := range tanks {
		entry := raw.(map[string]any)
		if entry["tank"].(map[string]any)["id"] == tankID {
			return entry
		}
	}
	t.Fatalf("tank %s not found in overview", tankID)
	return nil
}

// seedClosedDayShift inserts an operating day (on the given business date) and
// a closed shift on station1 with a single frozen close line on the nozzle, so
// the approval path has metered sales to draw down. Returns (dayID, shiftID).
func seedClosedDayShift(t *testing.T, ctx context.Context, h *harness, openedBy, nozzleID uuid.UUID, businessDate string, litresSold float64) (uuid.UUID, uuid.UUID) {
	t.Helper()
	var dayID, shiftID uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO operating_days (tenant_id, station_id, business_date, opened_by)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, h.ids.tenantID, h.ids.station1, businessDate, openedBy).Scan(&dayID); err != nil {
		t.Fatalf("seed operating day: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO shifts (tenant_id, station_id, operating_day_id, name, opened_by, status, closed_by, closed_at)
		VALUES ($1, $2, $3, 'Sale', $4, 'closed', $5, now()) RETURNING id
	`, h.ids.tenantID, h.ids.station1, dayID, openedBy, h.ids.opID).Scan(&shiftID); err != nil {
		t.Fatalf("seed shift: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO shift_close_lines
		    (tenant_id, shift_id, nozzle_id, opening_reading, closing_reading, litres_sold, unit_price, expected_value)
		VALUES ($1, $2, $3, 0, $4::numeric, $4::numeric, 2950, $4::numeric * 2950)
	`, h.ids.tenantID, shiftID, nozzleID, litresSold); err != nil {
		t.Fatalf("seed close line: %v", err)
	}
	return dayID, shiftID
}

// seedChart creates the tank's active calibration chart (one per tank), needed
// as the FK target for seeded dips.
func seedChart(t *testing.T, ctx context.Context, h *harness, tankID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO tank_calibration_charts (tenant_id, tank_id, name) VALUES ($1, $2, 'Recon Chart') RETURNING id
	`, h.ids.tenantID, tankID).Scan(&id); err != nil {
		t.Fatalf("seed chart: %v", err)
	}
	return id
}

// seedClosingDip records a closing dip for a tank on a shift at the given
// physical volume — the figure a reconciliation compares book stock against.
func seedClosingDip(t *testing.T, ctx context.Context, h *harness, shiftID, tankID, chartID, recordedBy uuid.UUID, volume float64) {
	t.Helper()
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO tank_dip_readings (tenant_id, shift_id, tank_id, reading_type, dip_mm, volume_litres, chart_id, recorded_by)
		VALUES ($1, $2, $3, 'closing', 1000, $4, $5, $6)
	`, h.ids.tenantID, shiftID, tankID, volume, chartID, recordedBy); err != nil {
		t.Fatalf("seed closing dip: %v", err)
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
