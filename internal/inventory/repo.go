// Package inventory is the data layer for the per-tank stock ledger — the
// append-only record of every litre moved in or out of a tank (Phase 4,
// Stage 1).
//
// Book stock is derived, not stored: CurrentBalance sums the ledger. A row's
// balance_after is only a snapshot of the running balance at post time, kept
// for display and audit; the sum is authoritative.
//
// Corrections never rewrite a posted movement. ReverseMovement marks the
// original 'reversed' (a status annotation — its litres are untouched) and
// posts a contra entry with negated litres and supersedes_id pointing at the
// original. The two net to zero, so the balance sum spans every row
// regardless of status.
package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Movement types — the litre-flow category of a ledger row.
const (
	TypeOpening    = "opening"
	TypeDelivery   = "delivery"
	TypeSales      = "sales"
	TypeAdjustment = "adjustment"
	TypeTransfer   = "transfer"
)

// Movement statuses.
const (
	StatusPosted   = "posted"
	StatusReversed = "reversed"
)

var (
	// ErrMovementNotFound is returned when a movement id doesn't resolve
	// within the tenant.
	ErrMovementNotFound = errors.New("inventory: movement not found")
	// ErrAlreadyReversed is returned when reversing a movement that has
	// already been reversed.
	ErrAlreadyReversed = errors.New("inventory: movement already reversed")
	// ErrNoOpeningBalance is returned when a flow movement (delivery/sales/
	// transfer) is posted to a tank that has no opening balance yet.
	ErrNoOpeningBalance = errors.New("inventory: tank has no opening balance")
	// ErrOpeningExists is returned when setting an opening balance on a tank
	// that already has one.
	ErrOpeningExists = errors.New("inventory: opening balance already set")
)

// requiresOpening reports whether a movement type may only post after the
// tank has an opening balance. Opening and adjustment movements are exempt —
// the opening establishes the ledger, and adjustments (incl. reversal
// contras) can correct it.
func requiresOpening(movementType string) bool {
	switch movementType {
	case TypeDelivery, TypeSales, TypeTransfer:
		return true
	default:
		return false
	}
}

