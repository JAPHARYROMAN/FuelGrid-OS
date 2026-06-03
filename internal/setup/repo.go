// Package setup owns the persisted setup checklist and readiness projection.
package setup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

const (
	StatusPending   = "pending"
	StatusCompleted = "completed"
	StatusSkipped   = "skipped"
)

var (
	ErrInvalidStep   = errors.New("setup: invalid step")
	ErrInvalidStatus = errors.New("setup: invalid status")
)

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

type StepDefinition struct {
	Code        string
	Title       string
	Description string
	Href        string
	CTA         string
	Required    bool
}

type StepState struct {
	Code        string
	Status      string
	CompletedAt *time.Time
	CompletedBy *uuid.UUID
	UpdatedAt   time.Time
	Notes       *string
}

type Step struct {
	StepDefinition
	Status        string
	Ready         bool
	Blocked       bool
	BlockedReason *string
	Count         int
	RequiredCount int
	CompletedAt   *time.Time
	CompletedBy   *uuid.UUID
	UpdatedAt     *time.Time
	Notes         *string
}

type Checklist struct {
	Steps              []Step
	RequiredTotal      int
	RequiredReady      int
	RequiredCompleted  int
	OperationallyReady bool
	Blocked            []Blocker
}

type Blocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Counts struct {
	Companies         int
	Regions           int
	Stations          int
	Products          int
	Suppliers         int
	Tanks             int
	Pumps             int
	Nozzles           int
	OpenedTanks       int
	Employees         int
	LoginEmployees    int
	Teams             int
	RotationAnchors   int
	Users             int
	RequiredTeams     int
	RequiredRotations int
	RequiredOpenTanks int
}

var stepDefinitions = []StepDefinition{
	{
		Code:        "company",
		Title:       "Company",
		Description: "Your operating entity, currency, timezone, and legal identity.",
		Href:        "/settings/companies",
		CTA:         "Add company",
		Required:    true,
	},
	{
		Code:        "regions",
		Title:       "Regions",
		Description: "Group stations geographically for reporting and management.",
		Href:        "/settings/regions",
		CTA:         "Add regions",
		Required:    true,
	},
	{
		Code:        "stations",
		Title:       "Stations",
		Description: "Create the physical fuel sites that operations run from.",
		Href:        "/settings/stations",
		CTA:         "Add stations",
		Required:    true,
	},
	{
		Code:        "products",
		Title:       "Products",
		Description: "Set up fuel grades, default prices, taxes, and colors.",
		Href:        "/settings/products",
		CTA:         "Add products",
		Required:    true,
	},
	{
		Code:        "suppliers",
		Title:       "Suppliers",
		Description: "Create supplier records used by purchase orders and deliveries.",
		Href:        "/settings/suppliers",
		CTA:         "Add suppliers",
		Required:    false,
	},
	{
		Code:        "tanks",
		Title:       "Tanks",
		Description: "Install station tanks and bind each to a product.",
		Href:        "/settings/tanks",
		CTA:         "Add tanks",
		Required:    true,
	},
	{
		Code:        "pumps",
		Title:       "Pumps",
		Description: "Install dispensing units at the station.",
		Href:        "/settings/pumps",
		CTA:         "Add pumps",
		Required:    true,
	},
	{
		Code:        "nozzles",
		Title:       "Nozzles",
		Description: "Attach nozzles to pumps and the tanks they draw from.",
		Href:        "/settings/pumps",
		CTA:         "Add nozzles",
		Required:    true,
	},
	{
		Code:        "opening_stock",
		Title:       "Opening stock",
		Description: "Seed one opening balance for every station tank.",
		Href:        "/setup/opening-stock",
		CTA:         "Set opening stock",
		Required:    true,
	},
	{
		Code:        "employees",
		Title:       "Employees",
		Description: "Create station employees and link shift staff to login users.",
		Href:        "/settings/employees",
		CTA:         "Add employees",
		Required:    true,
	},
	{
		Code:        "teams",
		Title:       "Teams",
		Description: "Create the three-team rotation for each station.",
		Href:        "/settings/teams",
		CTA:         "Create teams",
		Required:    true,
	},
	{
		Code:        "rotation_anchor",
		Title:       "Rotation anchor",
		Description: "Set the date that starts each station's rotation cycle.",
		Href:        "/settings/teams",
		CTA:         "Set anchor",
		Required:    true,
	},
	{
		Code:        "users",
		Title:       "Users",
		Description: "Invite the first active operators, supervisors, and managers.",
		Href:        "/settings/users",
		CTA:         "Invite users",
		Required:    true,
	},
}

