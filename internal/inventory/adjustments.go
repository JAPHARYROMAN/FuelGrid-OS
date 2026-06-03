package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Stock-adjustment lifecycle errors. They map to 4xx responses at the handler.
var (
	// ErrAdjustmentNotFound is returned when an adjustment id doesn't resolve
	// within the tenant.
	ErrAdjustmentNotFound = errors.New("inventory: stock adjustment not found")
	// ErrAdjustmentBadState is returned for a transition the adjustment's
	// current status doesn't allow (e.g. approving one that isn't requested).
	ErrAdjustmentBadState = errors.New("inventory: stock adjustment is not in the required state")
	// ErrAdjustmentSelfApprove is returned when the approver is the requester —
	// separation of duties forbids deciding your own adjustment.
	ErrAdjustmentSelfApprove = errors.New("inventory: approver cannot be the requester")
)

// Adjustment classifications — the coarse machine category of why book stock is
// being corrected. Mirrors chk_stock_adj_classification (migration 0087).
var adjustmentClassifications = map[string]bool{
	"evaporation": true, "measurement_error": true, "theft": true,
	"spillage": true, "temperature": true, "data_entry": true, "other": true,
}

// ValidClassification reports whether c is an accepted adjustment
// classification.
func ValidClassification(c string) bool { return adjustmentClassifications[c] }

// Adjustment is one row of the stock-adjustment request->approve->post
// lifecycle. delta_litres is a signed decimal STRING (+in / -out); the balance
// snapshots are recorded only once the adjustment posts.
type Adjustment struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	TankID         uuid.UUID
	DeltaLitres    string // signed decimal (numeric(14,3) as text)
	Reason         string
	Classification string
	Status         string
	BalanceBefore  *string
	BalanceAfter   *string
	MovementID     *uuid.UUID
	RequestedBy    uuid.UUID
	ApprovedBy     *uuid.UUID
	PostedBy       *uuid.UUID
	RejectedBy     *uuid.UUID
	DecisionNote   *string
	RequestedAt    time.Time
	DecidedAt      *time.Time
	PostedAt       *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// AdjustmentInput is the data needed to request a stock adjustment.
type AdjustmentInput struct {
	TankID uuid.UUID
	// DeltaLitres is a signed exact-decimal STRING bound into the numeric
	// column via $N::numeric; never a Go float. The caller validates it is a
	// well-formed, non-zero decimal.
	DeltaLitres    string
	Reason         string
	Classification string
	RequestedBy    uuid.UUID
}

const adjustmentColumns = `
    id, tenant_id, tank_id, delta_litres::text, reason, classification, status,
    balance_before::text, balance_after::text, movement_id,
    requested_by, approved_by, posted_by, rejected_by, decision_note,
    requested_at, decided_at, posted_at, created_at, updated_at
`

func scanAdjustment(row pgx.Row, a *Adjustment) error {
	return row.Scan(
		&a.ID, &a.TenantID, &a.TankID, &a.DeltaLitres, &a.Reason, &a.Classification, &a.Status,
		&a.BalanceBefore, &a.BalanceAfter, &a.MovementID,
		&a.RequestedBy, &a.ApprovedBy, &a.PostedBy, &a.RejectedBy, &a.DecisionNote,
		&a.RequestedAt, &a.DecidedAt, &a.PostedAt, &a.CreatedAt, &a.UpdatedAt,
	)
}

