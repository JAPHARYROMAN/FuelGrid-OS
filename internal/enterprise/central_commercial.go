package enterprise

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrInsufficientStock = errors.New("enterprise: insufficient source stock for transfer")

// ErrProductMismatch is returned when a stock transfer's source or destination
// tank does not hold the transfer's product — moving fuel between tanks of
// different products would corrupt both ledgers (ENT-25).
var ErrProductMismatch = errors.New("enterprise: transfer product does not match both tanks")

// ---- Central pricing (Stage 7) ----

type PriceRollout struct {
	ID              uuid.UUID
	ProductID       uuid.UUID
	ScopeType       string
	ScopeID         *uuid.UUID
	UnitPrice       string
	EffectiveFrom   time.Time
	Status          string
	StationsApplied int
}

const rolloutColumns = `id, product_id, scope_type, scope_id, unit_price::text, effective_from, status, stations_applied`

func scanRollout(row pgx.Row, p *PriceRollout) error {
	return row.Scan(&p.ID, &p.ProductID, &p.ScopeType, &p.ScopeID, &p.UnitPrice, &p.EffectiveFrom, &p.Status, &p.StationsApplied)
}

func (r *Repo) CreatePriceRollout(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, productID uuid.UUID, scopeType string, scopeID *uuid.UUID, unitPrice string, effectiveFrom time.Time, createdBy uuid.UUID) (*PriceRollout, error) {
	var p PriceRollout
	if err := scanRollout(tx.QueryRow(ctx, `
		INSERT INTO central_price_rollouts (tenant_id, product_id, scope_type, scope_id, unit_price, effective_from, created_by)
		VALUES ($1, $2, COALESCE(NULLIF($3,''),'tenant'), $4, $5::numeric, $6, $7)
		RETURNING `+rolloutColumns,
		tenantID, productID, scopeType, scopeID, unitPrice, effectiveFrom, createdBy,
	), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repo) ListPriceRollouts(ctx context.Context, tenantID uuid.UUID) ([]PriceRollout, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+rolloutColumns+` FROM central_price_rollouts WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PriceRollout{}
	for rows.Next() {
		var p PriceRollout
		if err := scanRollout(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) ApprovePriceRollout(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*PriceRollout, error) {
	var p PriceRollout
	err := scanRollout(tx.QueryRow(ctx, `
		UPDATE central_price_rollouts SET status = 'approved'
		WHERE tenant_id = $1 AND id = $2 AND status IN ('draft','pending_approval')
		RETURNING `+rolloutColumns, tenantID, id), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBadState
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ActivatePriceRollout writes a station-effective Phase-6 price_changes row for
// every station in the rollout's scope, then marks the rollout active.
func (r *Repo) ActivatePriceRollout(ctx context.Context, tx pgx.Tx, tenantID, id, setBy uuid.UUID) (*PriceRollout, error) {
	p, err := r.lockedRollout(ctx, tx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if p.Status != "approved" && p.Status != "scheduled" {
		return nil, ErrBadState
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO price_changes (tenant_id, station_id, product_id, unit_price, effective_from, reason, set_by,
		    previous_price)
		SELECT $1, s.id, $2, $3::numeric, $4, 'central rollout', $5,
		       (SELECT unit_price FROM price_changes pc WHERE pc.tenant_id = $1 AND pc.station_id = s.id AND pc.product_id = $2
		        ORDER BY pc.effective_from DESC LIMIT 1)
		FROM stations s
		WHERE s.tenant_id = $1
		  AND ( $6 = 'tenant'
		     OR ($6 = 'region'  AND s.region_id = $7)
		     OR ($6 = 'station' AND s.id = $7) )
	`, tenantID, p.ProductID, p.UnitPrice, p.EffectiveFrom, setBy, p.ScopeType, p.ScopeID)
	if err != nil {
		return nil, err
	}
	applied := int(tag.RowsAffected())
	var out PriceRollout
	if err := scanRollout(tx.QueryRow(ctx, `
		UPDATE central_price_rollouts SET status = 'active', stations_applied = $3
		WHERE tenant_id = $1 AND id = $2 RETURNING `+rolloutColumns,
		tenantID, id, applied), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *Repo) lockedRollout(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*PriceRollout, error) {
	var p PriceRollout
	err := scanRollout(tx.QueryRow(ctx, `SELECT `+rolloutColumns+` FROM central_price_rollouts WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, id), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ---- Central procurement (Stage 8) ----

func (r *Repo) CreatePlan(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, name string, createdBy uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO central_procurement_plans (tenant_id, name, created_by) VALUES ($1, $2, $3) RETURNING id`, tenantID, name, createdBy).Scan(&id)
	return id, err
}

func (r *Repo) AddPlanLine(ctx context.Context, tx pgx.Tx, tenantID, planID, stationID, productID uuid.UUID, targetLitres string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO central_procurement_plan_lines (tenant_id, plan_id, station_id, product_id, target_litres)
		VALUES ($1, $2, $3, $4, $5::numeric) RETURNING id
	`, tenantID, planID, stationID, productID, targetLitres).Scan(&id)
	return id, err
}

// ReleasePlan marks an approved plan released and its lines released — the
// station-scoped allocation hand-off to Phase-5 procurement. Returns the number
// of released lines.
func (r *Repo) ReleasePlan(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (int, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE central_procurement_plans SET status = 'released'
		WHERE tenant_id = $1 AND id = $2 AND status IN ('draft','reviewed','approved')
	`, tenantID, id)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, ErrBadState
	}
	ct, err := tx.Exec(ctx, `UPDATE central_procurement_plan_lines SET released = true WHERE tenant_id = $1 AND plan_id = $2`, tenantID, id)
	if err != nil {
		return 0, err
	}
	return int(ct.RowsAffected()), nil
}

