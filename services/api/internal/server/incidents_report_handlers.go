package server

// Attendant issue reporting (Mobile Attendant App Phase 7, PRD §6.12). A
// holder of the station-scoped incidents.report permission (attendant tier)
// opens an incident SELF-SCOPED: the incident's station is the station of the
// actor's own current shift, derived server-side — never trusted from client
// input. The supervisor-tier incidents.manage path (POST /incidents) is
// untouched.
//
// The mobile offline queue replays creations, so the request accepts an
// optional client dedupe_key: a replay carrying the same key returns the
// already-opened incident (200) instead of duplicating it (201), and skips the
// audit/outbox side effects — exactly the payments idempotency pattern (0096).

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/incidents"
)

// reportIncidentTypes is the PRD §6.12 attendant issue vocabulary (allowed by
// the 0103 CHECK alongside the pre-existing incident types). The self-service
// path accepts exactly these; the manage path keeps its wider vocabulary.
var reportIncidentTypes = map[string]bool{
	"pump": true, "nozzle": true, "meter": true, "payment": true,
	"safety": true, "other": true,
}

var incidentSeverities = map[string]bool{
	"low": true, "medium": true, "high": true, "critical": true,
}

type reportIncidentRequest struct {
	Type        string `json:"type"`
	Severity    string `json:"severity,omitempty"`
	Description string `json:"description"`
	// StationID is OPTIONAL and never trusted: when supplied it must match the
	// server-derived station of the actor's current shift (else 403). It exists
	// only so an offline client can assert which station it thought it was at.
	StationID         *uuid.UUID `json:"station_id,omitempty"`
	RelatedEntityType *string    `json:"related_entity_type,omitempty"`
	RelatedEntityID   *uuid.UUID `json:"related_entity_id,omitempty"`
	// DedupeKey is the offline-replay idempotency key (recommended: one UUID
	// per queued action). A replayed create returns the existing incident.
	DedupeKey *string `json:"dedupe_key,omitempty"`
}

// handleReportIncident is POST /incidents/report — the attendant self-service
// creation path (incidents.report, station-scoped, gated at the route).
func (s *Server) handleReportIncident(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req reportIncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Description = strings.TrimSpace(req.Description)
	if req.Description == "" {
		writeError(w, http.StatusBadRequest, "description is required")
		return
	}
	if !reportIncidentTypes[req.Type] {
		writeError(w, http.StatusBadRequest, "type must be pump, nozzle, meter, payment, safety, or other")
		return
	}
	if req.Severity != "" && !incidentSeverities[req.Severity] {
		writeError(w, http.StatusBadRequest, "severity must be low, medium, high, or critical")
		return
	}
	if req.DedupeKey != nil {
		k := strings.TrimSpace(*req.DedupeKey)
		if k == "" || len(k) > 255 {
			writeError(w, http.StatusBadRequest, "dedupe_key must be 1-255 characters")
			return
		}
		req.DedupeKey = &k
	}

	ctx := r.Context()
	// SELF-SCOPE: the incident's station is the station of the actor's own
	// current shift (open, or closed pending approval) — derived server-side.
	shift, err := s.operations.ActiveShiftForAttendant(ctx, actor.TenantID, actor.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":  "you are not on an active shift; issues are reported from your current shift",
			"code":   "no_active_shift",
			"status": http.StatusConflict,
		})
		return
	}
	if err != nil {
		s.logger.Error("report incident: active shift", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// A client-asserted station that disagrees with the derived one is refused
	// outright — the cross-station guard the offline queue can hit when a
	// queued report is replayed after the attendant moved.
	if req.StationID != nil && *req.StationID != shift.StationID {
		writeError(w, http.StatusForbidden, "incident station must be the station of your current shift")
		return
	}
	// The station-scoped permission must hold AT the derived station, mirroring
	// every other station-scoped write.
	if !s.authorizeStation(w, r, actor, "incidents.report", shift.StationID) {
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	res, err := s.incidents.CreateDeduped(ctx, tx, actor.TenantID, incidents.CreateInput{
		StationID:         shift.StationID,
		RelatedEntityType: req.RelatedEntityType,
		RelatedEntityID:   req.RelatedEntityID,
		Type:              req.Type,
		Severity:          req.Severity,
		Description:       req.Description,
		OpenedBy:          actor.UserID, // the reporter, recorded
		DedupeKey:         req.DedupeKey,
	})
	if err != nil {
		s.logger.Error("report incident", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if res.Replayed {
		// Offline-queue replay: the incident (and its audit/outbox trail)
		// already exists — answer idempotently without re-applying side effects.
		writeJSON(w, http.StatusOK, toIncidentDTO(res.Incident))
		return
	}

	// Same audit action + event type as the manage path: IncidentOpened already
	// raises the supervisor-facing (tenant-wide, critical) notification via the
	// subscriber, so reported issues land in the same queue and feed.
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "incident.opened", EventType: "IncidentOpened",
		EntityType: "incident", EntityID: res.Incident.ID.String(),
		NewValue: toIncidentDTO(res.Incident),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("report incident: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toIncidentDTO(res.Incident))
}