// RequestAdjustment records a new adjustment in 'requested' state inside the
// caller's tx.
func (r *Repo) RequestAdjustment(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in AdjustmentInput) (*Adjustment, error) {
	var a Adjustment
	if err := scanAdjustment(tx.QueryRow(ctx, `
		INSERT INTO stock_adjustments
		    (tenant_id, tank_id, delta_litres, reason, classification, requested_by)
		VALUES ($1, $2, $3::numeric, $4, $5, $6)
		RETURNING `+adjustmentColumns,
		tenantID, in.TankID, in.DeltaLitres, in.Reason, in.Classification, in.RequestedBy,
	), &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// GetAdjustment returns one adjustment by id within the tenant, or
// ErrAdjustmentNotFound.
func (r *Repo) GetAdjustment(ctx context.Context, tenantID, id uuid.UUID) (*Adjustment, error) {
	var a Adjustment
	err := scanAdjustment(r.pool.QueryRow(ctx, `
		SELECT `+adjustmentColumns+` FROM stock_adjustments WHERE tenant_id = $1 AND id = $2
	`, tenantID, id), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAdjustmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListAdjustmentsPage returns a page of the tenant's stock adjustments,
// optionally filtered by status and/or tank, newest first (id breaks ties for
// stable paging).
func (r *Repo) ListAdjustmentsPage(ctx context.Context, tenantID uuid.UUID, status string, tankID *uuid.UUID, limit, offset int) ([]Adjustment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+adjustmentColumns+` FROM stock_adjustments
		WHERE tenant_id = $1
		  AND ($2 = '' OR status = $2)
		  AND ($3::uuid IS NULL OR tank_id = $3)
		ORDER BY requested_at DESC, id
		LIMIT $4 OFFSET $5
	`, tenantID, status, tankID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Adjustment{}
	for rows.Next() {
		var a Adjustment
		if err := scanAdjustment(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ApproveAdjustment moves requested -> approved, recording the approver.
// Separation of duties: the approver must not be the requester. The row is
// locked so the state + requester check and the transition are atomic.
func (r *Repo) ApproveAdjustment(ctx context.Context, tx pgx.Tx, tenantID, id, approverID uuid.UUID, note *string) (*Adjustment, error) {
	var status string
	var requestedBy uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT status, requested_by FROM stock_adjustments
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, id).Scan(&status, &requestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAdjustmentNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "requested" {
		return nil, ErrAdjustmentBadState
	}
	if requestedBy == approverID {
		return nil, ErrAdjustmentSelfApprove
	}

	var a Adjustment
	err = scanAdjustment(tx.QueryRow(ctx, `
		UPDATE stock_adjustments
		SET status = 'approved', approved_by = $3, decision_note = $4, decided_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'requested'
		RETURNING `+adjustmentColumns,
		tenantID, id, approverID, note,
	), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAdjustmentBadState
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// RejectAdjustment moves requested|approved -> rejected. The requester may not
// reject (decide) their own adjustment, mirroring approve's separation of
// duties.
func (r *Repo) RejectAdjustment(ctx context.Context, tx pgx.Tx, tenantID, id, deciderID uuid.UUID, note *string) (*Adjustment, error) {
	var status string
	var requestedBy uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT status, requested_by FROM stock_adjustments
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, id).Scan(&status, &requestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAdjustmentNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "requested" && status != "approved" {
		return nil, ErrAdjustmentBadState
	}
	if requestedBy == deciderID {
		return nil, ErrAdjustmentSelfApprove
	}

	var a Adjustment
	err = scanAdjustment(tx.QueryRow(ctx, `
		UPDATE stock_adjustments
		SET status = 'rejected', rejected_by = $3, decision_note = $4, decided_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status IN ('requested', 'approved')
		RETURNING `+adjustmentColumns,
		tenantID, id, deciderID, note,
	), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAdjustmentBadState
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// PostAdjustment posts an approved adjustment: it appends a single 'adjustment'
// movement to the tank's ledger (delta litres signed) and flips the adjustment
// to 'posted', linking the movement and snapshotting the before/after book
// stock. Posting is idempotent and immutable — only an 'approved' adjustment
// posts; once 'posted' it can never re-post (status guard + the
// uq_stock_adj_movement unique index). The whole thing runs in the caller's tx,
// so the ledger row and the lifecycle flip commit together or not at all.
//
// balance_before is read under the same per-tank advisory lock PostMovement
// takes (PostMovement acquires it first), so it reflects the ledger sum
// immediately before this movement; balance_after is the posted row's snapshot.
func (r *Repo) PostAdjustment(ctx context.Context, tx pgx.Tx, tenantID, id, posterID uuid.UUID) (*Adjustment, *Movement, error) {
	// Lock the lifecycle row first so two concurrent posters serialize on it.
	var status string
	var tankID uuid.UUID
	var delta string
	err := tx.QueryRow(ctx, `
		SELECT status, tank_id, delta_litres::text FROM stock_adjustments
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, id).Scan(&status, &tankID, &delta)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrAdjustmentNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if status != "approved" {
		return nil, nil, ErrAdjustmentBadState
	}

	// Capture the book balance immediately before the movement posts. This and
	// the INSERT inside PostMovement both run after PostMovement's per-tank
	// advisory lock, so no concurrent post can interleave between this read and
	// the append.
	srcType := "adjustment"
	m, err := r.PostMovement(ctx, tx, tenantID, PostInput{
		TankID:        tankID,
		MovementType:  TypeAdjustment,
		SourceRefType: &srcType,
		SourceRefID:   &id,
		Litres:        delta,
		RecordedBy:    posterID,
	})
	if err != nil {
		return nil, nil, err
	}

	// balance_before = balance_after - delta (both are the ledger's own
	// numeric, so this subtraction is exact in SQL, never a Go float).
	var a Adjustment
	err = scanAdjustment(tx.QueryRow(ctx, `
		UPDATE stock_adjustments
		SET status = 'posted', posted_by = $3, movement_id = $4, posted_at = now(),
		    balance_after  = $5::numeric,
		    balance_before = $5::numeric - delta_litres
		WHERE tenant_id = $1 AND id = $2 AND status = 'approved'
		RETURNING `+adjustmentColumns,
		tenantID, id, posterID, m.ID, m.BalanceAfter,
	), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrAdjustmentBadState
	}
	if err != nil {
		return nil, nil, err
	}
	return &a, m, nil
}
