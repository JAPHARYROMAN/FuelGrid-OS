package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
	"github.com/japharyroman/fuelgrid-os/internal/workforce"
)

type shiftDTO struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	StationID      uuid.UUID  `json:"station_id"`
	OperatingDayID uuid.UUID  `json:"operating_day_id"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	OpenedBy       uuid.UUID  `json:"opened_by"`
	OpenedAt       string     `json:"opened_at"`
	ClosedBy       *uuid.UUID `json:"closed_by,omitempty"`
	ClosedAt       *string    `json:"closed_at,omitempty"`
	ApprovedBy     *uuid.UUID `json:"approved_by,omitempty"`
	ApprovedAt     *string    `json:"approved_at,omitempty"`
	Notes          *string    `json:"notes,omitempty"`
	Slot           *string    `json:"slot,omitempty"`
	TeamID         *uuid.UUID `json:"team_id,omitempty"`
}

type attendantDTO struct {
	ShiftID    uuid.UUID `json:"shift_id"`
	UserID     uuid.UUID `json:"user_id"`
	AssignedBy uuid.UUID `json:"assigned_by"`
	AssignedAt string    `json:"assigned_at"`
}

type nozzleAssignmentDTO struct {
	ID          uuid.UUID `json:"id"`
	ShiftID     uuid.UUID `json:"shift_id"`
	NozzleID    uuid.UUID `json:"nozzle_id"`
	AttendantID uuid.UUID `json:"attendant_id"`
	AssignedAt  string    `json:"assigned_at"`
}

type shiftDetailDTO struct {
	shiftDTO
	Attendants        []attendantDTO        `json:"attendants"`
	NozzleAssignments []nozzleAssignmentDTO `json:"nozzle_assignments"`
}

func toShiftDTO(s *operations.Shift) shiftDTO {
	return shiftDTO{
		ID: s.ID, TenantID: s.TenantID, StationID: s.StationID,
		OperatingDayID: s.OperatingDayID, Name: s.Name, Status: s.Status,
		OpenedBy: s.OpenedBy, OpenedAt: s.OpenedAt.Format(time.RFC3339),
		ClosedBy: s.ClosedBy, ClosedAt: fmtTime(s.ClosedAt),
		ApprovedBy: s.ApprovedBy, ApprovedAt: fmtTime(s.ApprovedAt),
		Notes: s.Notes, Slot: s.Slot, TeamID: s.TeamID,
	}
}

func toAttendantDTO(a *operations.Attendant) attendantDTO {
	return attendantDTO{
		ShiftID: a.ShiftID, UserID: a.UserID,
		AssignedBy: a.AssignedBy, AssignedAt: a.AssignedAt.Format(time.RFC3339),
	}
}

func toNozzleAssignmentDTO(n *operations.NozzleAssignment) nozzleAssignmentDTO {
	return nozzleAssignmentDTO{
		ID: n.ID, ShiftID: n.ShiftID, NozzleID: n.NozzleID,
		AttendantID: n.AttendantID, AssignedAt: n.AssignedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleListShifts(w http.ResponseWriter, r *http.Request) {
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
	var dayID *uuid.UUID
	if v := r.URL.Query().Get("operating_day_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid operating_day_id")
			return
		}
		dayID = &id
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.operations.ListShiftsPage(r.Context(), actor.TenantID, stationID, dayID, limit+1, offset)
	if err != nil {
		s.logger.Error("list shifts", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]shiftDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toShiftDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

type openShiftRequest struct {
	OperatingDayID uuid.UUID `json:"operating_day_id"`
	Name           string    `json:"name"`
	Slot           string    `json:"slot"`
	Notes          *string   `json:"notes,omitempty"`
}

func (s *Server) handleOpenShift(w http.ResponseWriter, r *http.Request) {
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
	var req openShiftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.OperatingDayID == uuid.Nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "operating_day_id and name are required")
		return
	}
	slot := workforce.Slot(req.Slot)
	if !slot.Valid() {
		writeError(w, http.StatusBadRequest, "slot is required and must be morning or evening")
		return
	}

	ctx := r.Context()
	day, err := s.operations.GetDay(ctx, actor.TenantID, req.OperatingDayID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "operating day not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if day.StationID != stationID {
		writeError(w, http.StatusBadRequest, "operating day belongs to a different station")
		return
	}
	if day.Status != "open" {
		writeError(w, http.StatusConflict, "operating day is not open")
		return
	}

	// Resolve the team rostered onto this slot for the operating day's business
	// date. A shift never opens without its expected employees: reject when the
	// station has no rotation anchor/teams or the scheduled team is empty.
	sched, err := s.workforce.ScheduledTeamFor(ctx, actor.TenantID, stationID, day.BusinessDate, slot)
	if errors.Is(err, workforce.ErrNotFound) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		s.logger.Error("open shift: scheduled team", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if sched.Team == nil {
		writeError(w, http.StatusBadRequest,
			"station has no rotation configured (set the rotation anchor and the three teams first)")
		return
	}
	// Attendants are the team members that have a login account (user_id);
	// back-office staff without one are simply not auto-assigned.
	attendantUserIDs := make([]uuid.UUID, 0, len(sched.Members))
	for i := range sched.Members {
		if sched.Members[i].UserID != nil {
			attendantUserIDs = append(attendantUserIDs, *sched.Members[i].UserID)
		}
	}
	if len(attendantUserIDs) == 0 {
		writeError(w, http.StatusBadRequest,
			"the scheduled team has no members with a login account — no shift without its expected employees")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Re-check the day's status under a FOR SHARE lock inside the tx. This
	// conflicts with the FOR UPDATE a day close/reopen takes, so the day cannot
	// be closed between the pool read above and inserting the shift — closing
	// the TOCTOU where an open shift lands on a just-closed day (OPS-007).
	status, err := s.operations.LockDayStatusForShare(ctx, tx, actor.TenantID, req.OperatingDayID)
	if errors.Is(err, operations.ErrNotFound) {
		writeError(w, http.StatusNotFound, "operating day not found")
		return
	}
	if err != nil {
		s.logger.Error("open shift: lock day", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if status != "open" {
		writeError(w, http.StatusConflict, "operating day is not open")
		return
	}

	slotStr := string(slot)
	teamID := sched.Team.ID
	shift, err := s.operations.OpenShift(ctx, tx, actor.TenantID, operations.OpenShiftInput{
		StationID: stationID, OperatingDayID: req.OperatingDayID, OpenedBy: actor.UserID,
		Name: req.Name, Notes: req.Notes, Slot: &slotStr, TeamID: &teamID,
	})
	if err != nil {
		s.logger.Error("open shift", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Auto-populate the shift's attendants from the scheduled team's members
	// that have a login account.
	for _, uid := range attendantUserIDs {
		if err := s.operations.AssignAttendant(ctx, tx, actor.TenantID, shift.ID, uid, actor.UserID); err != nil {
			s.logger.Error("open shift: auto-assign attendant", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift.opened", EventType: "ShiftOpened",
		EntityType: "shift", EntityID: shift.ID.String(),
		NewValue: toShiftDTO(shift),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("open shift: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toShiftDTO(shift))
}

// shiftForWrite loads a shift, authorizes the actor for the given permission
// against its station, and (when requireOpen) checks it's still open.
// Returns the shift + ok; writes the error response on failure.
func (s *Server) shiftForWrite(w http.ResponseWriter, r *http.Request, actor identity.Actor, perm string, requireOpen bool) (*operations.Shift, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid shift id")
		return nil, false
	}
	shift, err := s.operations.GetShift(r.Context(), actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "shift not found")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	if !s.authorizeStation(w, r, actor, perm, shift.StationID) {
		return nil, false
	}
	if requireOpen && shift.Status != "open" {
		writeError(w, http.StatusConflict, "shift is not open")
		return nil, false
	}
	return shift, true
}

// shiftForScopedWrite loads the shift for an attendant-class write (meter/dip
// readings, cash). The actor is allowed if they hold overridePerm at the
// shift's station (supervisor/manager path, override=true) or hold basePerm
// at the station AND are an attendant on the shift (attendant self-scope
// path, override=false). The override flag lets callers further restrict an
// attendant to their own assigned nozzles/tanks. Returns the shift, the
// override flag, and ok; writes the error response on failure.
func (s *Server) shiftForScopedWrite(w http.ResponseWriter, r *http.Request, actor identity.Actor, basePerm, overridePerm string, requireOpen bool) (*operations.Shift, bool, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid shift id")
		return nil, false, false
	}
	ctx := r.Context()
	shift, err := s.operations.GetShift(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "shift not found")
		return nil, false, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false, false
	}

	override := s.policy.Can(ctx, actor, overridePerm, policy.AtStation(shift.StationID)) == nil
	if !override {
		// Attendant self-scope: must hold the base permission at the station
		// and be assigned to this shift.
		if !s.authorizeStation(w, r, actor, basePerm, shift.StationID) {
			return nil, false, false
		}
		onShift, err := s.operations.IsAttendantOnShift(ctx, actor.TenantID, shift.ID, actor.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return nil, false, false
		}
		if !onShift {
			writeError(w, http.StatusForbidden, "you are not assigned to this shift")
			return nil, false, false
		}
	}

	if requireOpen && shift.Status != "open" {
		writeError(w, http.StatusConflict, "shift is not open")
		return nil, false, false
	}
	return shift, override, true
}

// requireNozzleAssigned enforces that the nozzle is assigned on the shift
// (integrity, all roles) and, unless the caller is acting via override, that
// it's assigned to the actor (attendant self-scope). Returns true when
// allowed; otherwise writes the error response and returns false.
func (s *Server) requireNozzleAssigned(w http.ResponseWriter, ctx context.Context, actor identity.Actor, shiftID, nozzleID uuid.UUID, override bool) bool {
	var attendantFilter *uuid.UUID
	if !override {
		attendantFilter = &actor.UserID
	}
	assigned, err := s.operations.NozzleAssignedOnShift(ctx, actor.TenantID, shiftID, nozzleID, attendantFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	if !assigned {
		if override {
			writeError(w, http.StatusUnprocessableEntity, "nozzle is not assigned on this shift")
		} else {
			writeError(w, http.StatusForbidden, "nozzle is not assigned to you on this shift")
		}
		return false
	}
	return true
}

// requireTankAssigned enforces that some nozzle drawing from the tank is
// assigned on the shift and, unless override, that one is assigned to the
// actor. Returns true when allowed; otherwise writes the response.
func (s *Server) requireTankAssigned(w http.ResponseWriter, ctx context.Context, actor identity.Actor, shiftID, tankID uuid.UUID, override bool) bool {
	var attendantFilter *uuid.UUID
	if !override {
		attendantFilter = &actor.UserID
	}
	assigned, err := s.operations.TankAssignedOnShift(ctx, actor.TenantID, shiftID, tankID, attendantFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	if !assigned {
		if override {
			writeError(w, http.StatusUnprocessableEntity, "no nozzle drawing from this tank is assigned on this shift")
		} else {
			writeError(w, http.StatusForbidden, "no nozzle drawing from this tank is assigned to you on this shift")
		}
		return false
	}
	return true
}

func (s *Server) handleGetShift(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid shift id")
		return
	}
	ctx := r.Context()
	shift, err := s.operations.GetShift(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "shift not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", shift.StationID) {
		return
	}

	attendants, err := s.operations.ListAttendants(ctx, actor.TenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	assignments, err := s.operations.ListNozzleAssignments(ctx, actor.TenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	detail := shiftDetailDTO{
		shiftDTO:          toShiftDTO(shift),
		Attendants:        make([]attendantDTO, 0, len(attendants)),
		NozzleAssignments: make([]nozzleAssignmentDTO, 0, len(assignments)),
	}
	for i := range attendants {
		detail.Attendants = append(detail.Attendants, toAttendantDTO(&attendants[i]))
	}
	for i := range assignments {
		detail.NozzleAssignments = append(detail.NozzleAssignments, toNozzleAssignmentDTO(&assignments[i]))
	}
	writeJSON(w, http.StatusOK, detail)
}

type assignAttendantRequest struct {
	UserID uuid.UUID `json:"user_id"`
}

func (s *Server) handleAssignAttendant(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req assignAttendantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.UserID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	shift, ok := s.shiftForWrite(w, r, actor, "shift.assign", true)
	if !ok {
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.operations.AssignAttendant(ctx, tx, actor.TenantID, shift.ID, req.UserID, actor.UserID); err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "user is already on this shift")
			return
		}
		if isForeignKeyViolation(err) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		s.logger.Error("assign attendant", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift.attendant_assigned", EventType: "ShiftAttendantAssigned",
		EntityType: "shift", EntityID: shift.ID.String(),
		NewValue: map[string]any{"shift_id": shift.ID, "user_id": req.UserID},
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleUnassignAttendant(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	shift, ok := s.shiftForWrite(w, r, actor, "shift.assign", true)
	if !ok {
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.operations.UnassignAttendant(ctx, tx, actor.TenantID, shift.ID, userID); err != nil {
		if errors.Is(err, operations.ErrAssignmentNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift.attendant_unassigned", EventType: "ShiftAttendantUnassigned",
		EntityType: "shift", EntityID: shift.ID.String(),
		PreviousValue: map[string]any{"shift_id": shift.ID, "user_id": userID},
		IP:            clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type assignNozzleRequest struct {
	NozzleID    uuid.UUID `json:"nozzle_id"`
	AttendantID uuid.UUID `json:"attendant_id"`
}

func (s *Server) handleAssignNozzle(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req assignNozzleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.NozzleID == uuid.Nil || req.AttendantID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "nozzle_id and attendant_id are required")
		return
	}
	shift, ok := s.shiftForWrite(w, r, actor, "shift.assign", true)
	if !ok {
		return
	}

	ctx := r.Context()
	// The nozzle must belong to the shift's station.
	nozzle, err := s.nozzles.Get(ctx, actor.TenantID, req.NozzleID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "nozzle not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if nozzle.StationID != shift.StationID {
		writeError(w, http.StatusBadRequest, "nozzle is at a different station than the shift")
		return
	}
	// The attendant must already be on the shift.
	onShift, err := s.operations.IsAttendantOnShift(ctx, actor.TenantID, shift.ID, req.AttendantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !onShift {
		writeError(w, http.StatusBadRequest, "attendant is not assigned to this shift")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	assignment, err := s.operations.AssignNozzle(ctx, tx, actor.TenantID, shift.StationID, shift.ID, req.NozzleID, req.AttendantID, actor.UserID)
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "nozzle is already assigned on this shift")
		return
	}
	if err != nil {
		s.logger.Error("assign nozzle", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift.nozzle_assigned", EventType: "ShiftNozzleAssigned",
		EntityType: "shift", EntityID: shift.ID.String(),
		NewValue: toNozzleAssignmentDTO(assignment),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toNozzleAssignmentDTO(assignment))
}

func (s *Server) handleUnassignNozzle(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	assignmentID, err := uuid.Parse(chi.URLParam(r, "assignmentID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid assignment id")
		return
	}
	shift, ok := s.shiftForWrite(w, r, actor, "shift.assign", true)
	if !ok {
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.operations.UnassignNozzle(ctx, tx, actor.TenantID, shift.ID, assignmentID); err != nil {
		if errors.Is(err, operations.ErrAssignmentNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift.nozzle_unassigned", EventType: "ShiftNozzleUnassigned",
		EntityType: "shift", EntityID: shift.ID.String(),
		PreviousValue: map[string]any{"shift_id": shift.ID, "assignment_id": assignmentID},
		IP:            clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
