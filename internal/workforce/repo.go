package workforce

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// ErrNotFound is returned when an employee or team does not exist (in the
// caller's tenant).
var ErrNotFound = errors.New("workforce: not found")

// Employee is a member of a station's workforce. UserID is set when the
// employee also has a login account (required for attendants who capture
// readings); back-office staff may have none. TeamID is the team they belong
// to, if any (derived from membership for convenience in lists).
type Employee struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	StationID    uuid.UUID
	UserID       *uuid.UUID
	FullName     string
	Role         string
	EmployeeCode *string
	Phone        *string
	Email        *string
	Status       string
	TeamID       *uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Team is one of a station's three rotation teams.
type Team struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	StationID     uuid.UUID
	Name          string
	RotationOrder int
	MemberCount   int
}

// ScheduledTeam is the team (and its members) on duty for a given date+slot.
// Team is nil when the station has no rotation configured (no anchor / teams).
type ScheduledTeam struct {
	Date    time.Time
	Slot    Slot
	Team    *Team
	Members []Employee
}

// DayRoster is one row of a forward-looking roster preview.
type DayRoster struct {
	Date        time.Time
	MorningTeam *Team
	EveningTeam *Team
	RestingTeam *Team
}

type Repo struct{ pool *database.Pool }

// New constructs the workforce repository.
func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

// ---- Employees ------------------------------------------------------------

// CreateEmployeeInput holds the fields needed to add an employee.
type CreateEmployeeInput struct {
	StationID    uuid.UUID
	UserID       *uuid.UUID
	FullName     string
	Role         string
	EmployeeCode *string
	Phone        *string
	Email        *string
}

const employeeColumns = `e.id, e.tenant_id, e.station_id, e.user_id, e.full_name, e.role,
	e.employee_code, e.phone, e.email, e.status,
	(SELECT m.team_id FROM shift_team_members m WHERE m.tenant_id = e.tenant_id AND m.employee_id = e.id) AS team_id,
	e.created_at, e.updated_at`

