// Package operations is the data layer for the station operating cadence —
// operating days now, shifts and assignments in later Phase-3 stages.
package operations

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// OperatingDay buckets a station's work for one business date through an
// open -> closed -> locked lifecycle.
type OperatingDay struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	StationID    uuid.UUID
	BusinessDate time.Time
	Status       string
	OpenedBy     uuid.UUID
	OpenedAt     time.Time
	ClosedBy     *uuid.UUID
	ClosedAt     *time.Time
	LockedBy     *uuid.UUID
	LockedAt     *time.Time
	Notes        *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

var ErrNotFound = errors.New("operations: operating day not found")

const dayColumns = `
    id, tenant_id, station_id, business_date, status,
    opened_by, opened_at, closed_by, closed_at, locked_by, locked_at,
    notes, created_at, updated_at
`

func scanDay(row pgx.Row, d *OperatingDay) error {
	return row.Scan(
		&d.ID, &d.TenantID, &d.StationID, &d.BusinessDate, &d.Status,
		&d.OpenedBy, &d.OpenedAt, &d.ClosedBy, &d.ClosedAt, &d.LockedBy, &d.LockedAt,
		&d.Notes, &d.CreatedAt, &d.UpdatedAt,
	)
}

// ListDays returns a station's operating days, newest date first.
func (r *Repo) ListDays(ctx context.Context, tenantID, stationID uuid.UUID) ([]OperatingDay, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+dayColumns+`
		FROM operating_days
		WHERE tenant_id = $1 AND station_id = $2
		ORDER BY business_date DESC
	`, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OperatingDay
	for rows.Next() {
		var d OperatingDay
		if err := scanDay(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// LatestActiveDayForStation returns the station's most recent day still being
// operated (open or closed — locked days are done), or pgx.ErrNoRows. This is
// the day the supervisor operations dashboard runs against.
func (r *Repo) LatestActiveDayForStation(ctx context.Context, tenantID, stationID uuid.UUID) (*OperatingDay, error) {
	var d OperatingDay
	if err := scanDay(r.pool.QueryRow(ctx, `
		SELECT `+dayColumns+`
		FROM operating_days
		WHERE tenant_id = $1 AND station_id = $2 AND status IN ('open', 'closed')
		ORDER BY business_date DESC
		LIMIT 1
	`, tenantID, stationID), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repo) GetDay(ctx context.Context, tenantID, id uuid.UUID) (*OperatingDay, error) {
	var d OperatingDay
	if err := scanDay(r.pool.QueryRow(ctx, `
		SELECT `+dayColumns+`
		FROM operating_days WHERE id = $1 AND tenant_id = $2
	`, id, tenantID), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// GetDayForUpdate loads a day and locks its row FOR UPDATE inside the caller's
// tx. A day status transition (close/reopen) must re-read and re-validate under
// this lock so it serializes against a concurrent shift-open — which takes
// FOR SHARE on the same row (see LockDayStatusForShare) — and against another
// concurrent close. Returns ErrNotFound when the day does not exist.
func (r *Repo) GetDayForUpdate(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*OperatingDay, error) {
	var d OperatingDay
	err := scanDay(tx.QueryRow(ctx, `
		SELECT `+dayColumns+`
		FROM operating_days WHERE id = $1 AND tenant_id = $2
		FOR UPDATE
	`, id, tenantID), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// LockDayStatusForShare takes a FOR SHARE lock on the day row inside the
// caller's tx and returns its current status. FOR SHARE conflicts with the
// FOR UPDATE a day close/reopen/lock takes, so a shift cannot be opened on a
// day that is concurrently being closed (and vice versa): whichever tx commits
// first wins, and the other observes the post-commit state. Returns ErrNotFound
// when the day does not exist for the tenant.
func (r *Repo) LockDayStatusForShare(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `
		SELECT status FROM operating_days WHERE id = $1 AND tenant_id = $2
		FOR SHARE
	`, id, tenantID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return status, err
}

// OpenDay creates a new open day for a station/date inside the caller's tx.
// The partial unique index rejects a second non-locked day for the same
// (station, date) — surfaced as a unique-violation for the handler to map.
func (r *Repo) OpenDay(ctx context.Context, tx pgx.Tx, tenantID, stationID, openedBy uuid.UUID, businessDate time.Time, notes *string) (*OperatingDay, error) {
	var d OperatingDay
	if err := scanDay(tx.QueryRow(ctx, `
		INSERT INTO operating_days (tenant_id, station_id, business_date, opened_by, notes)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+dayColumns,
		tenantID, stationID, businessDate, openedBy, notes,
	), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// SetStatus moves a day between open and closed, stamping closed_by/at when
// closing and clearing them when reopening. Locking goes through Lock.
func (r *Repo) SetStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status string, actorID uuid.UUID) (*OperatingDay, error) {
	closing := status == "closed"
	var d OperatingDay
	err := scanDay(tx.QueryRow(ctx, `
		UPDATE operating_days
		SET status    = $3,
		    closed_by = CASE WHEN $4 THEN $5::uuid ELSE NULL END,
		    closed_at = CASE WHEN $4 THEN now()    ELSE NULL END
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+dayColumns,
		id, tenantID, status, closing, actorID,
	), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Lock marks a day locked (terminal), stamping locked_by/at.
func (r *Repo) Lock(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, actorID uuid.UUID) (*OperatingDay, error) {
	var d OperatingDay
	err := scanDay(tx.QueryRow(ctx, `
		UPDATE operating_days
		SET status = 'locked', locked_by = $3, locked_at = now()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+dayColumns,
		id, tenantID, actorID,
	), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}
