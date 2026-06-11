package server

// Attendant check-in/out + nozzle-assignment confirmation (Mobile Attendant
// App, Phase 0). Check-in/out and confirmation are SELF-scoped attendant
// actions in the /me/shift style: gated by authentication alone, with the
// membership / assignee check enforced in-handler. The attendance list is a
// station-scoped supervisor read (station.read).

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
)

type attendanceDTO struct {
	ID          uuid.UUID       `json:"id"`
	TenantID    uuid.UUID       `json:"tenant_id"`
	StationID   uuid.UUID       `json:"station_id"`
	ShiftID     uuid.UUID       `json:"shift_id"`
	AttendantID uuid.UUID       `json:"attendant_id"`
	Status      string          `json:"status"`
	CheckInAt   string          `json:"check_in_at"`
	CheckOutAt  *string         `json:"check_out_at,omitempty"`
	DeviceInfo  json.RawMessage `json:"device_info,omitempty"`
}

func toAttendanceDTO(a *operations.Attendance) attendanceDTO {
	return attendanceDTO{
		ID: a.ID, TenantID: a.TenantID, StationID: a.StationID,
		ShiftID: a.ShiftID, AttendantID: a.AttendantID, Status: a.Status,
		CheckInAt: a.CheckInAt.Format(time.RFC3339), CheckOutAt: fmtTime(a.CheckOutAt),
		DeviceInfo: a.DeviceInfo,
	}
}

// shiftForSelfAction loads the shift for a self-scoped attendant action and
// enforces membership: the actor must be on the shift's attendant roster,
// else 403. No permission is required beyond the session (mirrors the
// /me/shift self-scoping). Returns the shift + ok; writes the error response
// on failure.
func (s *Server) shiftForSelfAction(w http.ResponseWriter, r *http.Request, actor identity.Actor) (*operations.Shift, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid shift id")
		return nil, false
	}
	ctx := r.Context()
	shift, err := s.operations.GetShift(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "shift not found")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	onShift, err := s.operations.IsAttendantOnShift(ctx, actor.TenantID, shift.ID, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	if !onShift {
		writeError(w, http.StatusForbidden, "you are not assigned to this shift")
		return nil, false
	}
	return shift, true
}

type checkInRequest struct {
	DeviceInfo json.RawMessage `json:"device_info,omitempty"`
}