func (r *Repo) ListPlans(ctx context.Context, tenantID uuid.UUID) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, status FROM central_procurement_plans WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, status string
		if err := rows.Scan(&id, &name, &status); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "name": name, "status": status})
	}
	return out, rows.Err()
}

// ---- Stock transfers (Stage 9) ----

type Transfer struct {
	ID         uuid.UUID
	FromTankID uuid.UUID
	ToTankID   uuid.UUID
	ProductID  uuid.UUID
	Litres     string
	Status     string
}

const transferColumns = `id, from_tank_id, to_tank_id, product_id, litres::text, status`

func scanTransfer(row pgx.Row, t *Transfer) error {
	return row.Scan(&t.ID, &t.FromTankID, &t.ToTankID, &t.ProductID, &t.Litres, &t.Status)
}

func (r *Repo) CreateTransfer(ctx context.Context, tx pgx.Tx, tenantID, fromTank, toTank, productID uuid.UUID, litres string, createdBy uuid.UUID) (*Transfer, error) {
	var t Transfer
	if err := scanTransfer(tx.QueryRow(ctx, `
		INSERT INTO stock_transfer_orders (tenant_id, from_tank_id, to_tank_id, product_id, litres, created_by)
		VALUES ($1, $2, $3, $4, $5::numeric, $6) RETURNING `+transferColumns,
		tenantID, fromTank, toTank, productID, litres, createdBy,
	), &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repo) ListTransfers(ctx context.Context, tenantID uuid.UUID) ([]Transfer, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+transferColumns+` FROM stock_transfer_orders WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Transfer{}
	for rows.Next() {
		var t Transfer
		if err := scanTransfer(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repo) ApproveTransfer(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*Transfer, error) {
	var t Transfer
	err := scanTransfer(tx.QueryRow(ctx, `
		UPDATE stock_transfer_orders SET status = 'approved'
		WHERE tenant_id = $1 AND id = $2 AND status = 'draft' RETURNING `+transferColumns, tenantID, id), &t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBadState
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ReceiveTransfer posts the paired Phase-4 'transfer' stock movements (out of
// the source tank, into the destination tank) and marks the order received.
// Guards against overdrawing the source tank.
func (r *Repo) ReceiveTransfer(ctx context.Context, tx pgx.Tx, tenantID, id, recordedBy uuid.UUID) (*Transfer, error) {
	t, err := r.lockedTransfer(ctx, tx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if t.Status != "approved" && t.Status != "dispatched" {
		return nil, ErrBadState
	}
	// ENT-25: both tanks must hold the transfer's product. Without this, a
	// transfer could move litres of one product out of a tank holding another,
	// corrupting both tanks' ledgers and their reconciliations.
	var fromProduct, toProduct uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT product_id FROM tanks WHERE tenant_id = $1 AND id = $2`, tenantID, t.FromTankID).Scan(&fromProduct); err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `SELECT product_id FROM tanks WHERE tenant_id = $1 AND id = $2`, tenantID, t.ToTankID).Scan(&toProduct); err != nil {
		return nil, err
	}
	if fromProduct != t.ProductID || toProduct != t.ProductID {
		return nil, ErrProductMismatch
	}
	// ENT-24/INV-003: serialize on both tanks before reading or posting.
	// Lock by hashed key in ascending order (canonical, so two opposing
	// transfers can't deadlock), matching inventory.PostMovement's per-tank key.
	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(k)
		FROM unnest(ARRAY[hashtextextended($1::text, 0), hashtextextended($2::text, 0)]) AS k
		ORDER BY k
	`, t.FromTankID, t.ToTankID); err != nil {
		return nil, err
	}
	// Available stock is the authoritative ledger SUM (every row, so a reversed
	// movement nets against its contra) — not a stale, status-filtered
	// balance_after snapshot that ignored contra entries (ENT-24).
	var sufficient bool
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE((SELECT SUM(litres) FROM stock_movements WHERE tenant_id = $1 AND tank_id = $2), 0) >= $3::numeric
	`, tenantID, t.FromTankID, t.Litres).Scan(&sufficient); err != nil {
		return nil, err
	}
	if !sufficient {
		return nil, ErrInsufficientStock
	}
	// Each leg derives balance_after from the live ledger SUM in the same
	// statement (as PostMovement does), so the running balance stays consistent.
	var outID, inID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO stock_movements (tenant_id, tank_id, movement_type, source_ref_type, source_ref_id, litres, balance_after, recorded_by)
		VALUES ($1, $2, 'transfer', 'transfer', $3, -$4::numeric,
		    (SELECT COALESCE(SUM(litres), 0) FROM stock_movements WHERE tenant_id = $1 AND tank_id = $2) - $4::numeric, $5)
		RETURNING id
	`, tenantID, t.FromTankID, id, t.Litres, recordedBy).Scan(&outID); err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO stock_movements (tenant_id, tank_id, movement_type, source_ref_type, source_ref_id, litres, balance_after, recorded_by)
		VALUES ($1, $2, 'transfer', 'transfer', $3, $4::numeric,
		    (SELECT COALESCE(SUM(litres), 0) FROM stock_movements WHERE tenant_id = $1 AND tank_id = $2) + $4::numeric, $5)
		RETURNING id
	`, tenantID, t.ToTankID, id, t.Litres, recordedBy).Scan(&inID); err != nil {
		return nil, err
	}
	var out Transfer
	if err := scanTransfer(tx.QueryRow(ctx, `
		UPDATE stock_transfer_orders SET status = 'received', out_movement_id = $3, in_movement_id = $4
		WHERE tenant_id = $1 AND id = $2 RETURNING `+transferColumns,
		tenantID, id, outID, inID), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *Repo) lockedTransfer(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*Transfer, error) {
	var t Transfer
	err := scanTransfer(tx.QueryRow(ctx, `SELECT `+transferColumns+` FROM stock_transfer_orders WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, id), &t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
