package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/risk"
)

func caseMap(c *risk.Case) map[string]any {
	return map[string]any{
		"id": c.ID, "title": c.Title, "case_type": c.CaseType, "status": c.Status,
		"severity": c.Severity, "assigned_to": c.AssignedTo, "resolution": c.Resolution,
	}
}

func (s *Server) handleCreateCase(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Title    string     `json:"title"`
		CaseType string     `json:"case_type,omitempty"`
		Severity string     `json:"severity,omitempty"`
		AlertID  *uuid.UUID `json:"alert_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	var c *risk.Case
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "investigation.opened", EventType: "InvestigationOpened", EntityType: "investigation_case",
	}, func(tx pgx.Tx) (string, error) {
		out, err := s.risk.CreateCase(r.Context(), tx, actor.TenantID, req.Title, req.CaseType, req.Severity, actor.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		if req.AlertID != nil {
			if err := s.risk.AttachAlert(r.Context(), tx, actor.TenantID, out.ID, *req.AlertID); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return "", err
			}
		}
		c = out
		return out.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, caseMap(c))
}

func (s *Server) handleListCases(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.risk.ListCases(r.Context(), actor.TenantID, r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, caseMap(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleGetCaseTimeline(w http.ResponseWriter, r *http.Request) {
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
	c, err := s.risk.GetCase(r.Context(), actor.TenantID, id)
	if errors.Is(err, risk.ErrNotFound) {
		writeError(w, http.StatusNotFound, "case not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	timeline, err := s.risk.CaseTimeline(r.Context(), actor.TenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	resp := caseMap(c)
	resp["timeline"] = timeline
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAttachAlertToCase(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		AlertID uuid.UUID `json:"alert_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AlertID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "alert_id is required")
		return
	}
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "investigation.evidence_added", EventType: "InvestigationEvidenceAdded", EntityType: "investigation_case",
		EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.risk.AttachAlert(r.Context(), tx, actor.TenantID, id, req.AlertID); err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown case or alert")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"case_id": id, "alert_id": req.AlertID})
}

func (s *Server) handleAddCaseComment(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}
	var commentID uuid.UUID
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "investigation.commented", EventType: "InvestigationCommented", EntityType: "investigation_case",
		EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		cid, err := s.risk.AddComment(r.Context(), tx, actor.TenantID, id, req.Body, actor.UserID)
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown case")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		commentID = cid
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": commentID})
}

func (s *Server) handleAddCaseAction(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		ActionType string `json:"action_type"`
		Detail     string `json:"detail,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ActionType == "" {
		writeError(w, http.StatusBadRequest, "action_type is required")
		return
	}
	var actionID uuid.UUID
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "risk_recommendation.suggested", EventType: "RiskRecommendationSuggested", EntityType: "investigation_case_action",
		EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		aid, err := s.risk.AddAction(r.Context(), tx, actor.TenantID, id, req.ActionType, req.Detail)
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown case")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		actionID = aid
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": actionID})
}

func (s *Server) handleSetCaseActionStatus(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	actionID, err := uuid.Parse(chi.URLParam(r, "actionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid action id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	valid := map[string]bool{"suggested": true, "accepted": true, "completed": true, "dismissed": true}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !valid[req.Status] {
		writeError(w, http.StatusBadRequest, "status must be suggested|accepted|completed|dismissed")
		return
	}
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "risk_recommendation." + req.Status, EventType: "RiskRecommendation", EntityType: "investigation_case_action",
		EntityID: actionID.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.risk.SetActionStatus(r.Context(), tx, actor.TenantID, actionID, req.Status); errors.Is(err, risk.ErrNotFound) {
			writeError(w, http.StatusNotFound, "action not found")
			return "", err
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		return actionID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": actionID, "status": req.Status})
}

func (s *Server) handleSetCaseStatus(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Status     string  `json:"status"`
		Resolution *string `json:"resolution,omitempty"`
	}
	valid := map[string]bool{"open": true, "assigned": true, "in_review": true, "action_required": true, "resolved": true, "closed": true}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !valid[req.Status] {
		writeError(w, http.StatusBadRequest, "invalid case status")
		return
	}
	var c *risk.Case
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "investigation." + req.Status, EventType: "InvestigationStatus", EntityType: "investigation_case", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		assignee := &actor.UserID
		out, err := s.risk.SetCaseStatus(r.Context(), tx, actor.TenantID, id, req.Status, req.Resolution, assignee)
		if errors.Is(err, risk.ErrNotFound) {
			writeError(w, http.StatusNotFound, "case not found")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		c = out
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, caseMap(c))
}
