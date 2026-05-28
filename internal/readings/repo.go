package readings

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// MeterReading is one captured opening/closing pump-meter reading. A
// correction inserts a new active row and marks the prior one superseded,
// pointing back at it via SupersedesID.
type MeterReading struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	ShiftID      uuid.UUID
	NozzleID     uuid.UUID
	ReadingType  string
	Reading      float64
	RecordedBy   uuid.UUID
	RecordedAt   time.Time
	SupersedesID *uuid.UUID
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CaptureInput struct {
	ShiftID      uuid.UUID
	NozzleID     uuid.UUID
	ReadingType  string
	Reading      float64
	RecordedBy   uuid.UUID
	SupersedesID *uuid.UUID
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

var ErrNotFound = errors.New("readings: meter reading not found")

const meterColumns = `
    id, tenant_id, shift_id, nozzle_id, reading_type, reading,
    recorded_by, recorded_at, supersedes_id, status, created_at, updated_at
`

func scanMeter(row pgx.Row, m *MeterReading) error {
	return row.Scan(
		&m.ID, &m.TenantID, &m.ShiftID, &m.NozzleID, &m.ReadingType, &m.Reading,
		&m.RecordedBy, &m.RecordedAt, &m.SupersedesID, &m.Status, &m.CreatedAt, &m.UpdatedAt,
	)
}

// ListActiveForShift returns the shift's active readings, ordered by nozzle
// then type so opening precedes closing.
func (r *Repo) ListActiveForShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]MeterReading, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+meterColumns+`
		FROM meter_readings
		WHERE tenant_id = $1 AND shift_id = $2 AND status = 'active'
		ORDER BY nozzle_id, reading_type
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MeterReading
	for rows.Next() {
		var m MeterReading
		if err := scanMeter(rows, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*MeterReading, error) {
	var m MeterReading
	if err := scanMeter(r.pool.QueryRow(ctx, `
		SELECT `+meterColumns+` FROM meter_readings WHERE id = $1 AND tenant_id = $2
	`, id, tenantID), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Capture inserts a new active reading. A second active reading for the same
// (shift, nozzle, reading_type) trips the partial unique index, which the
// handler maps to 409.
func (r *Repo) Capture(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CaptureInput) (*MeterReading, error) {
	var m MeterReading
	if err := scanMeter(tx.QueryRow(ctx, `
		INSERT INTO meter_readings
		    (tenant_id, shift_id, nozzle_id, reading_type, reading, recorded_by, supersedes_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+meterColumns,
		tenantID, in.ShiftID, in.NozzleID, in.ReadingType, in.Reading, in.RecordedBy, in.SupersedesID,
	), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Supersede marks an active reading superseded inside the caller's tx.
func (r *Repo) Supersede(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE meter_readings SET status = 'superseded'
		WHERE id = $1 AND tenant_id = $2 AND status = 'active'
	`, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
