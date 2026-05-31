package pumps

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Calibration is one recorded calibration event for a pump.
type Calibration struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	PumpID           uuid.UUID
	PerformedAt      time.Time
	PerformedBy      uuid.UUID
	Notes            *string
	TolerancePercent *float64
	Status           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateCalibrationInput struct {
	PumpID           uuid.UUID
	PerformedBy      uuid.UUID
	PerformedAt      *time.Time
	Notes            *string
	TolerancePercent *float64
	Status           string
}

const calibrationColumns = `
    id, tenant_id, pump_id, performed_at, performed_by, notes,
    tolerance_percent, status, created_at, updated_at
`

func scanCalibration(row pgx.Row, c *Calibration) error {
	return row.Scan(
		&c.ID, &c.TenantID, &c.PumpID, &c.PerformedAt, &c.PerformedBy, &c.Notes,
		&c.TolerancePercent, &c.Status, &c.CreatedAt, &c.UpdatedAt,
	)
}

// ListCalibrations returns a pump's calibration history, newest first.
func (r *Repo) ListCalibrations(ctx context.Context, tenantID, pumpID uuid.UUID) ([]Calibration, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+calibrationColumns+`
		FROM pump_calibrations
		WHERE tenant_id = $1 AND pump_id = $2
		ORDER BY performed_at DESC
	`, tenantID, pumpID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Calibration
	for rows.Next() {
		var c Calibration
		if err := scanCalibration(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListCalibrationsPage mirrors ListCalibrations with limit/offset paging and a
// stable (performed_at DESC, id) ordering. Callers fetch limit+1 to detect a
// further page.
func (r *Repo) ListCalibrationsPage(ctx context.Context, tenantID, pumpID uuid.UUID, limit, offset int) ([]Calibration, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+calibrationColumns+`
		FROM pump_calibrations
		WHERE tenant_id = $1 AND pump_id = $2
		ORDER BY performed_at DESC, id
		LIMIT $3 OFFSET $4
	`, tenantID, pumpID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Calibration
	for rows.Next() {
		var c Calibration
		if err := scanCalibration(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateCalibration records a calibration event inside the caller's tx.
func (r *Repo) CreateCalibration(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CreateCalibrationInput) (*Calibration, error) {
	status := in.Status
	if status == "" {
		status = "passed"
	}
	performedAt := time.Now()
	if in.PerformedAt != nil {
		performedAt = *in.PerformedAt
	}
	var c Calibration
	if err := scanCalibration(tx.QueryRow(ctx, `
		INSERT INTO pump_calibrations
		    (tenant_id, pump_id, performed_at, performed_by, notes, tolerance_percent, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+calibrationColumns,
		tenantID, in.PumpID, performedAt, in.PerformedBy, in.Notes, in.TolerancePercent, status,
	), &c); err != nil {
		return nil, err
	}
	return &c, nil
}
