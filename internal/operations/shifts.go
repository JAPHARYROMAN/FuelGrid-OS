package operations

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type Shift struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	StationID      uuid.UUID
	OperatingDayID uuid.UUID
	Name           string
	Status         string
	OpenedBy       uuid.UUID
	OpenedAt       time.Time
	ClosedBy       *uuid.UUID
	ClosedAt       *time.Time
	ApprovedBy     *uuid.UUID
	ApprovedAt     *time.Time
	Notes          *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Attendant struct {
	ShiftID    uuid.UUID
	UserID     uuid.UUID
	AssignedBy uuid.UUID
	AssignedAt time.Time
}

type NozzleAssignment struct {
	ID          uuid.UUID
	ShiftID     uuid.UUID
	NozzleID    uuid.UUID
	AttendantID uuid.UUID
	AssignedAt  time.Time
}

var (
	ErrShiftNotFound      = errors.New("operations: shift not found")
	ErrAssignmentNotFound = errors.New("operations: assignment not found")
)

const shiftColumns = `
    id, tenant_id, station_id, operating_day_id, name, status,
    opened_by, opened_at, closed_by, closed_at, approved_by, approved_at,
    notes, created_at, updated_at
`

func scanShift(row pgx.Row, s *Shift) error {
	return row.Scan(
		&s.ID, &s.TenantID, &s.StationID, &s.OperatingDayID, &s.Name, &s.Status,
		&s.OpenedBy, &s.OpenedAt, &s.ClosedBy, &s.ClosedAt, &s.ApprovedBy, &s.ApprovedAt,
		&s.Notes, &s.CreatedAt, &s.UpdatedAt,
	)
}

