package accounting

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

var (
	ErrPeriodNotFound   = errors.New("accounting: period not found")
	ErrPeriodOverlap    = errors.New("accounting: period overlaps an existing period")
	ErrPeriodTransition = errors.New("accounting: invalid period transition")
	ErrNoPeriod         = errors.New("accounting: no accounting period covers this date")
	ErrPeriodClosed     = errors.New("accounting: period is closed")
	ErrPeriodLocked     = errors.New("accounting: period is locked")
)

// Period statuses.
const (
	PeriodOpen    = "open"
	PeriodClosing = "closing"
	PeriodClosed  = "closed"
	PeriodLocked  = "locked"
)

type Period struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	StartDate time.Time
	EndDate   time.Time
	Status    string
	ClosedBy  *uuid.UUID
	ClosedAt  *time.Time
	LockedBy  *uuid.UUID
	LockedAt  *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

const periodColumns = `
    id, tenant_id, start_date, end_date, status, closed_by, closed_at, locked_by, locked_at, created_at, updated_at
`

func scanPeriod(row pgx.Row, p *Period) error {
	return row.Scan(
		&p.ID, &p.TenantID, &p.StartDate, &p.EndDate, &p.Status,
		&p.ClosedBy, &p.ClosedAt, &p.LockedBy, &p.LockedAt, &p.CreatedAt, &p.UpdatedAt,
	)
}

func (r *Repo) CreatePeriod(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, start, end time.Time) (*Period, error) {
	var p Period
	err := scanPeriod(tx.QueryRow(ctx, `
		INSERT INTO accounting_periods (tenant_id, start_date, end_date) VALUES ($1, $2, $3)
		RETURNING `+periodColumns,
		tenantID, start, end,
	), &p)
	if err != nil {
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23P01" { // exclusion_violation
			return nil, ErrPeriodOverlap
		}
		return nil, err
	}
	return &p, nil
}

func (r *Repo) ListPeriods(ctx context.Context, tenantID uuid.UUID) ([]Period, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+periodColumns+` FROM accounting_periods WHERE tenant_id = $1 ORDER BY start_date DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Period{}
	for rows.Next() {
		var p Period
		if err := scanPeriod(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListPeriodsPage returns a page of accounting periods for the tenant ordered
// by start_date DESC (with id as a tiebreaker for stable paging), applying the
// supplied limit and offset.
func (r *Repo) ListPeriodsPage(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]Period, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+periodColumns+` FROM accounting_periods WHERE tenant_id = $1 ORDER BY start_date DESC, id LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Period{}
	for rows.Next() {
		var p Period
		if err := scanPeriod(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) GetPeriod(ctx context.Context, tenantID, id uuid.UUID) (*Period, error) {
	var p Period
	err := scanPeriod(r.pool.QueryRow(ctx, `SELECT `+periodColumns+` FROM accounting_periods WHERE tenant_id = $1 AND id = $2`, tenantID, id), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPeriodNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// transition moves a period between statuses, guarding the allowed edges.
func (r *Repo) transition(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, from, to string, actorID uuid.UUID) (*Period, error) {
	closing := to == PeriodClosed
	locking := to == PeriodLocked
	var p Period
	err := scanPeriod(tx.QueryRow(ctx, `
		UPDATE accounting_periods SET
		    status    = $4,
		    closed_by = CASE WHEN $5 THEN $6::uuid ELSE closed_by END,
		    closed_at = CASE WHEN $5 THEN now()    ELSE closed_at END,
		    locked_by = CASE WHEN $7 THEN $6::uuid ELSE locked_by END,
		    locked_at = CASE WHEN $7 THEN now()    ELSE locked_at END
		WHERE tenant_id = $1 AND id = $2 AND status = $3
		RETURNING `+periodColumns,
		tenantID, id, from, to, closing, actorID, locking,
	), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gErr := r.GetPeriod(ctx, tenantID, id); errors.Is(gErr, ErrPeriodNotFound) {
			return nil, ErrPeriodNotFound
		}
		return nil, ErrPeriodTransition
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repo) StartClose(ctx context.Context, tx pgx.Tx, tenantID, id, actorID uuid.UUID) (*Period, error) {
	return r.transition(ctx, tx, tenantID, id, PeriodOpen, PeriodClosing, actorID)
}

func (r *Repo) ClosePeriod(ctx context.Context, tx pgx.Tx, tenantID, id, actorID uuid.UUID) (*Period, error) {
	return r.transition(ctx, tx, tenantID, id, PeriodClosing, PeriodClosed, actorID)
}

func (r *Repo) ReopenPeriod(ctx context.Context, tx pgx.Tx, tenantID, id, actorID uuid.UUID) (*Period, error) {
	return r.transition(ctx, tx, tenantID, id, PeriodClosed, PeriodOpen, actorID)
}

func (r *Repo) LockPeriod(ctx context.Context, tx pgx.Tx, tenantID, id, actorID uuid.UUID) (*Period, error) {
	return r.transition(ctx, tx, tenantID, id, PeriodClosed, PeriodLocked, actorID)
}

// resolvePostingPeriod finds the period covering a date and enforces the
// posting guard: locked periods reject everything; closed periods reject
// unless allowClosed (the adjustment flow).
func (r *Repo) resolvePostingPeriod(ctx context.Context, q database.Querier, tenantID uuid.UUID, date time.Time, allowClosed bool) (uuid.UUID, error) {
	var id, status = uuid.UUID{}, ""
	err := q.QueryRow(ctx, `
		SELECT id, status FROM accounting_periods
		WHERE tenant_id = $1 AND $2::date BETWEEN start_date AND end_date
	`, tenantID, date).Scan(&id, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoPeriod
	}
	if err != nil {
		return uuid.Nil, err
	}
	switch status {
	case PeriodLocked:
		return uuid.Nil, ErrPeriodLocked
	case PeriodClosed:
		if !allowClosed {
			return uuid.Nil, ErrPeriodClosed
		}
	}
	return id, nil
}
