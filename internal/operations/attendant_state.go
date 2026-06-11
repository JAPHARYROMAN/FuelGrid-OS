package operations

// Self-scoped reads behind the attendant workflow-state endpoint (Mobile
// Attendant App, Phase 1). Everything here is keyed by the calling attendant
// (shift membership / assignment ownership), so the mobile home screen needs
// no station-wide read access.

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AttendantAssignment is one of the calling attendant's nozzle assignments on
// a shift, denormalized with the labels the mobile home screen renders
// (pump/nozzle numbers, product) plus its confirmation state.
type AttendantAssignment struct {
	ID           uuid.UUID
	NozzleID     uuid.UUID
	PumpNumber   int
	NozzleNumber int
	ProductName  string
	ProductColor string
	// MeterDecimalPlaces is the nozzle's meter precision (0..4). The mobile
	// opening/closing capture screens validate input scale against it client
	// side, mirroring readings.ValidateScale on the server (Phase 2).
	MeterDecimalPlaces int
	AssignedAt         time.Time
	ConfirmedAt        *time.Time
}

// AttendantAssignments returns the attendant's own labelled nozzle
// assignments on the shift, ordered pump then nozzle.
func (r *Repo) AttendantAssignments(ctx context.Context, tenantID, shiftID, attendantID uuid.UUID) ([]AttendantAssignment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT sna.id, n.id, p.number, n.number, pr.name, pr.color,
		       n.meter_decimal_places, sna.assigned_at, sna.confirmed_at
		FROM shift_nozzle_assignments sna
		JOIN nozzles n   ON n.id  = sna.nozzle_id
		JOIN pumps p     ON p.id  = n.pump_id
		JOIN products pr ON pr.id = n.product_id
		WHERE sna.tenant_id = $1 AND sna.shift_id = $2 AND sna.attendant_id = $3
		ORDER BY p.number, n.number
	`, tenantID, shiftID, attendantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AttendantAssignment
	for rows.Next() {
		var a AttendantAssignment
		if err := rows.Scan(&a.ID, &a.NozzleID, &a.PumpNumber, &a.NozzleNumber,
			&a.ProductName, &a.ProductColor, &a.MeterDecimalPlaces,
			&a.AssignedAt, &a.ConfirmedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// StationName returns the station's display name, or pgx.ErrNoRows. Exposed
// for the self-scoped attendant snapshot, which must label the actor's own
// station without granting station.read.
func (r *Repo) StationName(ctx context.Context, tenantID, stationID uuid.UUID) (string, error) {
	var name string
	err := r.pool.QueryRow(ctx, `
		SELECT name FROM stations WHERE tenant_id = $1 AND id = $2
	`, tenantID, stationID).Scan(&name)
	return name, err
}

// LatestApprovedShiftTodayForAttendant returns the actor's most recent
// APPROVED shift opened on the current date, or pgx.ErrNoRows. Once a shift
// is approved it leaves ActiveShiftForAttendant; this lookup lets the mobile
// home screen keep showing "shift complete" for the rest of the day instead
// of snapping back to off-duty.
func (r *Repo) LatestApprovedShiftTodayForAttendant(ctx context.Context, tenantID, userID uuid.UUID) (*Shift, error) {
	var s Shift
	err := scanShift(r.pool.QueryRow(ctx, `
		SELECT s.id, s.tenant_id, s.station_id, s.operating_day_id, s.name, s.status,
		       s.opened_by, s.opened_at, s.closed_by, s.closed_at, s.approved_by, s.approved_at,
		       s.notes, s.slot, s.team_id, s.created_at, s.updated_at
		FROM shifts s
		JOIN shift_attendants a ON a.shift_id = s.id
		WHERE s.tenant_id = $1 AND a.user_id = $2 AND s.status = 'approved'
		  AND s.opened_at::date = CURRENT_DATE
		ORDER BY s.opened_at DESC, s.id DESC
		LIMIT 1
	`, tenantID, userID), &s)
	if err != nil {
		return nil, err
	}
	return &s, nil
}
