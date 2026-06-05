package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/workforce"
)

// ---- DTOs ------------------------------------------------------------------

type employeeDTO struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	StationID    uuid.UUID  `json:"station_id"`
	UserID       *uuid.UUID `json:"user_id,omitempty"`
	FullName     string     `json:"full_name"`
	Role         string     `json:"role"`
	EmployeeCode *string    `json:"employee_code,omitempty"`
	Phone        *string    `json:"phone,omitempty"`
	Email        *string    `json:"email,omitempty"`
	Status       string     `json:"status"`
	TeamID       *uuid.UUID `json:"team_id,omitempty"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
}

type employeeRoleDTO struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	IsDefault bool      `json:"is_default"`
	Status    string    `json:"status"`
	SortOrder int       `json:"sort_order"`
	CreatedAt string    `json:"created_at"`
	UpdatedAt string    `json:"updated_at"`
}

func toEmployeeRoleDTO(r *workforce.EmployeeRole) employeeRoleDTO {
	return employeeRoleDTO{
		ID: r.ID, TenantID: r.TenantID, Code: r.Code, Name: r.Name,
		IsDefault: r.IsDefault, Status: r.Status, SortOrder: r.SortOrder,
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
		UpdatedAt: r.UpdatedAt.Format(time.RFC3339),
	}
}

func toEmployeeDTO(e *workforce.Employee) employeeDTO {
	return employeeDTO{
		ID: e.ID, TenantID: e.TenantID, StationID: e.StationID, UserID: e.UserID,
		FullName: e.FullName, Role: e.Role, EmployeeCode: e.EmployeeCode,
		Phone: e.Phone, Email: e.Email, Status: e.Status, TeamID: e.TeamID,
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
}

type teamDTO struct {
	ID            uuid.UUID `json:"id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	StationID     uuid.UUID `json:"station_id"`
	Name          string    `json:"name"`
	RotationOrder int       `json:"rotation_order"`
	MemberCount   int       `json:"member_count"`
}

func toTeamDTO(t *workforce.Team) teamDTO {
	return teamDTO{
		ID: t.ID, TenantID: t.TenantID, StationID: t.StationID,
		Name: t.Name, RotationOrder: t.RotationOrder, MemberCount: t.MemberCount,
	}
}

type scheduledTeamDTO struct {
	Date    string        `json:"date"`
	Slot    string        `json:"slot"`
	Team    *teamDTO      `json:"team"`
	Members []employeeDTO `json:"members"`
}

type dayRosterDTO struct {
	Date        string   `json:"date"`
	MorningTeam *teamDTO `json:"morning_team"`
	EveningTeam *teamDTO `json:"evening_team"`
	RestingTeam *teamDTO `json:"resting_team"`
}

func toTeamDTOPtr(t *workforce.Team) *teamDTO {
	if t == nil {
		return nil
	}
	d := toTeamDTO(t)
	return &d
}

