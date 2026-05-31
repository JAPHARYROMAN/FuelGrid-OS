package readings

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DipReading is one captured opening/closing tank dip. The litre volume and
// the chart that resolved it are snapshotted at capture time.
type DipReading struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	ShiftID     uuid.UUID
	TankID      uuid.UUID
	ReadingType string
	// DipMM and VolumeLitres are exact decimal STRINGS (both numeric(14,3) read
	// ::text); never Go float64. water_mm/temperature_c stay float pointers
	// (sensor metadata, not ledger figures).
	DipMM        string
	VolumeLitres string
	WaterMM      *float64
	TemperatureC *float64
	ChartID      uuid.UUID
	RecordedBy   uuid.UUID
	RecordedAt   time.Time
	SupersedesID *uuid.UUID
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CaptureDipInput struct {
	ShiftID      uuid.UUID
	TankID       uuid.UUID
	ReadingType  string
	DipMM        string // numeric(14,3), bound $N::numeric
	VolumeLitres string // numeric(14,3), bound $N::numeric
	WaterMM      *float64
	TemperatureC *float64
	ChartID      uuid.UUID
	RecordedBy   uuid.UUID
	SupersedesID *uuid.UUID
}

var ErrDipNotFound = errors.New("readings: dip reading not found")

const dipColumns = `
    id, tenant_id, shift_id, tank_id, reading_type, dip_mm::text, volume_litres::text,
    water_mm, temperature_c, chart_id, recorded_by, recorded_at,
    supersedes_id, status, created_at, updated_at
`

func scanDip(row pgx.Row, d *DipReading) error {
	return row.Scan(
		&d.ID, &d.TenantID, &d.ShiftID, &d.TankID, &d.ReadingType, &d.DipMM, &d.VolumeLitres,
		&d.WaterMM, &d.TemperatureC, &d.ChartID, &d.RecordedBy, &d.RecordedAt,
		&d.SupersedesID, &d.Status, &d.CreatedAt, &d.UpdatedAt,
	)
}

func (r *Repo) ListDipsForShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]DipReading, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+dipColumns+`
		FROM tank_dip_readings
		WHERE tenant_id = $1 AND shift_id = $2 AND status = 'active'
		ORDER BY tank_id, reading_type
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DipReading
	for rows.Next() {
		var d DipReading
		if err := scanDip(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListDipsForShiftPage returns a page of the shift's active dip readings,
// ordered by tank then type with id as a deterministic tiebreaker so paging is
// stable. limit/offset come from the clamped HTTP page params.
func (r *Repo) ListDipsForShiftPage(ctx context.Context, tenantID, shiftID uuid.UUID, limit, offset int) ([]DipReading, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+dipColumns+`
		FROM tank_dip_readings
		WHERE tenant_id = $1 AND shift_id = $2 AND status = 'active'
		ORDER BY tank_id, reading_type, id
		LIMIT $3 OFFSET $4
	`, tenantID, shiftID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DipReading
	for rows.Next() {
		var d DipReading
		if err := scanDip(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repo) GetDip(ctx context.Context, tenantID, id uuid.UUID) (*DipReading, error) {
	var d DipReading
	if err := scanDip(r.pool.QueryRow(ctx, `
		SELECT `+dipColumns+` FROM tank_dip_readings WHERE id = $1 AND tenant_id = $2
	`, id, tenantID), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// FirstDipForTank returns the tank's earliest active dip reading — the
// physical level its stock ledger opens from. Returns pgx.ErrNoRows when the
// tank has never been dipped.
func (r *Repo) FirstDipForTank(ctx context.Context, tenantID, tankID uuid.UUID) (*DipReading, error) {
	var d DipReading
	if err := scanDip(r.pool.QueryRow(ctx, `
		SELECT `+dipColumns+`
		FROM tank_dip_readings
		WHERE tenant_id = $1 AND tank_id = $2 AND status = 'active'
		ORDER BY recorded_at, created_at
		LIMIT 1
	`, tenantID, tankID), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repo) CaptureDip(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CaptureDipInput) (*DipReading, error) {
	var d DipReading
	if err := scanDip(tx.QueryRow(ctx, `
		INSERT INTO tank_dip_readings
		    (tenant_id, shift_id, tank_id, reading_type, dip_mm, volume_litres,
		     water_mm, temperature_c, chart_id, recorded_by, supersedes_id)
		VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, $7, $8, $9, $10, $11)
		RETURNING `+dipColumns,
		tenantID, in.ShiftID, in.TankID, in.ReadingType, in.DipMM, in.VolumeLitres,
		in.WaterMM, in.TemperatureC, in.ChartID, in.RecordedBy, in.SupersedesID,
	), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repo) SupersedeDip(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE tank_dip_readings SET status = 'superseded'
		WHERE id = $1 AND tenant_id = $2 AND status = 'active'
	`, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrDipNotFound
	}
	return nil
}

