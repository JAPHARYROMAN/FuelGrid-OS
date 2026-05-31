package server

import (
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
	"github.com/japharyroman/fuelgrid-os/internal/operations"
)

type operatingDayDTO struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	StationID    uuid.UUID  `json:"station_id"`
	BusinessDate string     `json:"business_date"`
	Status       string     `json:"status"`
	OpenedBy     uuid.UUID  `json:"opened_by"`
	OpenedAt     string     `json:"opened_at"`
	ClosedBy     *uuid.UUID `json:"closed_by,omitempty"`
	ClosedAt     *string    `json:"closed_at,omitempty"`
	LockedBy     *uuid.UUID `json:"locked_by,omitempty"`
	LockedAt     *string    `json:"locked_at,omitempty"`
	Notes        *string    `json:"notes,omitempty"`
}

func toOperatingDayDTO(d *operations.OperatingDay) operatingDayDTO {
	return operatingDayDTO{
		ID: d.ID, TenantID: d.TenantID, StationID: d.StationID,
		BusinessDate: d.BusinessDate.Format(dateLayout),
		Status:       d.Status,
		OpenedBy:     d.OpenedBy, OpenedAt: d.OpenedAt.Format(time.RFC3339),
		ClosedBy: d.ClosedBy, ClosedAt: fmtTime(d.ClosedAt),
		LockedBy: d.LockedBy, LockedAt: fmtTime(d.LockedAt),
		Notes: d.Notes,
	}
}

func (s *Server) handleListOperatingDays(w http.ResponseWriter, r *http.Request) {
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
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	// Fetch one extra row to learn whether a further page exists, then trim.
	rows, err := s.operations.ListDaysPage(r.Context(), actor.TenantID, stationID, limit+1, offset)
	if err != nil {
		s.logger.Error("list operating days", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]operatingDayDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toOperatingDayDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

type openOperatingDayRequest struct {
	BusinessDate *string `json:"business_date,omitempty"`
	Notes        *string `json:"notes,omitempty"`
}

func (s *Server) handleOpenOperatingDay(w http.ResponseWriter, r *http.Request) {
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
	var req openOperatingDayRequest
	if r.Body != nil {
		// Body is optional; an empty body opens today.
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	businessDate := time.Now().UTC()
	if req.BusinessDate != nil && *req.BusinessDate != "" {
		parsed, err := parseDate(req.BusinessDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid business_date (want YYYY-MM-DD)")
			return
		}
		businessDate = *parsed
	}

	ctx := r.Context()
	// The route already authorized operations.manage_day at this station;
	// confirm it exists in the tenant for a clean 404.
	if _, err := s.stations.Get(ctx, actor.TenantID, stationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "station not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	day, err := s.operations.OpenDay(ctx, tx, actor.TenantID, stationID, actor.UserID, businessDate, req.Notes)
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "an operating day is already open for that date")
		return
	}
	if err != nil {
		s.logger.Error("open operating day", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "operating_day.opened", EventType: "OperatingDayOpened",
		EntityType: "operating_day", EntityID: day.ID.String(),
		NewValue: toOperatingDayDTO(day),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("open operating day: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toOperatingDayDTO(day))
}

func (s *Server) handleGetOperatingDay(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	day, err := s.operations.GetDay(r.Context(), actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "operating day not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", day.StationID) {
		return
	}
	writeJSON(w, http.StatusOK, toOperatingDayDTO(day))
}

type operatingDayStatusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func (s *Server) handleUpdateOperatingDayStatus(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req operatingDayStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Status != "open" && req.Status != "closed" {
		writeError(w, http.StatusBadRequest, "status must be open or closed (use /lock to lock)")
		return
	}

	ctx := r.Context()
	before, err := s.operations.GetDay(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.authorizeStation(w, r, actor, "operations.manage_day", before.StationID) {
		return
	}

	action, event := "operating_day.reopened", "OperatingDayReopened"
	if req.Status == "closed" {
		action, event = "operating_day.closed", "OperatingDayClosed"
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the day row, then re-validate its status — and, when closing, its
	// open-shift count — inside the tx. A concurrent shift-open takes FOR SHARE
	// on this same row, so it cannot slip an open shift onto a day that is being
	// closed, and a second status change cannot race this one (OPS-007).
	locked, err := s.operations.GetDayForUpdate(ctx, tx, actor.TenantID, id)
	if errors.Is(err, operations.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if locked.Status == "locked" {
		writeError(w, http.StatusConflict, "a locked day cannot change status")
		return
	}
	if locked.Status == req.Status {
		writeError(w, http.StatusConflict, "already "+req.Status)
		return
	}
	// A day can't close while it still has open shifts.
	if req.Status == "closed" {
		open, err := s.operations.OpenShiftCountForDay(ctx, tx, actor.TenantID, locked.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if open > 0 {
			writeError(w, http.StatusConflict, "close the day's open shifts first")
			return
		}
	}

	after, err := s.operations.SetStatus(ctx, tx, actor.TenantID, id, req.Status, actor.UserID)
	if errors.Is(err, operations.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.logger.Error("operating day status", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: action, EventType: event,
		EntityType: "operating_day", EntityID: after.ID.String(),
		PreviousValue: toOperatingDayDTO(locked), NewValue: toOperatingDayDTO(after),
		Reason: req.Reason,
		IP:     clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toOperatingDayDTO(after))
}

type lockOperatingDayRequest struct {
	Reason string `json:"reason,omitempty"`
}

func (s *Server) handleLockOperatingDay(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req lockOperatingDayRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	ctx := r.Context()
	before, err := s.operations.GetDay(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.authorizeStation(w, r, actor, "operations.manage_day", before.StationID) {
		return
	}

	if before.Status != "closed" {
		writeError(w, http.StatusConflict, "only a closed day can be locked")
		return
	}
	// A day can't lock until every shift in it is approved.
	if unapproved, err := s.operations.UnapprovedShiftCountForDay(ctx, actor.TenantID, before.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if unapproved > 0 {
		writeError(w, http.StatusConflict, "all shifts must be approved before locking the day")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	after, err := s.operations.Lock(ctx, tx, actor.TenantID, id, actor.UserID)
	if errors.Is(err, operations.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.logger.Error("lock operating day", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "operating_day.locked", EventType: "OperatingDayLocked",
		EntityType: "operating_day", EntityID: after.ID.String(),
		PreviousValue: toOperatingDayDTO(before), NewValue: toOperatingDayDTO(after),
		Reason: req.Reason,
		IP:     clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toOperatingDayDTO(after))
}
