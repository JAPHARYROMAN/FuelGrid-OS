package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	setupdomain "github.com/japharyroman/fuelgrid-os/internal/setup"
)

type setupStepDTO struct {
	Code          string     `json:"code"`
	StationID     *uuid.UUID `json:"station_id,omitempty"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Href          string     `json:"href"`
	CTA           string     `json:"cta"`
	Required      bool       `json:"required"`
	Status        string     `json:"status"`
	Ready         bool       `json:"ready"`
	Blocked       bool       `json:"blocked"`
	BlockedReason *string    `json:"blocked_reason,omitempty"`
	Count         int        `json:"count"`
	RequiredCount int        `json:"required_count"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CompletedBy   *uuid.UUID `json:"completed_by,omitempty"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
	Notes         *string    `json:"notes,omitempty"`
}

type setupChecklistDTO struct {
	Steps              []setupStepDTO        `json:"steps"`
	RequiredTotal      int                   `json:"required_total"`
	RequiredReady      int                   `json:"required_ready"`
	RequiredCompleted  int                   `json:"required_completed"`
	OperationallyReady bool                  `json:"operationally_ready"`
	Blocked            []setupdomain.Blocker `json:"blocked"`
}

func toSetupStepDTO(s setupdomain.Step) setupStepDTO {
	return setupStepDTO{
		Code: s.Code, Title: s.Title, Description: s.Description, Href: s.Href, CTA: s.CTA,
		StationID: s.StationID,
		Required:  s.Required, Status: s.Status, Ready: s.Ready, Blocked: s.Blocked,
		BlockedReason: s.BlockedReason, Count: s.Count, RequiredCount: s.RequiredCount,
		CompletedAt: s.CompletedAt, CompletedBy: s.CompletedBy, UpdatedAt: s.UpdatedAt, Notes: s.Notes,
	}
}

func toSetupChecklistDTO(c setupdomain.Checklist) setupChecklistDTO {
	out := setupChecklistDTO{
		Steps:              make([]setupStepDTO, 0, len(c.Steps)),
		RequiredTotal:      c.RequiredTotal,
		RequiredReady:      c.RequiredReady,
		RequiredCompleted:  c.RequiredCompleted,
		OperationallyReady: c.OperationallyReady,
		Blocked:            c.Blocked,
	}
	for i := range c.Steps {
		out.Steps = append(out.Steps, toSetupStepDTO(c.Steps[i]))
	}
	return out
}

func (s *Server) handleGetSetupChecklist(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	filter, ok := s.stationReadFilter(w, r, actor)
	if !ok {
		return
	}
	checklist, err := s.setup.Checklist(r.Context(), actor.TenantID, filter)
	if err != nil {
		s.logger.Error("setup checklist", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toSetupChecklistDTO(checklist))
}

type patchSetupChecklistRequest struct {
	StepCode string  `json:"step_code"`
	Status   string  `json:"status"`
	Notes    *string `json:"notes,omitempty"`
}

func (s *Server) handlePatchSetupChecklist(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req patchSetupChecklistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.StepCode == "" || req.Status == "" {
		writeError(w, http.StatusBadRequest, "step_code and status are required")
		return
	}
	filter, ok := s.stationReadFilter(w, r, actor)
	if !ok {
		return
	}
	stationID := setupStationID(filter)
	if setupdomain.IsStationScopedStep(req.StepCode) && stationID == nil {
		writeError(w, http.StatusBadRequest, "station_id is required for this setup step")
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	state, err := s.setup.UpsertStep(ctx, tx, actor.TenantID, actor.UserID, stationID, req.StepCode, req.Status, req.Notes)
	if errors.Is(err, setupdomain.ErrInvalidStep) {
		writeError(w, http.StatusBadRequest, "invalid setup step")
		return
	}
	if errors.Is(err, setupdomain.ErrInvalidStatus) {
		writeError(w, http.StatusBadRequest, "status must be pending, completed, or skipped")
		return
	}
	if errors.Is(err, setupdomain.ErrStationRequired) {
		writeError(w, http.StatusBadRequest, "station_id is required for this setup step")
		return
	}
	if err != nil {
		s.logger.Error("setup checklist update", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "setup.step_updated", EventType: "SetupStepUpdated",
		EntityType: "setup_step", EntityID: req.StepCode,
		NewValue: state,
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("setup checklist update: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	checklist, err := s.setup.Checklist(ctx, actor.TenantID, filter)
	if err != nil {
		s.logger.Error("setup checklist reload", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toSetupChecklistDTO(checklist))
}

func setupStationID(filter []uuid.UUID) *uuid.UUID {
	if len(filter) != 1 {
		return nil
	}
	id := filter[0]
	return &id
}
