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
)

// Movement is one row of a tank's stock ledger.
type Movement struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	TankID        uuid.UUID
	MovementType  string
	SourceRefType *string
	SourceRefID   *uuid.UUID
	Litres        float64 // signed: +in / -out
	BalanceAfter  float64
	RecordedBy    uuid.UUID
	RecordedAt    time.Time
	SupersedesID  *uuid.UUID
	Status        string
	Notes         *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PostInput is the data needed to append one movement to a tank's ledger.
type PostInput struct {
	TankID        uuid.UUID
	MovementType  string
	SourceRefType *string
	SourceRefID   *uuid.UUID
	Litres        float64 // signed: +in / -out
	SupersedesID  *uuid.UUID
	RecordedBy    uuid.UUID
	Notes         *string
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

const columns = `
    id, tenant_id, tank_id, movement_type, source_ref_type, source_ref_id,
    litres, balance_after, recorded_by, recorded_at, supersedes_id, status,
    notes, created_at, updated_at
`

func scan(row pgx.Row, m *Movement) error {
	return row.Scan(
		&m.ID, &m.TenantID, &m.TankID, &m.MovementType, &m.SourceRefType, &m.SourceRefID,
		&m.Litres, &m.BalanceAfter, &m.RecordedBy, &m.RecordedAt, &m.SupersedesID, &m.Status,
		&m.Notes, &m.CreatedAt, &m.UpdatedAt,
	)
}

// PostMovement appends a movement to a tank's ledger inside the caller's tx.
// balance_after is computed as the tank's current ledger sum plus this
// movement's litres, in the same statement, so it stays consistent with
// CurrentBalance even when several movements post in one transaction.
func (r *Repo) PostMovement(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in PostInput) (*Movement, error) {
	var m Movement
	if err := scan(tx.QueryRow(ctx, `
		INSERT INTO stock_movements
		    (tenant_id, tank_id, movement_type, source_ref_type, source_ref_id,
		     litres, balance_after, recorded_by, supersedes_id, notes)
		VALUES ($1, $2, $3, $4, $5, $6,
		    (SELECT COALESCE(SUM(litres), 0) FROM stock_movements
		        WHERE tenant_id = $1 AND tank_id = $2) + $6,
		    $7, $8, $9)
		RETURNING `+columns,
		tenantID, in.TankID, in.MovementType, in.SourceRefType, in.SourceRefID,
		in.Litres, in.RecordedBy, in.SupersedesID, in.Notes,
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