// ListShifts returns a station's shifts, optionally filtered to one day,
// newest opened first.
func (r *Repo) ListShifts(ctx context.Context, tenantID, stationID uuid.UUID, dayID *uuid.UUID) ([]Shift, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+shiftColumns+`
		FROM shifts
		WHERE tenant_id = $1 AND station_id = $2
		  AND ($3::uuid IS NULL OR operating_day_id = $3)
		ORDER BY opened_at DESC
	`, tenantID, stationID, dayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Shift
	for rows.Next() {
		var s Shift
		if err := scanShift(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListOpenShiftsForStation returns the station's currently-open shifts (for
// the dashboard strip).
func (r *Repo) ListOpenShiftsForStation(ctx context.Context, tenantID, stationID uuid.UUID) ([]Shift, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+shiftColumns+`
		FROM shifts
		WHERE tenant_id = $1 AND station_id = $2 AND status = 'open'
		ORDER BY opened_at DESC
	`, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Shift
	for rows.Next() {
		var s Shift
		if err := scanShift(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repo) GetShift(ctx context.Context, tenantID, id uuid.UUID) (*Shift, error) {
	var s Shift
	if err := scanShift(r.pool.QueryRow(ctx, `
		SELECT `+shiftColumns+` FROM shifts WHERE id = $1 AND tenant_id = $2
	`, id, tenantID), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repo) OpenShift(ctx context.Context, tx pgx.Tx, tenantID, stationID, dayID, openedBy uuid.UUID, name string, notes *string) (*Shift, error) {
	var s Shift
	if err := scanShift(tx.QueryRow(ctx, `
		INSERT INTO shifts (tenant_id, station_id, operating_day_id, name, opened_by, notes)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+shiftColumns,
		tenantID, stationID, dayID, name, openedBy, notes,
	), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// CloseShift flips an open shift to closed, stamping closed_by/at.
func (r *Repo) CloseShift(ctx context.Context, tx pgx.Tx, tenantID, id, actorID uuid.UUID) (*Shift, error) {
	var s Shift
	err := scanShift(tx.QueryRow(ctx, `
		UPDATE shifts SET status = 'closed', closed_by = $3, closed_at = now()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+shiftColumns,
		id, tenantID, actorID,
	), &s)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrShiftNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ApproveShift flips a closed shift to approved, stamping approved_by/at.
func (r *Repo) ApproveShift(ctx context.Context, tx pgx.Tx, tenantID, id, actorID uuid.UUID) (*Shift, error) {
	var s Shift
	err := scanShift(tx.QueryRow(ctx, `
		UPDATE shifts SET status = 'approved', approved_by = $3, approved_at = now()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+shiftColumns,
		id, tenantID, actorID,
	), &s)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrShiftNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// OpenShiftCountForDay counts shifts in a day that are still open — the guard
// for closing an operating day. It runs through any Querier so the day-close
// handler can count inside the same tx that holds FOR UPDATE on the day row,
// closing the TOCTOU where a shift is opened between the count and the close.
func (r *Repo) OpenShiftCountForDay(ctx context.Context, q database.Querier, tenantID, dayID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM shifts
		WHERE tenant_id = $1 AND operating_day_id = $2 AND status = 'open'
	`, tenantID, dayID).Scan(&n)
	return n, err
}

// UnapprovedShiftCountForDay counts shifts in a day not yet approved — the
// guard for locking an operating day.
func (r *Repo) UnapprovedShiftCountForDay(ctx context.Context, tenantID, dayID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM shifts
		WHERE tenant_id = $1 AND operating_day_id = $2 AND status <> 'approved'
	`, tenantID, dayID).Scan(&n)
	return n, err
}

// --- attendants ---

func (r *Repo) ListAttendants(ctx context.Context, tenantID, shiftID uuid.UUID) ([]Attendant, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT shift_id, user_id, assigned_by, assigned_at
		FROM shift_attendants WHERE tenant_id = $1 AND shift_id = $2
		ORDER BY assigned_at
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attendant
	for rows.Next() {
		var a Attendant
		if err := rows.Scan(&a.ShiftID, &a.UserID, &a.AssignedBy, &a.AssignedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AttendantSummary is a shift attendant with their display labels, for the
// supervisor operations dashboard (who is on which shift).
type AttendantSummary struct {
	UserID   uuid.UUID
	FullName string
	Email    string
}

// AttendantSummariesForShift returns the shift's attendants joined to their
// user record, so the dashboard can name them without a separate lookup.
func (r *Repo) AttendantSummariesForShift(ctx context.Context, tenantID, shiftID uuid.UUID) ([]AttendantSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.full_name, u.email
		FROM shift_attendants a
		JOIN users u ON u.id = a.user_id
		WHERE a.tenant_id = $1 AND a.shift_id = $2
		ORDER BY u.full_name
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AttendantSummary
	for rows.Next() {
		var a AttendantSummary
		if err := rows.Scan(&a.UserID, &a.FullName, &a.Email); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repo) AssignAttendant(ctx context.Context, tx pgx.Tx, tenantID, shiftID, userID, assignedBy uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO shift_attendants (shift_id, user_id, tenant_id, assigned_by)
		VALUES ($1, $2, $3, $4)
	`, shiftID, userID, tenantID, assignedBy)
	return err
}

func (r *Repo) UnassignAttendant(ctx context.Context, tx pgx.Tx, tenantID, shiftID, userID uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		DELETE FROM shift_attendants WHERE tenant_id = $1 AND shift_id = $2 AND user_id = $3
	`, tenantID, shiftID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAssignmentNotFound
	}
	return nil
}

// IsAttendantOnShift reports whether a user is assigned to the shift.
func (r *Repo) IsAttendantOnShift(ctx context.Context, tenantID, shiftID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM shift_attendants
			WHERE tenant_id = $1 AND shift_id = $2 AND user_id = $3
		)
	`, tenantID, shiftID, userID).Scan(&exists)
	return exists, err
}

// NozzleAssignedOnShift reports whether the nozzle is assigned on the shift.
// When attendantID is non-nil, it further requires the assignment to be to
// that attendant — the self-scope check for attendant meter writes.
func (r *Repo) NozzleAssignedOnShift(ctx context.Context, tenantID, shiftID, nozzleID uuid.UUID, attendantID *uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM shift_nozzle_assignments
			WHERE tenant_id = $1 AND shift_id = $2 AND nozzle_id = $3
			  AND ($4::uuid IS NULL OR attendant_id = $4)
		)
	`, tenantID, shiftID, nozzleID, attendantID).Scan(&exists)
	return exists, err
}

// TankAssignedOnShift reports whether any nozzle drawing from the tank is
// assigned on the shift. When attendantID is non-nil, it requires at least
// one such nozzle to be assigned to that attendant — the self-scope check for
// attendant dip writes.
func (r *Repo) TankAssignedOnShift(ctx context.Context, tenantID, shiftID, tankID uuid.UUID, attendantID *uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM shift_nozzle_assignments sna
			JOIN nozzles n ON n.id = sna.nozzle_id
			WHERE sna.tenant_id = $1 AND sna.shift_id = $2 AND n.tank_id = $3
			  AND ($4::uuid IS NULL OR sna.attendant_id = $4)
		)
	`, tenantID, shiftID, tankID, attendantID).Scan(&exists)
	return exists, err
}

// --- nozzle assignments ---

func (r *Repo) ListNozzleAssignments(ctx context.Context, tenantID, shiftID uuid.UUID) ([]NozzleAssignment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, shift_id, nozzle_id, attendant_id, assigned_at
		FROM shift_nozzle_assignments WHERE tenant_id = $1 AND shift_id = $2
		ORDER BY assigned_at
	`, tenantID, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NozzleAssignment
	for rows.Next() {
		var n NozzleAssignment
		if err := rows.Scan(&n.ID, &n.ShiftID, &n.NozzleID, &n.AttendantID, &n.AssignedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *Repo) AssignNozzle(ctx context.Context, tx pgx.Tx, tenantID, stationID, shiftID, nozzleID, attendantID, assignedBy uuid.UUID) (*NozzleAssignment, error) {
	var n NozzleAssignment
	if err := tx.QueryRow(ctx, `
		INSERT INTO shift_nozzle_assignments (tenant_id, station_id, shift_id, nozzle_id, attendant_id, assigned_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, shift_id, nozzle_id, attendant_id, assigned_at
	`, tenantID, stationID, shiftID, nozzleID, attendantID, assignedBy).Scan(
		&n.ID, &n.ShiftID, &n.NozzleID, &n.AttendantID, &n.AssignedAt,
	); err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *Repo) UnassignNozzle(ctx context.Context, tx pgx.Tx, tenantID, shiftID, assignmentID uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		DELETE FROM shift_nozzle_assignments
		WHERE tenant_id = $1 AND shift_id = $2 AND id = $3
	`, tenantID, shiftID, assignmentID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAssignmentNotFound
	}
	return nil
}
