package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Opening-stock request lifecycle errors. They map to 4xx responses at the
// handler. The shape mirrors the stock-adjustment lifecycle (adjustments.go).
var (
	// ErrOpeningRequestNotFound is returned when an opening-stock request id
	// doesn't resolve within the tenant.
	ErrOpeningRequestNotFound = errors.New("inventory: opening stock request not found")
	// ErrOpeningRequestBadState is returned for a transition the request's
	// current status doesn't allow (e.g. approving one that isn't a draft).
	ErrOpeningRequestBadState = errors.New("inventory: opening stock request is not in the required state")
	// ErrOpeningRequestSelfApprove is returned when the approver is the
	// requester — separation of duties forbids deciding your own opening stock.
	ErrOpeningRequestSelfApprove = errors.New("inventory: approver cannot be the requester")
	// ErrOpeningRequestExists is returned when a tank already has a live (draft
	// or approved/locked) opening-stock request.
	ErrOpeningRequestExists = errors.New("inventory: a live opening stock request already exists for this tank")
)

// OpeningRequest is one row of the opening-stock draft -> approve(lock) / reject
// lifecycle. litres is a non-negative decimal STRING; the movement link and
// balance snapshot are recorded only once the request is approved (locked).
type OpeningRequest struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	TankID       uuid.UUID
	Litres       string // non-negative decimal (numeric(14,3) as text)
	Notes        *string
	Status       string
	MovementID   *uuid.UUID
	BalanceAfter *string
	RequestedBy  uuid.UUID
	ApprovedBy   *uuid.UUID
	RejectedBy   *uuid.UUID
	DecisionNote *string
	RequestedAt  time.Time
	DecidedAt    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// OpeningRequestInput is the data needed to enter a draft opening-stock request.
type OpeningRequestInput struct {
	TankID uuid.UUID
	// Litres is a non-negative exact-decimal STRING bound into the numeric
	// column via $N::numeric; never a Go float. The caller validates it is a
	// well-formed, non-negative decimal.
	Litres      string
	Notes       *string
	RequestedBy uuid.UUID
}

const openingRequestColumns = `
    id, tenant_id, tank_id, litres::text, notes, status,
    movement_id, balance_after::text,
    requested_by, approved_by, rejected_by, decision_note,
    requested_at, decided_at, created_at, updated_at
`

func scanOpeningRequest(row pgx.Row, o *OpeningRequest) error {
	return row.Scan(
		&o.ID, &o.TenantID, &o.TankID, &o.Litres, &o.Notes, &o.Status,
		&o.MovementID, &o.BalanceAfter,
		&o.RequestedBy, &o.ApprovedBy, &o.RejectedBy, &o.DecisionNote,
		&o.RequestedAt, &o.DecidedAt, &o.CreatedAt, &o.UpdatedAt,
	)
}

