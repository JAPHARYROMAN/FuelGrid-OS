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
	ID          uuid.UUID
	TenantID    uuid.UUID
	ShiftID     uuid.UUID
	NozzleID    uuid.UUID
	ReadingType string
	// Reading is the exact decimal STRING form of the meter (reading
	// numeric(14,3) read ::text); litres-dispensed arithmetic is done in SQL,
	// never Go float64.
	Reading      string
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
	Reading      string // numeric(14,3), bound $N::numeric
	RecordedBy   uuid.UUID
	SupersedesID *uuid.UUID
}

// NozzleDispensed is the per-nozzle opening/closing/litres-dispensed figure for
// a shift, computed entirely in SQL numeric. All three are exact decimal
// strings; LitresDispensed is closing - opening done in the DB (OPS-001), and
// only nozzles whose closing is >= opening (no meter rollback) are returned.
type NozzleDispensed struct {
	NozzleID        uuid.UUID
	Opening         string
	Closing         string
	LitresDispensed string
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

var ErrNotFound = errors.New("readings: meter reading not found")

const meterColumns = `
    id, tenant_id, shift_id, nozzle_id, reading_type, reading::text,
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

// DispensedForShift returns, per nozzle that has BOTH an active opening and
// closing reading on the shift, the opening, closing, and litres dispensed —
// all computed in SQL numeric (closing - opening) and returned as exact decimal
// strings. Nozzles whose closing reads below opening (a meter rollback) are
// excluded, mirroring the Go LitresDispensed guard but without any float math
// (OPS-001). Results are ordered by nozzle_id for stable output.
func (r *Repo) DispensedForShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]NozzleDispensed, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT o.nozzle_id,
		       o.reading::text,
		       c.reading::text,
		       (c.reading - o.reading)::text
		FROM meter_readings o
		JOIN meter_readings c
		  ON c.tenant_id = o.tenant_id AND c.shift_id = o.shift_id
		 AND c.nozzle_id = o.nozzle_id
		 AND c.reading_type = 'closing' AND c.status = 'active'
		WHERE o.tenant_id = $1 AND o.shift_id = $2
		  AND o.reading_type = 'opening' AND o.status = 'active'
		  AND c.reading >= o.reading
		ORDER BY o.nozzle_id
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NozzleDispensed
	for rows.Next() {
		var d NozzleDispensed
		if err := rows.Scan(&d.NozzleID, &d.Opening, &d.Closing, &d.LitresDispensed); err != nil {
			return nil, err
		}
		out = append(out, d)
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
		VALUES ($1, $2, $3, $4, $5::numeric, $6, $7)
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
