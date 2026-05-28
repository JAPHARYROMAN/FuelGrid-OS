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
	"github.com/japharyroman/fuelgrid-os/internal/pumps"
)

// lifecycleStatuses are the statuses a pump or tank may be transitioned to
// via the dedicated status endpoints. 'deleted' is intentionally excluded —
// removal goes through DELETE, not a status PATCH.
var lifecycleStatuses = map[string]bool{
	"active": true, "inactive": true, "maintenance": true, "decommissioned": true,
}

type pumpDTO struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"tenant_id"`
	StationID        uuid.UUID `json:"station_id"`
	Number           int       `json:"number"`
	Name             *string   `json:"name,omitempty"`
	Manufacturer     *string   `json:"manufacturer,omitempty"`
	Model            *string   `json:"model,omitempty"`
	SerialNumber     *string   `json:"serial_number,omitempty"`
	Status           string    `json:"status"`
	InstallationDate *string   `json:"installation_date,omitempty"`
}

func toPumpDTO(p *pumps.Pump) pumpDTO {
	return pumpDTO{
		ID: p.ID, TenantID: p.TenantID, StationID: p.StationID, Number: p.Number,
		Name: p.Name, Manufacturer: p.Manufacturer, Model: p.Model,
		SerialNumber: p.SerialNumber, Status: p.Status,
		InstallationDate: fmtDate(p.InstallationDate),
	}
}

func (s *Server) handleListPumps(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	filter, ok := s.stationReadFilter(w, r, actor)
	if !ok {
		return
	}
	rows, err := s.pumps.List(r.Context(), actor.TenantID, filter)
	if err != nil {
		s.logger.Error("list pumps", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]pumpDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toPumpDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleGetPump(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pump id")
		return
	}
	p, err := s.pumps.Get(r.Context(), actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "pump not found")
		return
	}
	if err != nil {
		s.logger.Error("get pump", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", p.StationID) {
		return
	}
	writeJSON(w, http.StatusOK, toPumpDTO(p))
}

type createPumpRequest struct {
	StationID        uuid.UUID `json:"station_id"`
	Number           int       `json:"number"`
	Name             *string   `json:"name,omitempty"`
	Manufacturer     *string   `json:"manufacturer,omitempty"`
	Model            *string   `json:"model,omitempty"`
	SerialNumber     *string   `json:"serial_number,omitempty"`
	InstallationDate *string   `json:"installation_date,omitempty"`
}

func (s *Server) handleCreatePump(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createPumpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.StationID == uuid.Nil || req.Number <= 0 {
		writeError(w, http.StatusBadRequest, "station_id and a positive number are required")
		return
	}
	installDate, err := parseDate(req.InstallationDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid installation_date (want YYYY-MM-DD)")
		return
	}

	if !s.authorizeStation(w, r, actor, "pumps.manage", req.StationID) {
		return
	}

	ctx := r.Context()
	if _, err := s.stations.Get(ctx, actor.TenantID, req.StationID); err != nil {
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

	p, err := s.pumps.Create(ctx, tx, actor.TenantID, pumps.CreateInput{
		StationID: req.StationID, Number: req.Number, Name: req.Name,
		Manufacturer: req.Manufacturer, Model: req.Model,
		SerialNumber: req.SerialNumber, InstallationDate: installDate,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a pump with that number already exists at this station")
		return
	}
	if err != nil {
		s.logger.Error("create pump", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "pump.created", EventType: "PumpCreated",
		EntityType: "pump", EntityID: p.ID.String(),
		NewValue: toPumpDTO(p),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("create pump: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toPumpDTO(p))
}

type updatePumpRequest struct {
	Number           *int    `json:"number,omitempty"`
	Name             *string `json:"name,omitempty"`
	Manufacturer     *string `json:"manufacturer,omitempty"`
	Model            *string `json:"model,omitempty"`
	SerialNumber     *string `json:"serial_number,omitempty"`
	Status           *string `json:"status,omitempty"`
	InstallationDate *string `json:"installation_date,omitempty"`
}

func (s *Server) handleUpdatePump(w http.ResponseWriter, r *http.Request) {
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
	var req updatePumpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Number != nil && *req.Number <= 0 {
		writeError(w, http.StatusBadRequest, "number must be positive")
		return
	}
	installDate, err := parseDate(req.InstallationDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid installation_date (want YYYY-MM-DD)")
		return
	}

	ctx := r.Context()
	before, err := s.pumps.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.authorizeStation(w, r, actor, "pumps.manage", before.StationID) {
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	after, err := s.pumps.Update(ctx, tx, actor.TenantID, id, pumps.UpdateInput{
		Number: req.Number, Name: req.Name, Manufacturer: req.Manufacturer,
		Model: req.Model, SerialNumber: req.SerialNumber, Status: req.Status,
		InstallationDate: installDate,
	})
	if errors.Is(err, pumps.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a pump with that number already exists at this station")
		return
	}
	if err != nil {
		s.logger.Error("update pump", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "pump.updated", EventType: "PumpUpdated",
		EntityType: "pump", EntityID: after.ID.String(),
		PreviousValue: toPumpDTO(before), NewValue: toPumpDTO(after),
		IP: clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toPumpDTO(after))
}

func (s *Server) handleDeletePump(w http.ResponseWriter, r *http.Request) {
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

	ctx := r.Context()
	before, err := s.pumps.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.authorizeStation(w, r, actor, "pumps.manage", before.StationID) {
		return
	}

	// Refuse to remove a pump that still has live nozzles — the nozzle FK
	// is ON DELETE RESTRICT, but soft-delete wouldn't trip it, so guard here
	// to avoid orphaning nozzles under a deleted pump.
	live, err := s.nozzles.List(ctx, actor.TenantID, nil, &id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(live) > 0 {
		writeError(w, http.StatusConflict, "remove the pump's nozzles first")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.pumps.SoftDelete(ctx, tx, actor.TenantID, id); err != nil {
		if errors.Is(err, pumps.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "pump.deleted", EventType: "PumpDeleted",
		EntityType: "pump", EntityID: id.String(),
		PreviousValue: toPumpDTO(before),
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

// ----------------------------------------------------------------------------
// Pump calibration events
// ----------------------------------------------------------------------------

type pumpCalibrationDTO struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"tenant_id"`
	PumpID           uuid.UUID `json:"pump_id"`
	PerformedAt      string    `json:"performed_at"`
	PerformedBy      uuid.UUID `json:"performed_by"`
	Notes            *string   `json:"notes,omitempty"`
	TolerancePercent *float64  `json:"tolerance_percent,omitempty"`
	Status           string    `json:"status"`
}

