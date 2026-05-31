package operations

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// ShiftException is an auto-raised mechanical anomaly on a shift. An open
// exception blocks approval until resolved.
type ShiftException struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	ShiftID    uuid.UUID
	Type       string
	Severity   string
	Detail     *string
	Status     string
	RaisedAt   time.Time
	ResolvedBy *uuid.UUID
	ResolvedAt *time.Time
}

var ErrExceptionNotFound = errors.New("operations: shift exception not found")

const exceptionColumns = `
    id, tenant_id, shift_id, type, severity, detail, status,
    raised_at, resolved_by, resolved_at
`

func scanException(row pgx.Row, e *ShiftException) error {
	return row.Scan(
		&e.ID, &e.TenantID, &e.ShiftID, &e.Type, &e.Severity, &e.Detail, &e.Status,
		&e.RaisedAt, &e.ResolvedBy, &e.ResolvedAt,
	)
}

// RaiseException records a new open exception inside the caller's tx.
func (r *Repo) RaiseException(ctx context.Context, tx pgx.Tx, tenantID, shiftID uuid.UUID, typ, severity, detail string) (*ShiftException, error) {
	var e ShiftException
	if err := scanException(tx.QueryRow(ctx, `
		INSERT INTO shift_exceptions (tenant_id, shift_id, type, severity, detail)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+exceptionColumns,
		tenantID, shiftID, typ, severity, detail,
	), &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *Repo) ListExceptionsForShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]ShiftException, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+exceptionColumns+`
		FROM shift_exceptions WHERE tenant_id = $1 AND shift_id = $2
		ORDER BY raised_at DESC
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShiftException
	for rows.Next() {
		var e ShiftException
		if err := scanException(rows, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListExceptionsForShiftPage returns a page of a shift's exceptions, newest
// raised first with id as a deterministic tiebreaker so paging is stable.
func (r *Repo) ListExceptionsForShiftPage(ctx context.Context, tenantID, shiftID uuid.UUID, limit, offset int) ([]ShiftException, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+exceptionColumns+`
		FROM shift_exceptions WHERE tenant_id = $1 AND shift_id = $2
		ORDER BY raised_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`, tenantID, shiftID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShiftException
	for rows.Next() {
		var e ShiftException
		if err := scanException(rows, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repo) GetException(ctx context.Context, tenantID, id uuid.UUID) (*ShiftException, error) {
	var e ShiftException
	if err := scanException(r.pool.QueryRow(ctx, `
		SELECT `+exceptionColumns+` FROM shift_exceptions WHERE id = $1 AND tenant_id = $2
	`, id, tenantID), &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// ResolveException marks an open exception resolved inside the caller's tx.
func (r *Repo) ResolveException(ctx context.Context, tx pgx.Tx, tenantID, id, actorID uuid.UUID) (*ShiftException, error) {
	var e ShiftException
	err := scanException(tx.QueryRow(ctx, `
		UPDATE shift_exceptions
		SET status = 'resolved', resolved_by = $3, resolved_at = now()
		WHERE id = $1 AND tenant_id = $2 AND status = 'open'
		RETURNING `+exceptionColumns,
		id, tenantID, actorID,
	), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrExceptionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// OpenExceptionCountForShift counts unresolved exceptions on a shift — the
// guard that blocks approval. It runs through any Querier so approval can
// re-count inside the same tx that holds FOR UPDATE on the shift row, closing
// the TOCTOU where an exception is raised between the count and the approve.
func (r *Repo) OpenExceptionCountForShift(ctx context.Context, q database.Querier, tenantID, shiftID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM shift_exceptions
		WHERE tenant_id = $1 AND shift_id = $2 AND status = 'open'
	`, tenantID, shiftID).Scan(&n)
	return n, err
}