func ValidStep(code string) bool {
	for _, def := range stepDefinitions {
		if def.Code == code {
			return true
		}
	}
	return false
}

func validStatus(status string) bool {
	switch status {
	case StatusPending, StatusCompleted, StatusSkipped:
		return true
	default:
		return false
	}
}

func (r *Repo) Checklist(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID) (Checklist, error) {
	counts, err := r.Counts(ctx, tenantID, stationIDs)
	if err != nil {
		return Checklist{}, err
	}
	states, err := r.stepStates(ctx, tenantID)
	if err != nil {
		return Checklist{}, err
	}
	return buildChecklist(counts, states), nil
}

func buildChecklist(counts Counts, states map[string]StepState) Checklist {
	out := Checklist{Steps: make([]Step, 0, len(stepDefinitions))}
	for _, def := range stepDefinitions {
		step := Step{
			StepDefinition: def,
			Status:         StatusPending,
		}
		step.Ready, step.Blocked, step.BlockedReason, step.Count, step.RequiredCount = readinessFor(def.Code, counts)
		if st, ok := states[def.Code]; ok {
			step.Status = st.Status
			step.CompletedAt = st.CompletedAt
			step.CompletedBy = st.CompletedBy
			step.UpdatedAt = &st.UpdatedAt
			step.Notes = st.Notes
		}
		if def.Required {
			out.RequiredTotal++
			if step.Ready {
				out.RequiredReady++
			} else {
				msg := fmt.Sprintf("%s is not ready", def.Title)
				if step.BlockedReason != nil {
					msg = *step.BlockedReason
				}
				out.Blocked = append(out.Blocked, Blocker{Code: def.Code, Message: msg})
			}
			if step.Status == StatusCompleted {
				out.RequiredCompleted++
			}
		}
		out.Steps = append(out.Steps, step)
	}
	out.OperationallyReady = out.RequiredReady == out.RequiredTotal
	return out
}

func readinessFor(code string, c Counts) (ready, blocked bool, reason *string, count, requiredCount int) {
	switch code {
	case "company":
		return c.Companies > 0, false, nil, c.Companies, 1
	case "regions":
		return c.Regions > 0, false, nil, c.Regions, 1
	case "stations":
		return c.Stations > 0, false, nil, c.Stations, 1
	case "products":
		return c.Products > 0, false, nil, c.Products, 1
	case "suppliers":
		return c.Suppliers > 0, false, nil, c.Suppliers, 1
	case "tanks":
		return readyWithStation(c.Stations, c.Tanks, "Create a station before installing tanks")
	case "pumps":
		return readyWithStation(c.Stations, c.Pumps, "Create a station before installing pumps")
	case "nozzles":
		if c.Pumps == 0 {
			msg := "Create a pump before adding nozzles"
			return false, true, &msg, c.Nozzles, 1
		}
		return readyWithStation(c.Stations, c.Nozzles, "Create a station before adding nozzles")
	case "opening_stock":
		if c.Tanks == 0 {
			msg := "Create tanks before setting opening stock"
			return false, true, &msg, c.OpenedTanks, c.RequiredOpenTanks
		}
		return c.OpenedTanks >= c.RequiredOpenTanks, false, nil, c.OpenedTanks, c.RequiredOpenTanks
	case "employees":
		return readyWithStation(c.Stations, c.Employees, "Create a station before adding employees")
	case "teams":
		if c.Employees == 0 {
			msg := "Create employees before forming shift teams"
			return false, true, &msg, c.Teams, c.RequiredTeams
		}
		return c.Teams >= c.RequiredTeams && c.RequiredTeams > 0, false, nil, c.Teams, c.RequiredTeams
	case "rotation_anchor":
		if c.Teams < c.RequiredTeams || c.RequiredTeams == 0 {
			msg := "Create each station's three shift teams before setting rotation anchors"
			return false, true, &msg, c.RotationAnchors, c.RequiredRotations
		}
		return c.RotationAnchors >= c.RequiredRotations && c.RequiredRotations > 0, false, nil, c.RotationAnchors, c.RequiredRotations
	case "users":
		return c.Users > 0 && c.LoginEmployees > 0, false, nil, c.Users, 1
	default:
		return false, true, nil, 0, 1
	}
}

