package revenue

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Sale-void lifecycle errors. They map to 4xx responses at the handler.
var (
	// ErrSaleNotFound is returned when a sale id doesn't resolve within the
	// tenant.
	ErrSaleNotFound = errors.New("revenue: sale not found")
	// ErrVoidNotFound is returned when a sale-void id doesn't resolve within the
	// tenant.
	ErrVoidNotFound = errors.New("revenue: sale void not found")
	// ErrVoidBadState is returned for a transition the void's current status
	// doesn't allow (e.g. approving one that isn't requested).
	ErrVoidBadState = errors.New("revenue: sale void is not in the required state")
	// ErrVoidSelfApprove is returned when the decider is the requester —
	// separation of duties forbids deciding your own void.
	ErrVoidSelfApprove = errors.New("revenue: approver cannot be the requester")
	// ErrVoidActiveExists is returned when a non-rejected void already exists for
	// the sale — a sale can carry at most one active void (no double-void).
	ErrVoidActiveExists = errors.New("revenue: sale already has an active void")
)

// SaleVoid is one row of the sale-void request->approve|reject lifecycle. On
// approve it becomes the reversal record: the reversal_* fields hold the sale's
// recognized amounts NEGATED (decimal strings), so revenue rollups net it
// against the original sale without mutating that append-only row.
type SaleVoid struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	SaleID         uuid.UUID
	Status         string
	Reason         string
	ReversalLitres *string // numeric(14,3) as text; nil until approved
	ReversalGross  *string
	ReversalTax    *string
	ReversalNet    *string
	ReversalCogs   *string
	ReversalMargin *string
	RequestedBy    uuid.UUID
	DecidedBy      *uuid.UUID
	DecisionNote   *string
	RequestedAt    time.Time
	DecidedAt      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

const voidColumns = `
    id, tenant_id, sale_id, status, reason,
    reversal_litres::text, reversal_gross::text, reversal_tax::text,
    reversal_net::text, reversal_cogs::text, reversal_margin::text,
    requested_by, decided_by, decision_note, requested_at, decided_at,
    created_at, updated_at
`

// voidColumnsSV is voidColumns qualified to the sale_voids alias "sv" — needed
// in the approve UPDATE...FROM sales, where unqualified "id" is ambiguous.
const voidColumnsSV = `
    sv.id, sv.tenant_id, sv.sale_id, sv.status, sv.reason,
    sv.reversal_litres::text, sv.reversal_gross::text, sv.reversal_tax::text,
    sv.reversal_net::text, sv.reversal_cogs::text, sv.reversal_margin::text,
    sv.requested_by, sv.decided_by, sv.decision_note, sv.requested_at, sv.decided_at,
    sv.created_at, sv.updated_at
`

func scanVoid(row pgx.Row, v *SaleVoid) error {
	return row.Scan(
		&v.ID, &v.TenantID, &v.SaleID, &v.Status, &v.Reason,
		&v.ReversalLitres, &v.ReversalGross, &v.ReversalTax,
		&v.ReversalNet, &v.ReversalCogs, &v.ReversalMargin,
		&v.RequestedBy, &v.DecidedBy, &v.DecisionNote, &v.RequestedAt, &v.DecidedAt,
		&v.CreatedAt, &v.UpdatedAt,
	)
}

