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
	rows, err := s.operations.ListDays(r.Context(), actor.TenantID, stationID)
	if err != nil {
		s.logger.Error("list operating days", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]operatingDayDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toOperatingDayDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
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

	if before.Status == "locked" {
		writeError(w, http.StatusConflict, "a locked day cannot change status")
		return
	}
	if before.Status == req.Status {
		writeError(w, http.StatusConflict, "already "+req.Status)
		return
	}
	// NOTE (Stage 2): closing will also be refused while the day has open
	// shifts, once the shifts table exists.

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
	// NOTE (Stage 2): locking will also require every shift in the day to be
	// approved, once the shifts table exists.

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
