package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/risk"
)

func (s *Server) handleTuneRiskRule(w http.ResponseWriter, r *http.Request) {
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
		Threshold    string `json:"threshold,omitempty"`
		LookbackDays int    `json:"lookback_days,omitempty"`
		Severity     string `json:"severity,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "risk_rule.tuned", EventType: "RiskRuleTuned", EntityType: "risk_rule", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		if err := s.risk.TuneRule(r.Context(), tx, actor.TenantID, id, req.Threshold, req.LookbackDays, req.Severity); errors.Is(err, risk.ErrNotFound) {
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

func (s *Server) handleCreateSuppression(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		AlertType string     `json:"alert_type"`
		EntityID  *uuid.UUID `json:"entity_id,omitempty"`
		Reason    string     `json:"reason"`
		ExpiresAt string     `json:"expires_at,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AlertType == "" || req.Reason == "" {
		writeError(w, http.StatusBadRequest, "alert_type and reason are required")
		return
	}
	var expires *time.Time
	if req.ExpiresAt != "" {
		if t, perr := time.Parse(dateLayout, req.ExpiresAt); perr == nil {
			expires = &t
		}
	}
	var supID uuid.UUID
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "risk_alert.suppressed", EventType: "RiskAlertSuppressed", EntityType: "risk_suppression",
	}, func(tx pgx.Tx) (string, error) {
		id, err := s.risk.CreateSuppression(r.Context(), tx, actor.TenantID, req.AlertType, req.EntityID, req.Reason, expires, actor.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		supID = id
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": supID})
}

func (s *Server) handleListSuppressions(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.risk.ListSuppressionsPage(r.Context(), actor.TenantID, limit+1, offset)
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

func (s *Server) handlePauseAllRiskRules(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	paused := 0
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "risk_engine.paused", EventType: "RiskEnginePaused", EntityType: "risk_rule", EntityID: actor.TenantID.String(),
	}, func(tx pgx.Tx) (string, error) {
		n, err := s.risk.PauseAllRules(r.Context(), tx, actor.TenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		paused = n
		return actor.TenantID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paused_rules": paused})
}

func (s *Server) handleRiskGovernance(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	summary, err := s.risk.GovernanceSummary(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("risk governance", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