func scanEmployee(row pgx.Row) (Employee, error) {
	var e Employee
	err := row.Scan(&e.ID, &e.TenantID, &e.StationID, &e.UserID, &e.FullName, &e.Role,
		&e.EmployeeCode, &e.Phone, &e.Email, &e.Status, &e.TeamID, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

// ListEmployees returns a station's workforce, newest first.
func (r *Repo) ListEmployees(ctx context.Context, tenantID, stationID uuid.UUID) ([]Employee, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+employeeColumns+`
		FROM employees e
		WHERE e.tenant_id = $1 AND e.station_id = $2
		ORDER BY e.full_name ASC`, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Employee
	for rows.Next() {
		e, err := scanEmployee(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetEmployee loads one employee by id within the tenant.
func (r *Repo) GetEmployee(ctx context.Context, tenantID, id uuid.UUID) (Employee, error) {
	e, err := scanEmployee(r.pool.QueryRow(ctx, `
		SELECT `+employeeColumns+` FROM employees e
		WHERE e.tenant_id = $1 AND e.id = $2`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Employee{}, ErrNotFound
	}
	return e, err
}

// CreateEmployee inserts a new employee and returns it.
func (r *Repo) CreateEmployee(ctx context.Context, tenantID uuid.UUID, in CreateEmployeeInput) (Employee, error) {
	role := in.Role
	if role == "" {
		role = "pump_attendant"
	}
	var id uuid.UUID
	if err := r.pool.QueryRow(ctx, `
		INSERT INTO employees (tenant_id, station_id, user_id, full_name, role, employee_code, phone, email)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		tenantID, in.StationID, in.UserID, in.FullName, role, in.EmployeeCode, in.Phone, in.Email,
	).Scan(&id); err != nil {
		return Employee{}, err
	}
	return r.GetEmployee(ctx, tenantID, id)
}

// UpdateEmployeeInput carries the mutable employee fields. Nil fields are left
// unchanged.
type UpdateEmployeeInput struct {
	FullName     *string
	Role         *string
	EmployeeCode *string
	Phone        *string
	Email        *string
	Status       *string
	UserID       *uuid.UUID
}

// UpdateEmployee applies a partial update (COALESCE keeps unset fields).
func (r *Repo) UpdateEmployee(ctx context.Context, tenantID, id uuid.UUID, in UpdateEmployeeInput) (Employee, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE employees SET
			full_name     = COALESCE($3, full_name),
			role          = COALESCE($4, role),
			employee_code = COALESCE($5, employee_code),
			phone         = COALESCE($6, phone),
			email         = COALESCE($7, email),
			status        = COALESCE($8, status),
			user_id       = COALESCE($9, user_id)
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, in.FullName, in.Role, in.EmployeeCode, in.Phone, in.Email, in.Status, in.UserID)
	if err != nil {
		return Employee{}, err
	}
	if tag.RowsAffected() == 0 {
		return Employee{}, ErrNotFound
	}
	return r.GetEmployee(ctx, tenantID, id)
}

// ---- Teams ----------------------------------------------------------------

// ListTeams returns a station's teams (ordered by rotation_order) with member
// counts.
func (r *Repo) ListTeams(ctx context.Context, tenantID, stationID uuid.UUID) ([]Team, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.tenant_id, t.station_id, t.name, t.rotation_order,
			(SELECT count(*) FROM shift_team_members m WHERE m.tenant_id = t.tenant_id AND m.team_id = t.id)
		FROM shift_teams t
		WHERE t.tenant_id = $1 AND t.station_id = $2
		ORDER BY t.rotation_order ASC`, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Team
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.TenantID, &t.StationID, &t.Name, &t.RotationOrder, &t.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTeam loads one team by id within the tenant (with its member count).
func (r *Repo) GetTeam(ctx context.Context, tenantID, id uuid.UUID) (Team, error) {
	var t Team
	err := r.pool.QueryRow(ctx, `
		SELECT t.id, t.tenant_id, t.station_id, t.name, t.rotation_order,
			(SELECT count(*) FROM shift_team_members m WHERE m.tenant_id = t.tenant_id AND m.team_id = t.id)
		FROM shift_teams t
		WHERE t.tenant_id = $1 AND t.id = $2`, tenantID, id).
		Scan(&t.ID, &t.TenantID, &t.StationID, &t.Name, &t.RotationOrder, &t.MemberCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return Team{}, ErrNotFound
	}
	return t, err
}

// EnsureTeams guarantees the station has its three rotation teams (orders
// 0,1,2). Missing teams are created with the provided names (falling back to
// "Team A/B/C"); existing teams are left as-is. Returns all three, ordered.
func (r *Repo) EnsureTeams(ctx context.Context, tenantID, stationID uuid.UUID, names []string) ([]Team, error) {
	defaults := []string{"Team A", "Team B", "Team C"}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for order := 0; order < 3; order++ {
		name := defaults[order]
		if order < len(names) && names[order] != "" {
			name = names[order]
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO shift_teams (tenant_id, station_id, name, rotation_order)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (tenant_id, station_id, rotation_order) DO UPDATE SET name = EXCLUDED.name`,
			tenantID, stationID, name, order); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.ListTeams(ctx, tenantID, stationID)
}

// SetTeamMembers replaces a team's membership with the given employees. Because
// an employee can be on only one team, any prior membership of these employees
// (on any team) is cleared first.
func (r *Repo) SetTeamMembers(ctx context.Context, tenantID, teamID uuid.UUID, employeeIDs []uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Clear this team's current members.
	if _, err := tx.Exec(ctx, `DELETE FROM shift_team_members WHERE tenant_id = $1 AND team_id = $2`,
		tenantID, teamID); err != nil {
		return err
	}
	for _, empID := range employeeIDs {
		// Move the employee off any other team, then add to this one.
		if _, err := tx.Exec(ctx, `DELETE FROM shift_team_members WHERE tenant_id = $1 AND employee_id = $2`,
			tenantID, empID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO shift_team_members (tenant_id, team_id, employee_id)
			VALUES ($1, $2, $3)`, tenantID, teamID, empID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// TeamMembers returns the employees on a team.
func (r *Repo) TeamMembers(ctx context.Context, tenantID, teamID uuid.UUID) ([]Employee, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+employeeColumns+`
		FROM shift_team_members m
		JOIN employees e ON e.tenant_id = m.tenant_id AND e.id = m.employee_id
		WHERE m.tenant_id = $1 AND m.team_id = $2
		ORDER BY e.full_name ASC`, tenantID, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Employee
	for rows.Next() {
		e, err := scanEmployee(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- Rotation -------------------------------------------------------------

// RotationAnchor returns the station's rotation anchor date, or nil when the
// station has not started its rotation yet.
func (r *Repo) RotationAnchor(ctx context.Context, tenantID, stationID uuid.UUID) (*time.Time, error) {
	var anchor *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT rotation_anchor_date FROM stations WHERE tenant_id = $1 AND id = $2`,
		tenantID, stationID).Scan(&anchor)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return anchor, err
}

// SetRotationAnchor sets (or clears, when anchor is nil) the station's
// rotation anchor date — cycle day 0 of the 3-team rotation.
func (r *Repo) SetRotationAnchor(ctx context.Context, tenantID, stationID uuid.UUID, anchor *time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE stations SET rotation_anchor_date = $3 WHERE tenant_id = $1 AND id = $2`,
		tenantID, stationID, anchor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ScheduledTeamFor resolves the team (and its members) on duty for a station on
// the given date+slot. Team is nil when the station has no anchor or the
// matching team row is missing.
func (r *Repo) ScheduledTeamFor(ctx context.Context, tenantID, stationID uuid.UUID, day time.Time, slot Slot) (*ScheduledTeam, error) {
	anchor, err := r.RotationAnchor(ctx, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	out := &ScheduledTeam{Date: day, Slot: slot}
	if anchor == nil {
		return out, nil // rotation not configured
	}
	order := Rotation(*anchor, day).OrderForSlot(slot)

	var t Team
	err = r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, station_id, name, rotation_order
		FROM shift_teams WHERE tenant_id = $1 AND station_id = $2 AND rotation_order = $3`,
		tenantID, stationID, order).Scan(&t.ID, &t.TenantID, &t.StationID, &t.Name, &t.RotationOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil // teams not set up yet
	}
	if err != nil {
		return nil, err
	}
	members, err := r.TeamMembers(ctx, tenantID, t.ID)
	if err != nil {
		return nil, err
	}
	t.MemberCount = len(members)
	out.Team = &t
	out.Members = members
	return out, nil
}

// RosterPreview returns the rotation for `days` calendar days starting at
// `from`. Teams are nil for any day where the station has no anchor/teams.
func (r *Repo) RosterPreview(ctx context.Context, tenantID, stationID uuid.UUID, from time.Time, days int) ([]DayRoster, error) {
	teams, err := r.ListTeams(ctx, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	byOrder := map[int]*Team{}
	for i := range teams {
		byOrder[teams[i].RotationOrder] = &teams[i]
	}
	anchor, err := r.RotationAnchor(ctx, tenantID, stationID)
	if err != nil {
		return nil, err
	}

	out := make([]DayRoster, 0, days)
	for i := 0; i < days; i++ {
		d := from.AddDate(0, 0, i)
		row := DayRoster{Date: d}
		if anchor != nil && len(byOrder) == 3 {
			rot := Rotation(*anchor, d)
			row.MorningTeam = byOrder[rot.MorningOrder]
			row.EveningTeam = byOrder[rot.EveningOrder]
			row.RestingTeam = byOrder[rot.RestOrder]
		}
		out = append(out, row)
	}
	return out, nil
}