// Movement is one row of a tank's stock ledger.
type Movement struct {
	ID                 uuid.UUID
	Seq                int64 // monotonic insertion order; the ledger's append sequence
	TenantID           uuid.UUID
	TankID             uuid.UUID
	MovementType       string
	SourceRefType      *string
	SourceRefID        *uuid.UUID
	Litres             float64 // signed: +in / -out
	BalanceAfter       float64
	SupplierID         *uuid.UUID
	PurchaseOrderID    *uuid.UUID
	LandedCostTotal    *string
	LandedCostPerLitre *string
	RecordedBy         uuid.UUID
	RecordedAt         time.Time
	SupersedesID       *uuid.UUID
	Status             string
	Notes              *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// PostInput is the data needed to append one movement to a tank's ledger.
type PostInput struct {
	TankID             uuid.UUID
	MovementType       string
	SourceRefType      *string
	SourceRefID        *uuid.UUID
	Litres             float64 // signed: +in / -out
	SupplierID         *uuid.UUID
	PurchaseOrderID    *uuid.UUID
	LandedCostTotal    *string
	LandedCostPerLitre *string
	SupersedesID       *uuid.UUID
	RecordedBy         uuid.UUID
	Notes              *string
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `
    id, seq, tenant_id, tank_id, movement_type, source_ref_type, source_ref_id,
    litres, balance_after, supplier_id, purchase_order_id,
    landed_cost_total::text, landed_cost_per_litre::text,
    recorded_by, recorded_at, supersedes_id, status, notes, created_at, updated_at
`

func scan(row pgx.Row, m *Movement) error {
	return row.Scan(
		&m.ID, &m.Seq, &m.TenantID, &m.TankID, &m.MovementType, &m.SourceRefType, &m.SourceRefID,
		&m.Litres, &m.BalanceAfter, &m.SupplierID, &m.PurchaseOrderID,
		&m.LandedCostTotal, &m.LandedCostPerLitre,
		&m.RecordedBy, &m.RecordedAt, &m.SupersedesID, &m.Status, &m.Notes, &m.CreatedAt, &m.UpdatedAt,
	)
}

// PostMovement appends a movement to a tank's ledger inside the caller's tx.
// balance_after is computed as the tank's current ledger sum plus this
// movement's litres, in the same statement, so it stays consistent with
// CurrentBalance even when several movements post in one transaction.
func (r *Repo) PostMovement(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in PostInput) (*Movement, error) {
	// Serialize all posts to this tank within the tx (INV-003). balance_after
	// is derived from SUM(litres) in the INSERT below; without a lock two
	// concurrent transactions read the same sum under READ COMMITTED and write
	// inconsistent running balances. A transaction-scoped advisory lock keyed
	// on the tank (uuids are unique, so the tank alone is a sufficient key)
	// serializes per-tank posting and releases at commit/rollback.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, in.TankID); err != nil {
		return nil, err
	}
	if requiresOpening(in.MovementType) {
		has, err := r.hasOpeningTx(ctx, tx, tenantID, in.TankID)
		if err != nil {
			return nil, err
		}
		if !has {
			return nil, ErrNoOpeningBalance
		}
	}
	var m Movement
	if err := scan(tx.QueryRow(ctx, `
		INSERT INTO stock_movements
		    (tenant_id, tank_id, movement_type, source_ref_type, source_ref_id,
		     litres, balance_after, supplier_id, purchase_order_id,
		     landed_cost_total, landed_cost_per_litre,
		     recorded_by, supersedes_id, notes)
		VALUES ($1, $2, $3, $4, $5, $6,
		    (SELECT COALESCE(SUM(litres), 0) FROM stock_movements
		        WHERE tenant_id = $1 AND tank_id = $2) + $6,
		    $7, $8, $9::numeric, $10::numeric, $11, $12, $13)
		RETURNING `+columns,
		tenantID, in.TankID, in.MovementType, in.SourceRefType, in.SourceRefID,
		in.Litres, in.SupplierID, in.PurchaseOrderID, in.LandedCostTotal, in.LandedCostPerLitre,
		in.RecordedBy, in.SupersedesID, in.Notes,
	), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// CurrentBalance returns the tank's book stock — the authoritative sum of
// every movement on its ledger.
func (r *Repo) CurrentBalance(ctx context.Context, tenantID, tankID uuid.UUID) (float64, error) {
	var bal float64
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(litres), 0)
		FROM stock_movements
		WHERE tenant_id = $1 AND tank_id = $2
	`, tenantID, tankID).Scan(&bal)
	return bal, err
}

// ListMovements returns a tank's ledger in time order (oldest first), so the
// running balance reads top to bottom.
func (r *Repo) ListMovements(ctx context.Context, tenantID, tankID uuid.UUID) ([]Movement, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM stock_movements
		WHERE tenant_id = $1 AND tank_id = $2
		ORDER BY seq
	`, tenantID, tankID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Movement
	for rows.Next() {
		var m Movement
		if err := scan(rows, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetMovement returns one movement by id within the tenant, or
// ErrMovementNotFound.
func (r *Repo) GetMovement(ctx context.Context, tenantID, id uuid.UUID) (*Movement, error) {
	var m Movement
	err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+` FROM stock_movements WHERE id = $1 AND tenant_id = $2
	`, id, tenantID), &m)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMovementNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ReverseMovement reverses a posted movement inside the caller's tx: it marks
// the original 'reversed' and appends a contra entry (negated litres,
// supersedes_id -> original) that nets the original to zero. It returns the
// contra entry. A movement already reversed yields ErrAlreadyReversed.
func (r *Repo) ReverseMovement(ctx context.Context, tx pgx.Tx, tenantID, movementID, recordedBy uuid.UUID, notes *string) (*Movement, error) {
	var orig Movement
	err := scan(tx.QueryRow(ctx, `
		UPDATE stock_movements SET status = 'reversed'
		WHERE id = $1 AND tenant_id = $2 AND status = 'posted'
		RETURNING `+columns,
		movementID, tenantID,
	), &orig)
	if errors.Is(err, pgx.ErrNoRows) {
		// Either the movement doesn't exist or it's already reversed —
		// disambiguate so the caller can return the right status.
		if _, gErr := r.GetMovement(ctx, tenantID, movementID); errors.Is(gErr, ErrMovementNotFound) {
			return nil, ErrMovementNotFound
		}
		return nil, ErrAlreadyReversed
	}
	if err != nil {
		return nil, err
	}

	srcType := "correction"
	return r.PostMovement(ctx, tx, tenantID, PostInput{
		TankID:        orig.TankID,
		MovementType:  orig.MovementType,
		SourceRefType: &srcType,
		SourceRefID:   &orig.ID,
		Litres:        -orig.Litres,
		SupersedesID:  &orig.ID,
		RecordedBy:    recordedBy,
		Notes:         notes,
	})
}

// OpeningInput is the data needed to seed a tank's opening balance.
type OpeningInput struct {
	TankID uuid.UUID
	Litres float64
	// SourceRefType records what produced the opening: "opening" for a
	// manual/first-dip seed, "reconciliation" when a sealed day's physical
	// figure carries forward (Stage 6). Defaults to "opening" when nil.
	SourceRefType *string
	RecordedBy    uuid.UUID
	Notes         *string
}

// hasOpeningPredicate matches a tank's genuine opening movement — a posted
// 'opening' that is not itself a reversal contra.
const hasOpeningPredicate = `
	tenant_id = $1 AND tank_id = $2 AND movement_type = 'opening' AND status = 'posted'
	AND (source_ref_type IS NULL OR source_ref_type <> 'correction')
`

func (r *Repo) hasOpeningTx(ctx context.Context, tx pgx.Tx, tenantID, tankID uuid.UUID) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stock_movements WHERE`+hasOpeningPredicate+`)`,
		tenantID, tankID).Scan(&exists)
	return exists, err
}

// HasOpeningBalance reports whether a tank's ledger has been opened.
func (r *Repo) HasOpeningBalance(ctx context.Context, tenantID, tankID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stock_movements WHERE`+hasOpeningPredicate+`)`,
		tenantID, tankID).Scan(&exists)
	return exists, err
}

// OpeningBalance returns the tank's current opening movement, or
// ErrNoOpeningBalance.
func (r *Repo) OpeningBalance(ctx context.Context, tenantID, tankID uuid.UUID) (*Movement, error) {
	var m Movement
	err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+` FROM stock_movements WHERE`+hasOpeningPredicate+`
		ORDER BY seq DESC LIMIT 1
	`, tenantID, tankID), &m)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoOpeningBalance
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// SetOpeningBalance seeds a tank's opening balance inside the caller's tx,
// posting the genesis 'opening' movement. A tank that already has an opening
// yields ErrOpeningExists.
func (r *Repo) SetOpeningBalance(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in OpeningInput) (*Movement, error) {
	has, err := r.hasOpeningTx(ctx, tx, tenantID, in.TankID)
	if err != nil {
		return nil, err
	}
	if has {
		return nil, ErrOpeningExists
	}
	srcType := in.SourceRefType
	if srcType == nil {
		s := "opening"
		srcType = &s
	}
	return r.PostMovement(ctx, tx, tenantID, PostInput{
		TankID:        in.TankID,
		MovementType:  TypeOpening,
		SourceRefType: srcType,
		Litres:        in.Litres,
		RecordedBy:    in.RecordedBy,
		Notes:         in.Notes,
	})
}