// RequestOpeningStock records a new opening-stock request in 'draft' state
// inside the caller's tx. A tank that already has a live (draft or
// approved/locked) request yields ErrOpeningRequestExists, and a tank whose
// ledger has already been opened directly also yields ErrOpeningRequestExists —
// a tank gets exactly one opening, however it was established.
//
// The fast-path checks below short-circuit the common case but are racy on
// their own; the partial unique index uq_osr_one_live_per_tank is the
// authoritative guard, translated back into ErrOpeningRequestExists.
func (r *Repo) RequestOpeningStock(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in OpeningRequestInput) (*OpeningRequest, error) {
	// A tank already opened via the ledger has nothing left to request.
	opened, err := r.hasOpeningTx(ctx, tx, tenantID, in.TankID)
	if err != nil {
		return nil, err
	}
	if opened {
		return nil, ErrOpeningRequestExists
	}
	var o OpeningRequest
	err = scanOpeningRequest(tx.QueryRow(ctx, `
		INSERT INTO opening_stock_requests
		    (tenant_id, tank_id, litres, notes, requested_by)
		VALUES ($1, $2, $3::numeric, $4, $5)
		RETURNING `+openingRequestColumns,
		tenantID, in.TankID, in.Litres, in.Notes, in.RequestedBy,
	), &o)
	if isUniqueViolation(err) {
		return nil, ErrOpeningRequestExists
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// GetOpeningRequest returns one request by id within the tenant, or
// ErrOpeningRequestNotFound.
func (r *Repo) GetOpeningRequest(ctx context.Context, tenantID, id uuid.UUID) (*OpeningRequest, error) {
	var o OpeningRequest
	err := scanOpeningRequest(r.pool.QueryRow(ctx, `
		SELECT `+openingRequestColumns+` FROM opening_stock_requests WHERE tenant_id = $1 AND id = $2
	`, tenantID, id), &o)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOpeningRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// ListOpeningRequestsPage returns a page of the tenant's opening-stock requests,
// optionally filtered by status and/or tank, newest first (id breaks ties for
// stable paging).
func (r *Repo) ListOpeningRequestsPage(ctx context.Context, tenantID uuid.UUID, status string, tankID *uuid.UUID, limit, offset int) ([]OpeningRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+openingRequestColumns+` FROM opening_stock_requests
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
	out := []OpeningRequest{}
	for rows.Next() {
		var o OpeningRequest
		if err := scanOpeningRequest(rows, &o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ApproveOpeningStock posts a draft request to the tank's ledger and LOCKS it:
// it seeds the genesis 'opening' movement (via the same SetOpeningBalance path
// as the direct seed) and flips the request to 'approved', linking the movement
// and snapshotting the balance. Separation of duties: the approver must not be
// the requester (checked under a row lock). Approval is idempotent and
// immutable — only a 'draft' request approves; once 'approved' it can never
// re-approve (status guard + the uq_osr_movement unique index). Runs in the
// caller's tx so the ledger row and the lock commit together or not at all.
func (r *Repo) ApproveOpeningStock(ctx context.Context, tx pgx.Tx, tenantID, id, approverID uuid.UUID, note *string) (*OpeningRequest, *Movement, error) {
	// Lock the lifecycle row first so two concurrent approvers serialize on it,
	// and so the state + requester (separation-of-duties) check is atomic with
	// the transition.
	var status string
	var tankID, requestedBy uuid.UUID
	var litres string
	err := tx.QueryRow(ctx, `
		SELECT status, tank_id, requested_by, litres::text FROM opening_stock_requests
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, id).Scan(&status, &tankID, &requestedBy, &litres)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrOpeningRequestNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if status != "draft" {
		return nil, nil, ErrOpeningRequestBadState
	}
	if requestedBy == approverID {
		return nil, nil, ErrOpeningRequestSelfApprove
	}

	// Post the genesis opening movement (litres is an exact-decimal string,
	// bound into the numeric ledger column downstream — never a Go float). A
	// tank already opened by another path yields ErrOpeningExists.
	srcType := "opening"
	m, err := r.SetOpeningBalance(ctx, tx, tenantID, OpeningInput{
		TankID: tankID, Litres: litres, SourceRefType: &srcType,
		RecordedBy: approverID, Notes: note,
	})
	if errors.Is(err, ErrOpeningExists) {
		return nil, nil, ErrOpeningRequestExists
	}
	if err != nil {
		return nil, nil, err
	}

	var o OpeningRequest
	err = scanOpeningRequest(tx.QueryRow(ctx, `
		UPDATE opening_stock_requests
		SET status = 'approved', approved_by = $3, decision_note = $4,
		    movement_id = $5, balance_after = $6::numeric, decided_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'draft'
		RETURNING `+openingRequestColumns,
		tenantID, id, approverID, note, m.ID, m.BalanceAfter,
	), &o)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrOpeningRequestBadState
	}
	if err != nil {
		return nil, nil, err
	}
	return &o, m, nil
}

// RejectOpeningStock moves a draft request -> rejected, recording a reason. The
// requester may not reject (decide) their own request, mirroring approve's
// separation of duties. A rejected request frees the tank for a corrected
// figure (the live-per-tank unique index exempts rejected rows).
func (r *Repo) RejectOpeningStock(ctx context.Context, tx pgx.Tx, tenantID, id, deciderID uuid.UUID, note *string) (*OpeningRequest, error) {
	var status string
	var requestedBy uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT status, requested_by FROM opening_stock_requests
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, id).Scan(&status, &requestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOpeningRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "draft" {
		return nil, ErrOpeningRequestBadState
	}
	if requestedBy == deciderID {
		return nil, ErrOpeningRequestSelfApprove
	}

	var o OpeningRequest
	err = scanOpeningRequest(tx.QueryRow(ctx, `
		UPDATE opening_stock_requests
		SET status = 'rejected', rejected_by = $3, decision_note = $4, decided_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'draft'
		RETURNING `+openingRequestColumns,
		tenantID, id, deciderID, note,
	), &o)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOpeningRequestBadState
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}
