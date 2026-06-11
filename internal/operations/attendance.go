package operations

// Attendance + nozzle-assignment confirmation (Mobile Attendant App, Phase 0).
// Check-in/out is one row per attendant per shift, flipped checked_in ->
// checked_out in place; the duplicate-check-in path is idempotent (the caller
// gets the existing row back). Assignment confirmation stamps confirmed_at on
// shift_nozzle_assignments exactly once.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Attendance is one attendant's check-in/out record for a shift.
type Attendance struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	StationID   uuid.UUID
	ShiftID     uuid.UUID
	AttendantID uuid.UUID
	Status      string
	CheckInAt   time.Time
	CheckOutAt  *time.Time
	// DeviceInfo is the raw JSON the mobile client sent at check-in (nil when
	// none was provided).
	DeviceInfo []byte
}

var (
	// ErrAttendanceNotFound is returned when no attendance row exists for the
	// (shift, attendant) pair.
	ErrAttendanceNotFound = errors.New("operations: attendance record not found")
)

const attendanceColumns = `
    id, tenant_id, station_id, shift_id, attendant_id, status,
    check_in_at, check_out_at, device_info
`

func scanAttendance(row pgx.Row, a *Attendance) error {
	return row.Scan(
		&a.ID, &a.TenantID, &a.StationID, &a.ShiftID, &a.AttendantID, &a.Status,
		&a.CheckInAt, &a.CheckOutAt, &a.DeviceInfo,
	)
}

// GetAttendance returns the attendant's record for the shift, or
// ErrAttendanceNotFound.
func (r *Repo) GetAttendance(ctx context.Context, tenantID, shiftID, attendantID uuid.UUID) (*Attendance, error) {
	var a Attendance
	err := scanAttendance(r.pool.QueryRow(ctx, `
		SELECT `+attendanceColumns+`
		FROM shift_attendance
		WHERE tenant_id = $1 AND shift_id = $2 AND attendant_id = $3
	`, tenantID, shiftID, attendantID), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAttendanceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// CheckIn inserts the attendant's check-in row inside the caller's tx. A
// concurrent duplicate trips uq_shift_attendance, which the handler maps onto
// the idempotent path.
func (r *Repo) CheckIn(ctx context.Context, tx pgx.Tx, tenantID, stationID, shiftID, attendantID uuid.UUID, deviceInfo []byte) (*Attendance, error) {
	var a Attendance
	if err := scanAttendance(tx.QueryRow(ctx, `
		INSERT INTO shift_attendance (tenant_id, station_id, shift_id, attendant_id, device_info)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+attendanceColumns,
		tenantID, stationID, shiftID, attendantID, deviceInfo,
	), &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// CheckOut flips a checked_in row to checked_out inside the caller's tx,
// stamping check_out_at exactly once. Returns ErrAttendanceNotFound when the
// attendant has no checked_in row (never checked in, or already out — the
// handler disambiguates via GetAttendance for the idempotent path).
func (r *Repo) CheckOut(ctx context.Context, tx pgx.Tx, tenantID, shiftID, attendantID uuid.UUID) (*Attendance, error) {
	var a Attendance
	err := scanAttendance(tx.QueryRow(ctx, `
		UPDATE shift_attendance
		SET status = 'checked_out', check_out_at = now()
		WHERE tenant_id = $1 AND shift_id = $2 AND attendant_id = $3 AND status = 'checked_in'
		RETURNING `+attendanceColumns,
		tenantID, shiftID, attendantID,
	), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAttendanceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListAttendanceForShift returns the shift's attendance records, oldest
// check-in first.
func (r *Repo) ListAttendanceForShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]Attendance, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+attendanceColumns+`
		FROM shift_attendance
		WHERE tenant_id = $1 AND shift_id = $2
		ORDER BY check_in_at, id
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attendance
	for rows.Next() {
		var a Attendance
		if err := scanAttendance(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- nozzle-assignment confirmation ---

// ConfirmableAssignment is a shift_nozzle_assignments row with its
// confirmation state, loaded for the confirm endpoint.
type ConfirmableAssignment struct {
	ID          uuid.UUID
	ShiftID     uuid.UUID
	NozzleID    uuid.UUID
	AttendantID uuid.UUID
	AssignedAt  time.Time
	ConfirmedAt *time.Time
}

// GetNozzleAssignment loads one assignment on the shift, or
// ErrAssignmentNotFound.
func (r *Repo) GetNozzleAssignment(ctx context.Context, tenantID, shiftID, assignmentID uuid.UUID) (*ConfirmableAssignment, error) {
	var a ConfirmableAssignment
	err := r.pool.QueryRow(ctx, `
		SELECT id, shift_id, nozzle_id, attendant_id, assigned_at, confirmed_at
		FROM shift_nozzle_assignments
		WHERE tenant_id = $1 AND shift_id = $2 AND id = $3
	`, tenantID, shiftID, assignmentID).Scan(
		&a.ID, &a.ShiftID, &a.NozzleID, &a.AttendantID, &a.AssignedAt, &a.ConfirmedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssignmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ConfirmNozzleAssignment stamps confirmed_at exactly once inside the caller's
// tx. Returns ErrAssignmentNotFound when the row is absent or already
// confirmed (the handler disambiguates for the idempotent path).
func (r *Repo) ConfirmNozzleAssignment(ctx context.Context, tx pgx.Tx, tenantID, shiftID, assignmentID uuid.UUID) (*ConfirmableAssignment, error) {
	var a ConfirmableAssignment
	err := tx.QueryRow(ctx, `
		UPDATE shift_nozzle_assignments
		SET confirmed_at = now()
		WHERE tenant_id = $1 AND shift_id = $2 AND id = $3 AND confirmed_at IS NULL
		RETURNING id, shift_id, nozzle_id, attendant_id, assigned_at, confirmed_at
	`, tenantID, shiftID, assignmentID).Scan(
		&a.ID, &a.ShiftID, &a.NozzleID, &a.AttendantID, &a.AssignedAt, &a.ConfirmedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssignmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
