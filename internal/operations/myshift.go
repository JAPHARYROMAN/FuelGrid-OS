package operations

import (
	"context"

	"github.com/google/uuid"
)

// AssignedNozzleDetail is a denormalized row for the attendant console: the
// nozzle the attendant runs plus the labels (pump/product/tank) needed to
// render it, so the attendant needs no station-wide read access.
type AssignedNozzleDetail struct {
	NozzleID           uuid.UUID
	PumpNumber         int
	NozzleNumber       int
	ProductName        string
	ProductColor       string
	TankID             uuid.UUID
	TankCode           string
	DefaultPrice       float64
	MeterDecimalPlaces int
}

// ActiveShiftForAttendant returns the actor's most recent non-approved shift
// (open or closed) on which they're an attendant, or pgx.ErrNoRows.
func (r *Repo) ActiveShiftForAttendant(ctx context.Context, tenantID, userID uuid.UUID) (*Shift, error) {
	var s Shift
	if err := scanShift(r.pool.QueryRow(ctx, `
		SELECT s.id, s.tenant_id, s.station_id, s.operating_day_id, s.name, s.status,
		       s.opened_by, s.opened_at, s.closed_by, s.closed_at, s.approved_by, s.approved_at,
		       s.notes, s.created_at, s.updated_at
		FROM shifts s
		JOIN shift_attendants a ON a.shift_id = s.id
		WHERE s.tenant_id = $1 AND a.user_id = $2 AND s.status IN ('open', 'closed')
		ORDER BY s.opened_at DESC
		LIMIT 1
	`, tenantID, userID), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// AssignedNozzleDetails returns the labelled nozzles an attendant runs on a
// shift.
func (r *Repo) AssignedNozzleDetails(ctx context.Context, tenantID, shiftID, attendantID uuid.UUID) ([]AssignedNozzleDetail, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT n.id, p.number, n.number, pr.name, pr.color, t.id, t.code,
		       n.default_price, n.meter_decimal_places
		FROM shift_nozzle_assignments sna
		JOIN nozzles n  ON n.id  = sna.nozzle_id
		JOIN pumps p    ON p.id  = n.pump_id
		JOIN products pr ON pr.id = n.product_id
		JOIN tanks t    ON t.id  = n.tank_id
		WHERE sna.tenant_id = $1 AND sna.shift_id = $2 AND sna.attendant_id = $3
		ORDER BY p.number, n.number
	`, tenantID, shiftID, attendantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AssignedNozzleDetail
	for rows.Next() {
		var d AssignedNozzleDetail
		if err := rows.Scan(&d.NozzleID, &d.PumpNumber, &d.NozzleNumber, &d.ProductName,
			&d.ProductColor, &d.TankID, &d.TankCode, &d.DefaultPrice, &d.MeterDecimalPlaces); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