// GetSale returns one recognized sale by id within the tenant, or
// ErrSaleNotFound. Used to authorize a void against the sale's station.
func (r *Repo) GetSale(ctx context.Context, tenantID, id uuid.UUID) (*Sale, error) {
	var s Sale
	err := scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM sales WHERE tenant_id = $1 AND id = $2`, tenantID, id), &s)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSaleNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetVoid returns one sale-void by id within the tenant, or ErrVoidNotFound.
func (r *Repo) GetVoid(ctx context.Context, tenantID, id uuid.UUID) (*SaleVoid, error) {
	var v SaleVoid
	err := scanVoid(r.pool.QueryRow(ctx, `SELECT `+voidColumns+` FROM sale_voids WHERE tenant_id = $1 AND id = $2`, tenantID, id), &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVoidNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// VoidForSale returns the current (non-rejected) void for a sale, or
// ErrVoidNotFound when the sale has none. There is at most one such row (the
// uq_sale_void_active partial unique index).
func (r *Repo) VoidForSale(ctx context.Context, tenantID, saleID uuid.UUID) (*SaleVoid, error) {
	var v SaleVoid
	err := scanVoid(r.pool.QueryRow(ctx, `
		SELECT `+voidColumns+` FROM sale_voids
		WHERE tenant_id = $1 AND sale_id = $2 AND status <> 'rejected'
	`, tenantID, saleID), &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVoidNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// RequestVoid records a new void in 'requested' state for the sale inside the
// caller's tx. The sale must exist (FK) and carry no active void (partial unique
// index -> ErrVoidActiveExists).
func (r *Repo) RequestVoid(ctx context.Context, tx pgx.Tx, tenantID, saleID, requestedBy uuid.UUID, reason string) (*SaleVoid, error) {
	var v SaleVoid
	err := scanVoid(tx.QueryRow(ctx, `
		INSERT INTO sale_voids (tenant_id, sale_id, reason, requested_by)
		VALUES ($1, $2, $3, $4)
		RETURNING `+voidColumns,
		tenantID, saleID, reason, requestedBy,
	), &v)
	if isUniqueViolation(err) {
		return nil, ErrVoidActiveExists
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505) — used to map the uq_sale_void_active collision to
// ErrVoidActiveExists without leaking pgconn into the handler.
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}

// ApproveVoid moves requested -> approved and records the reversal: it
// snapshots the sale's recognized amounts NEGATED onto the void row (the
// reversal record), so revenue rollups net the reversal against the original
// sale. The original sale is NEVER mutated. Separation of duties: the approver
// must not be the requester. The row is locked so the state + requester check
// and the transition are atomic; the negation is done in SQL (::numeric) so the
// money arithmetic is exact, never a Go float.
func (r *Repo) ApproveVoid(ctx context.Context, tx pgx.Tx, tenantID, id, approverID uuid.UUID, note *string) (*SaleVoid, error) {
	var status string
	var requestedBy uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT status, requested_by FROM sale_voids
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, id).Scan(&status, &requestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVoidNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "requested" {
		return nil, ErrVoidBadState
	}
	if requestedBy == approverID {
		return nil, ErrVoidSelfApprove
	}

	// Snapshot the sale's amounts NEGATED into the reversal_* columns. The
	// subtraction (0 - amount) runs in SQL over the sales row's own numeric, so
	// it is exact. COGS/margin may be NULL on the sale (no costed delivery); the
	// reversal mirrors that.
	var v SaleVoid
	err = scanVoid(tx.QueryRow(ctx, `
		UPDATE sale_voids sv
		SET status = 'approved', decided_by = $3, decision_note = $4, decided_at = now(),
		    reversal_litres = -s.litres,
		    reversal_gross  = -s.gross_amount,
		    reversal_tax    = -s.tax_amount,
		    reversal_net    = -s.net_amount,
		    reversal_cogs   = CASE WHEN s.cogs_amount   IS NULL THEN NULL ELSE -s.cogs_amount   END,
		    reversal_margin = CASE WHEN s.margin_amount IS NULL THEN NULL ELSE -s.margin_amount END
		FROM sales s
		WHERE sv.tenant_id = $1 AND sv.id = $2 AND sv.status = 'requested'
		  AND s.tenant_id = sv.tenant_id AND s.id = sv.sale_id
		RETURNING `+voidColumnsSV,
		tenantID, id, approverID, note,
	), &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVoidBadState
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// RejectVoid moves requested -> rejected. The requester may not reject (decide)
// their own void, mirroring approve's separation of duties.
func (r *Repo) RejectVoid(ctx context.Context, tx pgx.Tx, tenantID, id, deciderID uuid.UUID, note *string) (*SaleVoid, error) {
	var status string
	var requestedBy uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT status, requested_by FROM sale_voids
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, id).Scan(&status, &requestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVoidNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "requested" {
		return nil, ErrVoidBadState
	}
	if requestedBy == deciderID {
		return nil, ErrVoidSelfApprove
	}

	var v SaleVoid
	err = scanVoid(tx.QueryRow(ctx, `
		UPDATE sale_voids
		SET status = 'rejected', decided_by = $3, decision_note = $4, decided_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'requested'
		RETURNING `+voidColumns,
		tenantID, id, deciderID, note,
	), &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVoidBadState
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ListVoidsPage returns a page of the tenant's sale voids, optionally filtered
// by status, newest first (id breaks ties for stable paging).
func (r *Repo) ListVoidsPage(ctx context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]SaleVoid, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+voidColumns+` FROM sale_voids
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY requested_at DESC, id
		LIMIT $3 OFFSET $4
	`, tenantID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SaleVoid{}
	for rows.Next() {
		var v SaleVoid
		if err := scanVoid(rows, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// VoidStatuses returns the current (non-rejected) void status per sale id, for
// the supplied set of sale ids — used to surface "voided"/"void_requested" on
// sale list rows without an N+1. Sales with no active void are absent from the
// map.
func (r *Repo) VoidStatuses(ctx context.Context, q database.Querier, tenantID uuid.UUID, saleIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(saleIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT sale_id, status FROM sale_voids
		WHERE tenant_id = $1 AND sale_id = ANY($2) AND status <> 'rejected'
	`, tenantID, saleIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var st string
		if err := rows.Scan(&id, &st); err != nil {
			return nil, err
		}
		out[id] = st
	}
	return out, rows.Err()
}