// handleCheckIn records the actor's check-in to a shift they are rostered on.
// Idempotent: a repeat check-in returns the existing record with 200 (the
// first returns 201); only the first writes audit + outbox.
func (s *Server) handleCheckIn(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	// The body is optional ({device_info} or nothing at all).
	var req checkInRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	shift, ok := s.shiftForSelfAction(w, r, actor)
	if !ok {
		return
	}
	ctx := r.Context()

	// Idempotent duplicate: hand back the existing record unchanged.
	if existing, err := s.operations.GetAttendance(ctx, actor.TenantID, shift.ID, actor.UserID); err == nil {
		writeJSON(w, http.StatusOK, toAttendanceDTO(existing))
		return
	} else if !errors.Is(err, operations.ErrAttendanceNotFound) {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// A first check-in needs a running shift.
	if shift.Status != "open" {
		writeError(w, http.StatusConflict, "shift is not open")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rec, err := s.operations.CheckIn(ctx, tx, actor.TenantID, shift.StationID, shift.ID, actor.UserID, req.DeviceInfo)
	if isUniqueViolation(err) {
		// Raced a concurrent duplicate; the idempotent answer is the winner's row.
		if existing, gerr := s.operations.GetAttendance(ctx, actor.TenantID, shift.ID, actor.UserID); gerr == nil {
			writeJSON(w, http.StatusOK, toAttendanceDTO(existing))
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err != nil {
		s.logger.Error("check in", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift.attendant_checked_in", EventType: "AttendantCheckedIn",
		EntityType: "shift_attendance", EntityID: rec.ID.String(),
		NewValue: toAttendanceDTO(rec),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("check in: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toAttendanceDTO(rec))
}

// handleCheckOut flips the actor's attendance to checked_out. Idempotent: a
// repeat check-out returns the existing checked_out record with 200; checking
// out without ever checking in is a 409.
func (s *Server) handleCheckOut(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	shift, ok := s.shiftForSelfAction(w, r, actor)
	if !ok {
		return
	}
	ctx := r.Context()

	existing, err := s.operations.GetAttendance(ctx, actor.TenantID, shift.ID, actor.UserID)
	if errors.Is(err, operations.ErrAttendanceNotFound) {
		writeError(w, http.StatusConflict, "you have not checked in to this shift")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if existing.Status == "checked_out" {
		writeJSON(w, http.StatusOK, toAttendanceDTO(existing))
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rec, err := s.operations.CheckOut(ctx, tx, actor.TenantID, shift.ID, actor.UserID)
	if errors.Is(err, operations.ErrAttendanceNotFound) {
		// Raced a concurrent check-out; the idempotent answer is the final row.
		if final, gerr := s.operations.GetAttendance(ctx, actor.TenantID, shift.ID, actor.UserID); gerr == nil {
			writeJSON(w, http.StatusOK, toAttendanceDTO(final))
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err != nil {
		s.logger.Error("check out", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift.attendant_checked_out", EventType: "AttendantCheckedOut",
		EntityType: "shift_attendance", EntityID: rec.ID.String(),
		PreviousValue: toAttendanceDTO(existing), NewValue: toAttendanceDTO(rec),
		IP: clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("check out: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toAttendanceDTO(rec))
}

// handleListShiftAttendance is the supervisor's station-scoped view of who
// checked in/out of a shift.
func (s *Server) handleListShiftAttendance(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.operations.ListAttendanceForShift(ctx, actor.TenantID, id)
	if err != nil {
		s.logger.Error("list shift attendance", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]attendanceDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toAttendanceDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

type confirmedAssignmentDTO struct {
	ID          uuid.UUID `json:"id"`
	ShiftID     uuid.UUID `json:"shift_id"`
	NozzleID    uuid.UUID `json:"nozzle_id"`
	AttendantID uuid.UUID `json:"attendant_id"`
	AssignedAt  string    `json:"assigned_at"`
	ConfirmedAt *string   `json:"confirmed_at,omitempty"`
}

func toConfirmedAssignmentDTO(a *operations.ConfirmableAssignment) confirmedAssignmentDTO {
	return confirmedAssignmentDTO{
		ID: a.ID, ShiftID: a.ShiftID, NozzleID: a.NozzleID, AttendantID: a.AttendantID,
		AssignedAt: a.AssignedAt.Format(time.RFC3339), ConfirmedAt: fmtTime(a.ConfirmedAt),
	}
}

// handleConfirmNozzleAssignment lets ONLY the assigned attendant acknowledge
// their nozzle for the shift. Idempotent: an already-confirmed assignment
// returns 200 with the original confirmation timestamp. A reassignment
// (delete + recreate, the only reassignment path) yields a fresh row with
// confirmed_at NULL, so it always needs a fresh confirmation.
func (s *Server) handleConfirmNozzleAssignment(w http.ResponseWriter, r *http.Request) {
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
	shift, ok := s.shiftForSelfAction(w, r, actor)
	if !ok {
		return
	}
	ctx := r.Context()

	assignment, err := s.operations.GetNozzleAssignment(ctx, actor.TenantID, shift.ID, assignmentID)
	if errors.Is(err, operations.ErrAssignmentNotFound) {
		writeError(w, http.StatusNotFound, "assignment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if assignment.AttendantID != actor.UserID {
		writeError(w, http.StatusForbidden, "only the assigned attendant may confirm this assignment")
		return
	}
	if assignment.ConfirmedAt != nil {
		writeJSON(w, http.StatusOK, toConfirmedAssignmentDTO(assignment))
		return
	}
	if shift.Status != "open" {
		writeError(w, http.StatusConflict, "shift is not open")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	confirmed, err := s.operations.ConfirmNozzleAssignment(ctx, tx, actor.TenantID, shift.ID, assignmentID)
	if errors.Is(err, operations.ErrAssignmentNotFound) {
		// Raced a concurrent confirm; the idempotent answer is the stamped row.
		if final, gerr := s.operations.GetNozzleAssignment(ctx, actor.TenantID, shift.ID, assignmentID); gerr == nil {
			writeJSON(w, http.StatusOK, toConfirmedAssignmentDTO(final))
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err != nil {
		s.logger.Error("confirm nozzle assignment", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift.assignment_confirmed", EventType: "AssignmentConfirmed",
		EntityType: "shift_nozzle_assignment", EntityID: confirmed.ID.String(),
		NewValue: toConfirmedAssignmentDTO(confirmed),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("confirm nozzle assignment: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toConfirmedAssignmentDTO(confirmed))
}