func readyWithStation(stations, count int, blockedReason string) (bool, bool, *string, int, int) {
	// Every station-gated step requires at least one of the counted resource.
	const required = 1
	if stations == 0 {
		msg := blockedReason
		return false, true, &msg, count, required
	}
	return count >= required, false, nil, count, required
}

func (r *Repo) Counts(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID) (Counts, error) {
	var c Counts
	err := r.pool.QueryRow(ctx, `
		WITH visible_stations AS (
			SELECT id, rotation_anchor_date
			FROM stations
			WHERE tenant_id = $1
			  AND status <> 'deleted'
			  AND ($2::uuid[] IS NULL OR id = ANY($2::uuid[]))
		),
		visible_tanks AS (
			SELECT id
			FROM tanks
			WHERE tenant_id = $1
			  AND status <> 'deleted'
			  AND station_id IN (SELECT id FROM visible_stations)
		)
		SELECT
			(SELECT count(*) FROM companies WHERE tenant_id = $1 AND status <> 'deleted'),
			(SELECT count(*) FROM regions WHERE tenant_id = $1 AND status <> 'deleted'),
			(SELECT count(*) FROM visible_stations),
			(SELECT count(*) FROM products WHERE tenant_id = $1 AND status <> 'deleted'),
			(SELECT count(*) FROM suppliers WHERE tenant_id = $1),
			(SELECT count(*) FROM visible_tanks),
			(SELECT count(*) FROM pumps WHERE tenant_id = $1 AND status <> 'deleted' AND station_id IN (SELECT id FROM visible_stations)),
			(SELECT count(*) FROM nozzles WHERE tenant_id = $1 AND status <> 'deleted' AND station_id IN (SELECT id FROM visible_stations)),
			(SELECT count(*) FROM visible_tanks t WHERE EXISTS (
				SELECT 1 FROM stock_movements sm
				WHERE sm.tenant_id = $1
				  AND sm.tank_id = t.id
				  AND sm.movement_type = 'opening'
				  AND sm.status = 'posted'
				  AND (sm.source_ref_type IS NULL OR sm.source_ref_type <> 'correction')
			)),
			(SELECT count(*) FROM employees WHERE tenant_id = $1 AND status = 'active' AND station_id IN (SELECT id FROM visible_stations)),
			(SELECT count(*)
			 FROM employees e
			 JOIN users u ON u.tenant_id = e.tenant_id AND u.id = e.user_id AND u.status = 'active'
			 WHERE e.tenant_id = $1 AND e.status = 'active' AND e.station_id IN (SELECT id FROM visible_stations)),
			(SELECT count(*) FROM shift_teams WHERE tenant_id = $1 AND station_id IN (SELECT id FROM visible_stations)),
			(SELECT count(*) FROM visible_stations WHERE rotation_anchor_date IS NOT NULL),
			(SELECT count(*) FROM users WHERE tenant_id = $1 AND status = 'active')
	`, tenantID, database.UUIDStrings(stationIDs)).Scan(
		&c.Companies, &c.Regions, &c.Stations, &c.Products, &c.Suppliers,
		&c.Tanks, &c.Pumps, &c.Nozzles, &c.OpenedTanks,
		&c.Employees, &c.LoginEmployees, &c.Teams, &c.RotationAnchors, &c.Users,
	)
	if err != nil {
		return Counts{}, err
	}
	c.RequiredTeams = c.Stations * 3
	c.RequiredRotations = c.Stations
	c.RequiredOpenTanks = c.Tanks
	return c, nil
}

