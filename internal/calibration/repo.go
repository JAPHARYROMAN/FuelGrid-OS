package calibration

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Chart is a versioned strapping chart for one tank. EntryCount is populated
// by the list/active reads; it is not a stored column.
type Chart struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	TankID         uuid.UUID
	Name           string
	EffectiveFrom  time.Time
	EffectiveUntil *time.Time
	Status         string
	Source         string
	EntryCount     int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

// ErrNoActiveChart is returned by Lookup when a tank has no active chart.
var ErrNoActiveChart = errors.New("calibration: tank has no active chart")

const chartColumns = `
    c.id, c.tenant_id, c.tank_id, c.name, c.effective_from, c.effective_until,
    c.status, c.source, c.created_at, c.updated_at,
    (SELECT count(*) FROM tank_calibration_entries e WHERE e.chart_id = c.id)
`

func scanChart(row pgx.Row, c *Chart) error {
	return row.Scan(
		&c.ID, &c.TenantID, &c.TankID, &c.Name, &c.EffectiveFrom, &c.EffectiveUntil,
		&c.Status, &c.Source, &c.CreatedAt, &c.UpdatedAt, &c.EntryCount,
	)
}

// ListCharts returns every chart for a tank, newest effective first.
func (r *Repo) ListCharts(ctx context.Context, tenantID, tankID uuid.UUID) ([]Chart, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+chartColumns+`
		FROM tank_calibration_charts c
		WHERE c.tenant_id = $1 AND c.tank_id = $2
		ORDER BY c.effective_from DESC
	`, tenantID, tankID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Chart
	for rows.Next() {
		var c Chart
		if err := scanChart(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ActiveChart returns the tank's current active chart, or pgx.ErrNoRows.
func (r *Repo) ActiveChart(ctx context.Context, tenantID, tankID uuid.UUID) (*Chart, error) {
	var c Chart
	if err := scanChart(r.pool.QueryRow(ctx, `
		SELECT `+chartColumns+`
		FROM tank_calibration_charts c
		WHERE c.tenant_id = $1 AND c.tank_id = $2 AND c.status = 'active'
	`, tenantID, tankID), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// SupersedeActive marks the tank's current active chart as superseded and
// stamps effective_until. It returns the superseded chart's id, or nil when
// the tank had no active chart. Runs inside the caller's transaction.
func (r *Repo) SupersedeActive(ctx context.Context, tx pgx.Tx, tenantID, tankID uuid.UUID) (*uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		UPDATE tank_calibration_charts
		SET status = 'superseded', effective_until = now()
		WHERE tenant_id = $1 AND tank_id = $2 AND status = 'active'
		RETURNING id
	`, tenantID, tankID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// CreateChart inserts a new active chart and its entries inside the caller's
// transaction. The caller is responsible for superseding any prior active
// chart first (the partial unique index enforces one active per tank).
func (r *Repo) CreateChart(ctx context.Context, tx pgx.Tx, tenantID, tankID uuid.UUID, name, source string, effectiveFrom time.Time, entries []Entry) (*Chart, error) {
	var c Chart
	if err := scanChart(tx.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO tank_calibration_charts (tenant_id, tank_id, name, source, effective_from)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING *
		)
		SELECT `+chartColumns+` FROM ins c
	`, tenantID, tankID, name, source, effectiveFrom), &c); err != nil {
		return nil, err
	}

	rows := make([][]any, len(entries))
	for i, e := range entries {
		rows[i] = []any{c.ID, int64(e.DipMM), e.VolumeLitres}
	}
	if _, err := tx.CopyFrom(ctx,
		pgx.Identifier{"tank_calibration_entries"},
		[]string{"chart_id", "dip_mm", "volume_litres"},
		pgx.CopyFromRows(rows),
	); err != nil {
		return nil, err
	}
	c.EntryCount = len(entries)
	return &c, nil
}

// entriesFor loads a chart's points sorted by dip.
func (r *Repo) entriesFor(ctx context.Context, chartID uuid.UUID) ([]Entry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT dip_mm, volume_litres
		FROM tank_calibration_entries
		WHERE chart_id = $1
		ORDER BY dip_mm
	`, chartID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.DipMM, &e.VolumeLitres); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Lookup interpolates the litre volume for a dip against the tank's active
// chart. Returns ErrNoActiveChart when none exists, or ErrOutOfRange when
// the dip falls outside the charted range.
func (r *Repo) Lookup(ctx context.Context, tenantID, tankID uuid.UUID, dipMM float64) (volume float64, chartID uuid.UUID, err error) {
	chart, err := r.ActiveChart(ctx, tenantID, tankID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, uuid.Nil, ErrNoActiveChart
	}
	if err != nil {
		return 0, uuid.Nil, err
	}
	entries, err := r.entriesFor(ctx, chart.ID)
	if err != nil {
		return 0, uuid.Nil, err
	}
	v, err := Interpolate(entries, dipMM)
	if err != nil {
		return 0, chart.ID, err
	}
	return v, chart.ID, nil
}