func toPumpCalibrationDTO(c *pumps.Calibration) pumpCalibrationDTO {
	return pumpCalibrationDTO{
		ID: c.ID, TenantID: c.TenantID, PumpID: c.PumpID,
		PerformedAt: c.PerformedAt.Format(time.RFC3339), PerformedBy: c.PerformedBy,
		Notes: c.Notes, TolerancePercent: c.TolerancePercent, Status: c.Status,
	}
}

func (s *Server) handleListPumpCalibrations(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pump id")
		return
	}
	pump, err := s.pumps.Get(r.Context(), actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "pump not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", pump.StationID) {
		return
	}
	rows, err := s.pumps.ListCalibrations(r.Context(), actor.TenantID, id)
	if err != nil {
		s.logger.Error("list pump calibrations", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]pumpCalibrationDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toPumpCalibrationDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

type createPumpCalibrationRequest struct {
	PerformedAt      *string  `json:"performed_at,omitempty"`
	Notes            *string  `json:"notes,omitempty"`
	TolerancePercent *float64 `json:"tolerance_percent,omitempty"`
	Status           string   `json:"status,omitempty"`
}

func (s *Server) handleCreatePumpCalibration(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pump id")
		return
	}
	var req createPumpCalibrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Status != "" && req.Status != "passed" && req.Status != "failed" && req.Status != "adjusted" {
		writeError(w, http.StatusBadRequest, "status must be passed, failed, or adjusted")
		return
	}
	var performedAt *time.Time
	if req.PerformedAt != nil && *req.PerformedAt != "" {
		t, err := time.Parse(time.RFC3339, *req.PerformedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "performed_at must be RFC3339")
			return
		}
		performedAt = &t
	}

	ctx := r.Context()
	pump, err := s.pumps.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "pump not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.authorizeStation(w, r, actor, "pumps.calibrate", pump.StationID) {
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	cal, err := s.pumps.CreateCalibration(ctx, tx, actor.TenantID, pumps.CreateCalibrationInput{
		PumpID: pump.ID, PerformedBy: actor.UserID, PerformedAt: performedAt,
		Notes: req.Notes, TolerancePercent: req.TolerancePercent, Status: req.Status,
	})
	if err != nil {
		s.logger.Error("create pump calibration", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "pump.calibrated", EventType: "PumpCalibrated",
		EntityType: "pump", EntityID: pump.ID.String(),
		NewValue: toPumpCalibrationDTO(cal),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("create pump calibration: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toPumpCalibrationDTO(cal))
}

// ----------------------------------------------------------------------------
// Pump status lifecycle
// ----------------------------------------------------------------------------

type statusChangeRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func (s *Server) handleUpdatePumpStatus(w http.ResponseWriter, r *http.Request) {
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
	var req statusChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !lifecycleStatuses[req.Status] {
		writeError(w, http.StatusBadRequest, "status must be active, inactive, maintenance, or decommissioned")
		return
	}

	ctx := r.Context()
	before, err := s.pumps.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.authorizeStation(w, r, actor, "pumps.manage", before.StationID) {
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	after, err := s.pumps.Update(ctx, tx, actor.TenantID, id, pumps.UpdateInput{Status: &req.Status})
	if errors.Is(err, pumps.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.logger.Error("update pump status", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "pump.status_changed", EventType: "PumpStatusChanged",
		EntityType: "pump", EntityID: after.ID.String(),
		PreviousValue: toPumpDTO(before), NewValue: toPumpDTO(after),
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
	writeJSON(w, http.StatusOK, toPumpDTO(after))
}
