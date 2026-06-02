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

// ---- Signals (Stage 1) ----

func (s *Server) handleBackfillSignals(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	created := 0
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "risk_signal.backfilled", EventType: "RiskSignalBackfilled", EntityType: "risk_signal",
		EntityID: actor.TenantID.String(),
	}, func(tx pgx.Tx) (string, error) {
		n, err := s.risk.BackfillSignals(r.Context(), tx, actor.TenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		created = n
		return actor.TenantID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": created})
}

func (s *Server) handleListSignals(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.risk.ListSignalsPage(r.Context(), actor.TenantID, r.URL.Query().Get("type"), limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	writePagedMore(w, http.StatusOK, rows, len(rows), limit, offset, hasMore)
}

// ---- Rules (Stage 2) ----

func (s *Server) handleCreateRiskRule(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Code                 string `json:"code"`
		Name                 string `json:"name"`
		RuleType             string `json:"rule_type,omitempty"`
		Category             string `json:"category,omitempty"`
		Condition            string `json:"condition,omitempty"`
		Severity             string `json:"severity,omitempty"`
		Description          string `json:"description,omitempty"`
		MessageTemplate      string `json:"message_template,omitempty"`
		RecommendedAction    string `json:"recommended_action,omitempty"`
		Threshold            string `json:"threshold,omitempty"`
		LookbackDays         int    `json:"lookback_days,omitempty"`
		ComparisonPeriodDays int    `json:"comparison_period_days,omitempty"`
		Status               string `json:"status,omitempty"`
		Enabled              *bool  `json:"enabled,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "code and name are required")
		return
	}
	var ruleID uuid.UUID
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "risk_rule.created", EventType: "RiskRuleCreated", EntityType: "risk_rule",
	}, func(tx pgx.Tx) (string, error) {
		id, err := s.risk.CreateRule(r.Context(), tx, actor.TenantID, risk.RuleInput{
			Code: req.Code, Name: req.Name, RuleType: req.RuleType, Category: req.Category,
			Condition: req.Condition, Severity: req.Severity, Description: req.Description,
			MessageTemplate: req.MessageTemplate, RecommendedAction: req.RecommendedAction,
			Threshold: req.Threshold, LookbackDays: req.LookbackDays,
			ComparisonPeriodDays: req.ComparisonPeriodDays, Status: req.Status, Enabled: req.Enabled,
		})
		if err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "a rule with this code already exists")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		ruleID = id
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": ruleID})
}

func (s *Server) handleListRiskRules(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.risk.ListRulesPage(r.Context(), actor.TenantID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	writePagedMore(w, http.StatusOK, rows, len(rows), limit, offset, hasMore)
}

func (s *Server) handleSetRiskRuleStatus(w http.ResponseWriter, r *http.Request) {
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
		Status string `json:"status"`
	}
	valid := map[string]bool{"draft": true, "active": true, "paused": true, "retired": true}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !valid[req.Status] {
		writeError(w, http.StatusBadRequest, "status must be draft|active|paused|retired")
		return
	}
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "risk_rule." + req.Status, EventType: "RiskRuleStatus", EntityType: "risk_rule", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.risk.SetRuleStatus(r.Context(), tx, actor.TenantID, id, req.Status); errors.Is(err, risk.ErrNotFound) {
			writeError(w, http.StatusNotFound, "rule not found")
			return "", err
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": req.Status})
}

func (s *Server) handleUpdateRiskRule(w http.ResponseWriter, r *http.Request) {
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
		Name                 string `json:"name,omitempty"`
		RuleType             string `json:"rule_type,omitempty"`
		Category             string `json:"category,omitempty"`
		Condition            string `json:"condition,omitempty"`
		Severity             string `json:"severity,omitempty"`
		Description          string `json:"description,omitempty"`
		MessageTemplate      string `json:"message_template,omitempty"`
		RecommendedAction    string `json:"recommended_action,omitempty"`
		Threshold            string `json:"threshold,omitempty"`
		ComparisonPeriodDays int    `json:"comparison_period_days,omitempty"`
		Status               string `json:"status,omitempty"`
		Enabled              *bool  `json:"enabled,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "risk_rule.updated", EventType: "RiskRuleUpdated", EntityType: "risk_rule", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.risk.UpdateRule(r.Context(), tx, actor.TenantID, id, risk.RuleInput{
			Name: req.Name, RuleType: req.RuleType, Category: req.Category, Condition: req.Condition,
			Severity: req.Severity, Description: req.Description, MessageTemplate: req.MessageTemplate,
			RecommendedAction: req.RecommendedAction, Threshold: req.Threshold,
			ComparisonPeriodDays: req.ComparisonPeriodDays, Status: req.Status, Enabled: req.Enabled,
		}); errors.Is(err, risk.ErrNotFound) {
			writeError(w, http.StatusNotFound, "rule not found")
			return "", err
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handleSetRiskRuleEnabled(w http.ResponseWriter, r *http.Request) {
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
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "risk_rule.enabled", EventType: "RiskRuleEnabled", EntityType: "risk_rule", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.risk.SetRuleEnabled(r.Context(), tx, actor.TenantID, id, *req.Enabled); errors.Is(err, risk.ErrNotFound) {
			writeError(w, http.StatusNotFound, "rule not found")
			return "", err
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": *req.Enabled})
}

// ---- Detection + alerts (Stages 3-8) ----

func (s *Server) handleRunDetection(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	created := 0
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "risk.detection_run", EventType: "RiskDetectionRun", EntityType: "risk_alert",
		EntityID: actor.TenantID.String(),
	}, func(tx pgx.Tx) (string, error) {
		n, err := s.risk.RunDetection(r.Context(), tx, actor.TenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		created = n
		return actor.TenantID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts_created": created})
}

func alertMap(a *risk.Alert) map[string]any {
	return map[string]any{
		"id": a.ID, "rule_code": a.RuleCode, "rule_id": a.RuleID, "alert_type": a.AlertType, "severity": a.Severity,
		"status": a.Status, "station_id": a.StationID, "subject_type": a.SubjectType, "subject_id": a.SubjectID,
		"detail": a.Detail, "amount": a.Amount, "recommended_action": a.RecommendedAction, "score": a.Score,
	}
}

func (s *Server) handleListRiskAlerts(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.risk.ListAlertsPage(r.Context(), actor.TenantID, r.URL.Query().Get("status"), r.URL.Query().Get("type"), limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, alertMap(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleGetRiskAlert(w http.ResponseWriter, r *http.Request) {
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
	a, err := s.risk.GetAlert(r.Context(), actor.TenantID, id)
	if errors.Is(err, risk.ErrNotFound) {
		writeError(w, http.StatusNotFound, "alert not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, alertMap(a))
}

func (s *Server) handleTransitionRiskAlert(to string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
			Disposition *string `json:"disposition,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var a *risk.Alert
		ok := s.txAudit(w, r, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "risk_alert." + to, EventType: "RiskAlertTransition", EntityType: "risk_alert", EntityID: id.String(),
		}, func(tx pgx.Tx) (string, error) {
			assignee := &actor.UserID
			out, err := s.risk.TransitionAlert(r.Context(), tx, actor.TenantID, id, to, req.Disposition, assignee)
			if errors.Is(err, risk.ErrNotFound) {
				writeError(w, http.StatusNotFound, "alert not found")
				return "", err
			}
			if errors.Is(err, risk.ErrBadState) {
				writeError(w, http.StatusConflict, "alert cannot transition to "+to+" from its current status")
				return "", err
			}
			if errors.Is(err, risk.ErrDispositionRequired) {
				writeError(w, http.StatusUnprocessableEntity, "a disposition is required to resolve or dismiss an alert")
				return "", err
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return "", err
			}
			a = out
			return id.String(), nil
		})
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, alertMap(a))
	}
}
