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
	"github.com/japharyroman/fuelgrid-os/internal/incidents"
)

var incidentStatuses = map[string]bool{
	"open": true, "investigating": true, "resolved": true, "closed": true,
}

type incidentDTO struct {
	ID                uuid.UUID  `json:"id"`
	TenantID          uuid.UUID  `json:"tenant_id"`
	StationID         uuid.UUID  `json:"station_id"`
	RelatedEntityType *string    `json:"related_entity_type,omitempty"`
	RelatedEntityID   *uuid.UUID `json:"related_entity_id,omitempty"`
	Type              string     `json:"type"`
	Severity          string     `json:"severity"`
	Description       string     `json:"description"`
	Status            string     `json:"status"`
	OpenedAt          string     `json:"opened_at"`
	OpenedBy          uuid.UUID  `json:"opened_by"`
	ResolvedAt        *string    `json:"resolved_at,omitempty"`
	ResolvedBy        *uuid.UUID `json:"resolved_by,omitempty"`
	DedupeKey         *string    `json:"dedupe_key,omitempty"`
}

func toIncidentDTO(i *incidents.Incident) incidentDTO {
	return incidentDTO{
		ID: i.ID, TenantID: i.TenantID, StationID: i.StationID,
		RelatedEntityType: i.RelatedEntityType, RelatedEntityID: i.RelatedEntityID,
		Type: i.Type, Severity: i.Severity, Description: i.Description, Status: i.Status,
		OpenedAt: i.OpenedAt.Format(time.RFC3339), OpenedBy: i.OpenedBy,
		ResolvedAt: fmtTime(i.ResolvedAt), ResolvedBy: i.ResolvedBy,
		DedupeKey: i.DedupeKey,
	}
}

func (s *Server) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationFilter, ok := s.stationReadFilter(w, r, actor)
	if !ok {
		return
	}
	f := incidents.ListFilter{StationIDs: stationFilter}
	if v := r.URL.Query().Get("status"); v != "" {
		if !incidentStatuses[v] {
			writeError(w, http.StatusBadRequest, "invalid status filter")
			return
		}
		f.Status = &v
	}
	if v := r.URL.Query().Get("severity"); v != "" {
		f.Severity = &v
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.incidents.ListPage(r.Context(), actor.TenantID, f, limit+1, offset)
	if err != nil {
		s.logger.Error("list incidents", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]incidentDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toIncidentDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

type createIncidentRequest struct {
	StationID         uuid.UUID  `json:"station_id"`
	RelatedEntityType *string    `json:"related_entity_type,omitempty"`
	RelatedEntityID   *uuid.UUID `json:"related_entity_id,omitempty"`
	Type              string     `json:"type,omitempty"`
	Severity          string     `json:"severity,omitempty"`
	Description       string     `json:"description"`
}

func (s *Server) handleCreateIncident(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createIncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.StationID == uuid.Nil || req.Description == "" {
		writeError(w, http.StatusBadRequest, "station_id and description are required")
		return
	}

	if !s.authorizeStation(w, r, actor, "incidents.manage", req.StationID) {
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

	inc, err := s.incidents.Create(ctx, tx, actor.TenantID, incidents.CreateInput{
		StationID: req.StationID, RelatedEntityType: req.RelatedEntityType,
		RelatedEntityID: req.RelatedEntityID, Type: req.Type, Severity: req.Severity,
		Description: req.Description, OpenedBy: actor.UserID,
	})
	if err != nil {
		s.logger.Error("create incident", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "incident.opened", EventType: "IncidentOpened",
		EntityType: "incident", EntityID: inc.ID.String(),
		NewValue: toIncidentDTO(inc),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("create incident: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toIncidentDTO(inc))
}

type updateIncidentStatusRequest struct {
	Status string `json:"status"`
}

func (s *Server) handleUpdateIncidentStatus(w http.ResponseWriter, r *http.Request) {
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
	var req updateIncidentStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !incidentStatuses[req.Status] {
		writeError(w, http.StatusBadRequest, "status must be open, investigating, resolved, or closed")
		return
	}

	ctx := r.Context()
	before, err := s.incidents.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.authorizeStation(w, r, actor, "incidents.manage", before.StationID) {
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	after, err := s.incidents.UpdateStatus(ctx, tx, actor.TenantID, id, req.Status, actor.UserID)
	if errors.Is(err, incidents.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.logger.Error("update incident status", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// A move into a terminal state is a resolution; everything else is a
	// plain transition.
	action, event := "incident.updated", "IncidentUpdated"
	if (after.Status == "resolved" || after.Status == "closed") &&
		before.Status != "resolved" && before.Status != "closed" {
		action, event = "incident.resolved", "IncidentResolved"
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: action, EventType: event,
		EntityType: "incident", EntityID: after.ID.String(),
		PreviousValue: toIncidentDTO(before), NewValue: toIncidentDTO(after),
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
	writeJSON(w, http.StatusOK, toIncidentDTO(after))
}