func (r *Repo) stepStates(ctx context.Context, tenantID uuid.UUID) (map[string]StepState, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT code, status, completed_at, completed_by, updated_at, notes
		FROM setup_steps
		WHERE tenant_id = $1
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]StepState{}
	for rows.Next() {
		var st StepState
		if err := rows.Scan(&st.Code, &st.Status, &st.CompletedAt, &st.CompletedBy, &st.UpdatedAt, &st.Notes); err != nil {
			return nil, err
		}
		out[st.Code] = st
	}
	return out, rows.Err()
}

func (r *Repo) UpsertStep(ctx context.Context, tx pgx.Tx, tenantID, actorID uuid.UUID, code, status string, notes *string) (StepState, error) {
	if !ValidStep(code) {
		return StepState{}, ErrInvalidStep
	}
	if !validStatus(status) {
		return StepState{}, ErrInvalidStatus
	}
	var st StepState
	err := tx.QueryRow(ctx, `
		INSERT INTO setup_steps (tenant_id, code, status, completed_by, completed_at, updated_by, notes)
		VALUES (
			$1, $2, $3,
			CASE WHEN $3 = 'completed' THEN $4 ELSE NULL END,
			CASE WHEN $3 = 'completed' THEN now() ELSE NULL END,
			$4, $5
		)
		ON CONFLICT (tenant_id, code) DO UPDATE SET
			status = EXCLUDED.status,
			completed_by = CASE WHEN EXCLUDED.status = 'completed' THEN EXCLUDED.updated_by ELSE NULL END,
			completed_at = CASE
				WHEN EXCLUDED.status = 'completed' THEN COALESCE(setup_steps.completed_at, now())
				ELSE NULL
			END,
			updated_by = EXCLUDED.updated_by,
			notes = EXCLUDED.notes
		RETURNING code, status, completed_at, completed_by, updated_at, notes
	`, tenantID, code, status, actorID, notes).Scan(
		&st.Code, &st.Status, &st.CompletedAt, &st.CompletedBy, &st.UpdatedAt, &st.Notes,
	)
	return st, err
}

// openShiftOperationalSteps is the minimal per-station operational set that must
// be ready before a shift can open at a station. It deliberately excludes
// tenant-wide checklist items (company, regions, products, suppliers, users) and
// the rotation/team configuration (teams, rotation_anchor): those are either not
// per-station prerequisites for running one shift, or are enforced separately by
// the open-shift handler (a shift never opens without its scheduled team and its
// login-linked members). What remains is the physical + stock + staffing
// readiness that genuinely makes a single shift runnable.
var openShiftOperationalSteps = map[string]bool{
	"tanks":         true,
	"pumps":         true,
	"nozzles":       true,
	"opening_stock": true,
	"employees":     true,
}

// OpenShiftBlockers reports the per-station operational prerequisites that are
// not yet satisfied for the given station, scoped to what makes a single shift
// runnable: the station must exist, be active, and have at least one tank, pump,
// nozzle, opening stock seeded for its tanks, and at least one active employee.
// It reuses the setup readiness projection but restricts the blocking set to
// those operational items — tenant-wide checklist items (regions, users, full
// team rotation, rotation anchor) are intentionally not blockers here.
func (r *Repo) OpenShiftBlockers(ctx context.Context, tenantID, stationID uuid.UUID) ([]Blocker, error) {
	var stationStatus string
	err := r.pool.QueryRow(ctx, `
		SELECT status
		FROM stations
		WHERE tenant_id = $1 AND id = $2 AND status <> 'deleted'
	`, tenantID, stationID).Scan(&stationStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return []Blocker{{Code: "stations", Message: "station not found or deleted"}}, nil
	}
	if err != nil {
		return nil, err
	}

	counts, err := r.Counts(ctx, tenantID, []uuid.UUID{stationID})
	if err != nil {
		return nil, err
	}
	all := buildChecklist(counts, nil)
	blockers := make([]Blocker, 0, len(all.Blocked))
	if stationStatus != "active" {
		blockers = append(blockers, Blocker{Code: "stations", Message: "station is not active"})
	}
	for _, b := range all.Blocked {
		if openShiftOperationalSteps[b.Code] {
			blockers = append(blockers, b)
		}
	}
	return blockers, nil
}