var (
	employeeRoleCodePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,63}$`)
	employeeRoleCodeSeparators = regexp.MustCompile(`[^a-z0-9]+`)
)

func normalizeEmployeeRoleName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

func normalizeEmployeeRoleCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	code = employeeRoleCodeSeparators.ReplaceAllString(code, "_")
	code = strings.Trim(code, "_")
	if len(code) > 64 {
		code = strings.TrimRight(code[:64], "_")
	}
	return code
}

// ---- Employee roles --------------------------------------------------------

func (s *Server) handleListEmployeeRoles(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	roles, err := s.workforce.ListEmployeeRoles(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("list employee roles", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]employeeRoleDTO, 0, len(roles))
	for i := range roles {
		out = append(out, toEmployeeRoleDTO(&roles[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

type createEmployeeRoleRequest struct {
	Name string `json:"name"`
	Code string `json:"code,omitempty"`
}

func (s *Server) handleCreateEmployeeRole(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createEmployeeRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name := normalizeEmployeeRoleName(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	codeSource := req.Code
	if codeSource == "" {
		codeSource = name
	}
	code := normalizeEmployeeRoleCode(codeSource)
	if !employeeRoleCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, "role code must start with a letter or number")
		return
	}

	role, err := s.workforce.CreateEmployeeRole(r.Context(), actor.TenantID, code, name)
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "employee role already exists")
		return
	}
	if err != nil {
		s.logger.Error("create employee role", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	dto := toEmployeeRoleDTO(&role)
	s.auditWorkforce(r.Context(), audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "employee_role.created", EventType: "EmployeeRoleCreated",
		EntityType: "employee_role", EntityID: role.ID.String(),
		NewValue: dto,
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(r.Context()),
	})
	writeJSON(w, http.StatusCreated, dto)
}

// ---- Employees -------------------------------------------------------------

func (s *Server) handleListEmployees(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", stationID) {
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.workforce.ListEmployeesPage(r.Context(), actor.TenantID, stationID, limit+1, offset)
	if err != nil {
		s.logger.Error("list employees", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]employeeDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toEmployeeDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

type createEmployeeRequest struct {
	UserID       *uuid.UUID `json:"user_id,omitempty"`
	FullName     string     `json:"full_name"`
	Role         string     `json:"role,omitempty"`
	EmployeeCode *string    `json:"employee_code,omitempty"`
	Phone        *string    `json:"phone,omitempty"`
	Email        *string    `json:"email,omitempty"`
}

func (s *Server) handleCreateEmployee(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	var req createEmployeeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.FullName == "" {
		writeError(w, http.StatusBadRequest, "full_name is required")
		return
	}

	ctx := r.Context()
	role := req.Role
	if role == "" {
		role = "pump_attendant"
	}
	ok, err := s.workforce.EmployeeRoleExists(ctx, actor.TenantID, role)
	if err != nil {
		s.logger.Error("validate employee role", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "employee role does not exist")
		return
	}

	// The route gates station.manage tenant-wide; confirm the station exists in
	// the tenant for a clean 404.
	if _, err := s.stations.Get(ctx, actor.TenantID, stationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "station not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	emp, err := s.workforce.CreateEmployee(ctx, actor.TenantID, workforce.CreateEmployeeInput{
		StationID: stationID, UserID: req.UserID, FullName: req.FullName, Role: role,
		EmployeeCode: req.EmployeeCode, Phone: req.Phone, Email: req.Email,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "an employee is already linked to that user")
		return
	}
	if isForeignKeyViolation(err) {
		writeError(w, http.StatusBadRequest, "linked user not found in this tenant")
		return
	}
	if err != nil {
		s.logger.Error("create employee", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.auditWorkforce(ctx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "employee.created", EventType: "EmployeeCreated",
		EntityType: "employee", EntityID: emp.ID.String(),
		NewValue: toEmployeeDTO(&emp),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	})
	writeJSON(w, http.StatusCreated, toEmployeeDTO(&emp))
}

type updateEmployeeRequest struct {
	FullName     *string    `json:"full_name,omitempty"`
	Role         *string    `json:"role,omitempty"`
	EmployeeCode *string    `json:"employee_code,omitempty"`
	Phone        *string    `json:"phone,omitempty"`
	Email        *string    `json:"email,omitempty"`
	Status       *string    `json:"status,omitempty"`
	UserID       *uuid.UUID `json:"user_id,omitempty"`
}

func (s *Server) handleUpdateEmployee(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "employeeID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid employee id")
		return
	}
	var req updateEmployeeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Status != nil && *req.Status != "active" && *req.Status != "inactive" {
		writeError(w, http.StatusBadRequest, "status must be active or inactive")
		return
	}

	ctx := r.Context()
	if req.Role != nil {
		ok, err := s.workforce.EmployeeRoleExists(ctx, actor.TenantID, *req.Role)
		if err != nil {
			s.logger.Error("validate employee role", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !ok {
			writeError(w, http.StatusBadRequest, "employee role does not exist")
			return
		}
	}
	before, err := s.workforce.GetEmployee(ctx, actor.TenantID, id)
	if errors.Is(err, workforce.ErrNotFound) {
		writeError(w, http.StatusNotFound, "employee not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	after, err := s.workforce.UpdateEmployee(ctx, actor.TenantID, id, workforce.UpdateEmployeeInput{
		FullName: req.FullName, Role: req.Role, EmployeeCode: req.EmployeeCode,
		Phone: req.Phone, Email: req.Email, Status: req.Status, UserID: req.UserID,
	})
	if errors.Is(err, workforce.ErrNotFound) {
		writeError(w, http.StatusNotFound, "employee not found")
		return
	}
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "an employee is already linked to that user")
		return
	}
	if isForeignKeyViolation(err) {
		writeError(w, http.StatusBadRequest, "linked user not found in this tenant")
		return
	}
	if err != nil {
		s.logger.Error("update employee", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.auditWorkforce(ctx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "employee.updated", EventType: "EmployeeUpdated",
		EntityType: "employee", EntityID: after.ID.String(),
		PreviousValue: toEmployeeDTO(&before), NewValue: toEmployeeDTO(&after),
		IP: clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	})
	writeJSON(w, http.StatusOK, toEmployeeDTO(&after))
}

// ---- Teams -----------------------------------------------------------------

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", stationID) {
		return
	}
	teams, err := s.workforce.ListTeams(r.Context(), actor.TenantID, stationID)
	if err != nil {
		s.logger.Error("list teams", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]teamDTO, 0, len(teams))
	for i := range teams {
		out = append(out, toTeamDTO(&teams[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

type ensureTeamsRequest struct {
	// Names is the optional ordered (0,1,2) team-name list; missing entries
	// fall back to defaults.
	Names []string `json:"names,omitempty"`
}

func (s *Server) handleEnsureTeams(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	var req ensureTeamsRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	ctx := r.Context()
	if _, err := s.stations.Get(ctx, actor.TenantID, stationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "station not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	teams, err := s.workforce.EnsureTeams(ctx, actor.TenantID, stationID, req.Names)
	if err != nil {
		s.logger.Error("ensure teams", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]teamDTO, 0, len(teams))
	for i := range teams {
		out = append(out, toTeamDTO(&teams[i]))
	}
	s.auditWorkforce(ctx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift_teams.ensured", EventType: "ShiftTeamsEnsured",
		EntityType: "station", EntityID: stationID.String(),
		NewValue: out,
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

type setTeamMembersRequest struct {
	EmployeeIDs []uuid.UUID `json:"employee_ids"`
}

func (s *Server) handleSetTeamMembers(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	teamID, err := uuid.Parse(chi.URLParam(r, "teamID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id")
		return
	}
	var req setTeamMembersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx := r.Context()
	// Confirm the team exists in the tenant for a clean 404.
	team, err := s.workforce.GetTeam(ctx, actor.TenantID, teamID)
	if errors.Is(err, workforce.ErrNotFound) {
		writeError(w, http.StatusNotFound, "team not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := s.workforce.SetTeamMembers(ctx, actor.TenantID, teamID, req.EmployeeIDs); err != nil {
		if isForeignKeyViolation(err) {
			writeError(w, http.StatusBadRequest, "one or more employees not found in this tenant")
			return
		}
		s.logger.Error("set team members", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	members, err := s.workforce.TeamMembers(ctx, actor.TenantID, teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]employeeDTO, 0, len(members))
	for i := range members {
		out = append(out, toEmployeeDTO(&members[i]))
	}
	s.auditWorkforce(ctx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift_team.members_set", EventType: "ShiftTeamMembersSet",
		EntityType: "shift_team", EntityID: teamID.String(),
		NewValue: map[string]any{"team_id": teamID, "station_id": team.StationID, "members": out},
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// ---- Rotation anchor -------------------------------------------------------

type rotationAnchorDTO struct {
	StationID uuid.UUID `json:"station_id"`
	Anchor    *string   `json:"rotation_anchor_date"`
}

func (s *Server) handleGetRotationAnchor(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", stationID) {
		return
	}
	anchor, err := s.workforce.RotationAnchor(r.Context(), actor.TenantID, stationID)
	if errors.Is(err, workforce.ErrNotFound) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, rotationAnchorDTO{StationID: stationID, Anchor: fmtDate(anchor)})
}

type setRotationAnchorRequest struct {
	// Anchor is the YYYY-MM-DD cycle-day-0 date, or null/empty to clear it.
	Anchor *string `json:"rotation_anchor_date"`
}

func (s *Server) handleSetRotationAnchor(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	var req setRotationAnchorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	anchor, err := parseDate(req.Anchor)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rotation_anchor_date (want YYYY-MM-DD)")
		return
	}

	ctx := r.Context()
	if err := s.workforce.SetRotationAnchor(ctx, actor.TenantID, stationID, anchor); err != nil {
		if errors.Is(err, workforce.ErrNotFound) {
			writeError(w, http.StatusNotFound, "station not found")
			return
		}
		s.logger.Error("set rotation anchor", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.auditWorkforce(ctx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "station.rotation_anchor_set", EventType: "RotationAnchorSet",
		EntityType: "station", EntityID: stationID.String(),
		NewValue: rotationAnchorDTO{StationID: stationID, Anchor: fmtDate(anchor)},
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	})
	writeJSON(w, http.StatusOK, rotationAnchorDTO{StationID: stationID, Anchor: fmtDate(anchor)})
}

// ---- Roster & scheduled team -----------------------------------------------

func (s *Server) handleRoster(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", stationID) {
		return
	}
	from := parseDateParam(r, "from", time.Now().UTC())
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		n, perr := parsePositiveInt(v)
		if perr != nil || n < 1 || n > 60 {
			writeError(w, http.StatusBadRequest, "days must be an integer between 1 and 60")
			return
		}
		days = n
	}
	roster, err := s.workforce.RosterPreview(r.Context(), actor.TenantID, stationID, from, days)
	if errors.Is(err, workforce.ErrNotFound) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		s.logger.Error("roster preview", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]dayRosterDTO, 0, len(roster))
	for i := range roster {
		out = append(out, dayRosterDTO{
			Date:        roster[i].Date.Format(dateLayout),
			MorningTeam: toTeamDTOPtr(roster[i].MorningTeam),
			EveningTeam: toTeamDTOPtr(roster[i].EveningTeam),
			RestingTeam: toTeamDTOPtr(roster[i].RestingTeam),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleScheduledTeam(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", stationID) {
		return
	}
	slot := workforce.Slot(r.URL.Query().Get("slot"))
	if !slot.Valid() {
		writeError(w, http.StatusBadRequest, "slot must be morning or evening")
		return
	}
	day := parseDateParam(r, "date", time.Now().UTC())

	sched, err := s.workforce.ScheduledTeamFor(r.Context(), actor.TenantID, stationID, day, slot)
	if errors.Is(err, workforce.ErrNotFound) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		s.logger.Error("scheduled team", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toScheduledTeamDTO(sched))
}

// auditWorkforce writes a workforce audit + outbox record in its own short
// transaction. The primary workforce mutation has already committed via the
// repo's own pool call, so a failure here is logged but never fails the
// request — the user-visible action already succeeded.
func (s *Server) auditWorkforce(ctx context.Context, rec audit.TxRecord) {
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		s.logger.Error("workforce audit: begin", "error", err, "action", rec.Action)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := audit.WriteWithOutbox(ctx, tx, rec); err != nil {
		s.logger.Error("workforce audit: write", "error", err, "action", rec.Action)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger.Error("workforce audit: commit", "error", err, "action", rec.Action)
	}
}

// parsePositiveInt parses a base-10 integer query value.
func parsePositiveInt(s string) (int, error) {
	return strconv.Atoi(s)
}

func toScheduledTeamDTO(sched *workforce.ScheduledTeam) scheduledTeamDTO {
	members := make([]employeeDTO, 0, len(sched.Members))
	for i := range sched.Members {
		members = append(members, toEmployeeDTO(&sched.Members[i]))
	}
	return scheduledTeamDTO{
		Date:    sched.Date.Format(dateLayout),
		Slot:    string(sched.Slot),
		Team:    toTeamDTOPtr(sched.Team),
		Members: members,
	}
}