// ClosingDipForTankDay returns the tank's most recent active closing dip among
// the shifts of one operating day — the physical figure a reconciliation
// compares book stock against. Returns pgx.ErrNoRows when the tank has no
// closing dip that day.
func (r *Repo) ClosingDipForTankDay(ctx context.Context, tenantID, tankID, operatingDayID uuid.UUID) (*DipReading, error) {
	var d DipReading
	if err := scanDip(r.pool.QueryRow(ctx, `
		SELECT `+prefixedDipColumns+`
		FROM tank_dip_readings d
		JOIN shifts sh ON sh.id = d.shift_id AND sh.tenant_id = d.tenant_id
		WHERE d.tenant_id = $1 AND d.tank_id = $2 AND d.reading_type = 'closing'
		  AND d.status = 'active' AND sh.operating_day_id = $3
		ORDER BY d.recorded_at DESC, d.created_at DESC
		LIMIT 1
	`, tenantID, tankID, operatingDayID), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// prefixedDipColumns is dipColumns qualified to the d alias for joins.
const prefixedDipColumns = `
    d.id, d.tenant_id, d.shift_id, d.tank_id, d.reading_type, d.dip_mm::text, d.volume_litres::text,
    d.water_mm, d.temperature_c, d.chart_id, d.recorded_by, d.recorded_at,
    d.supersedes_id, d.status, d.created_at, d.updated_at
`

// LatestDip is the most recent active dip for a tank plus the metadata a
// dashboard needs to judge how "current" it is: when it was taken, whether it
// was an opening or closing read, and the business date of the operating day
// it belongs to.
type LatestDip struct {
	TankID       uuid.UUID
	VolumeLitres float64
	ReadingType  string
	RecordedAt   time.Time
	BusinessDate time.Time
}

// LatestDipsForStation returns, per tank at a station, its most recent active
// dip with metadata. "Current" is defined as the latest active reading by
// recorded_at regardless of day; the caller gets the business date and
// reading type so a stale (prior-day) reading is visible rather than silently
// presented as today's level.
func (r *Repo) LatestDipsForStation(ctx context.Context, tenantID, stationID uuid.UUID) (map[uuid.UUID]LatestDip, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (d.tank_id)
		       d.tank_id, d.volume_litres, d.reading_type, d.recorded_at, od.business_date
		FROM tank_dip_readings d
		JOIN tanks t          ON t.id  = d.tank_id
		JOIN shifts sh        ON sh.id = d.shift_id
		JOIN operating_days od ON od.id = sh.operating_day_id
		WHERE d.tenant_id = $1 AND d.status = 'active' AND t.station_id = $2
		ORDER BY d.tank_id, d.recorded_at DESC
	`, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]LatestDip{}
	for rows.Next() {
		var d LatestDip
		if err := rows.Scan(&d.TankID, &d.VolumeLitres, &d.ReadingType, &d.RecordedAt, &d.BusinessDate); err != nil {
			return nil, err
		}
		out[d.TankID] = d
	}
	return out, rows.Err()
}
