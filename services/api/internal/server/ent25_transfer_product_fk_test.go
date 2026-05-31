package server_test

// DB-backed regression test for ENT-25-DB (migration 0076): the database must
// reject a stock_transfer_orders row whose product_id does not match the
// product of BOTH the source and destination tank, mirroring the application
// guard in ReceiveTransfer. Gated on TEST_DATABASE_URL + TEST_REDIS_URL.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// fkViolation reports whether err is a Postgres foreign-key violation (23503).
func fkViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func TestENT25_TransferProductAlignment(t *testing.T) {
	h, cleanup := setupHarness(t)
	defer cleanup()
	ctx := context.Background()
	adminID, _, _ := h.adminContext(t, ctx)

	const insert = `
		INSERT INTO stock_transfer_orders
		    (tenant_id, from_tank_id, to_tank_id, product_id, litres, created_by)
		VALUES ($1, $2, $3, $4, 100, $5) RETURNING id`

	// Aligned transfer: tankPMS and tankMSA both store pmsProduct — accepted.
	var okID uuid.UUID
	if err := h.pool.QueryRow(ctx, insert,
		h.ids.tenantID, h.ids.tankPMS, h.ids.tankMSA, h.ids.pmsProduct, adminID,
	).Scan(&okID); err != nil {
		t.Fatalf("aligned transfer rejected unexpectedly: %v", err)
	}

	// Source-tank mismatch: tankAGO stores agoProduct, transfer claims pmsProduct.
	_, err := h.pool.Exec(ctx, insert,
		h.ids.tenantID, h.ids.tankAGO, h.ids.tankMSA, h.ids.pmsProduct, adminID)
	if !fkViolation(err) {
		t.Fatalf("source-tank product mismatch: want FK violation (23503), got %v", err)
	}

	// Destination-tank mismatch: tankAGO stores agoProduct, transfer claims pmsProduct.
	_, err = h.pool.Exec(ctx, insert,
		h.ids.tenantID, h.ids.tankPMS, h.ids.tankAGO, h.ids.pmsProduct, adminID)
	if !fkViolation(err) {
		t.Fatalf("destination-tank product mismatch: want FK violation (23503), got %v", err)
	}
}
