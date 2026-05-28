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
	"github.com/japharyroman/fuelgrid-os/internal/fleet"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

func statementMap(s *fleet.Statement) map[string]any {
	return map[string]any{
		"id": s.ID, "customer_id": s.CustomerID,
		"period_start": s.PeriodStart.Format(dateLayout), "period_end": s.PeriodEnd.Format(dateLayout),
		"opening_balance": s.OpeningBalance, "charges": s.Charges, "payments": s.Payments,
		"closing_balance": s.ClosingBalance, "status": s.Status,
	}
}

func (s *Server) handleGenerateStatement(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	var req struct {
		PeriodStart string `json:"period_start"`
		PeriodEnd   string `json:"period_end"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	start, serr := time.Parse(dateLayout, req.PeriodStart)
	end, eerr := time.Parse(dateLayout, req.PeriodEnd)
	if serr != nil || eerr != nil {
		writeError(w, http.StatusBadRequest, "period_start and period_end must be YYYY-MM-DD")
		return
	}
	var st *fleet.Statement
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_statement.generated", EventType: "CustomerStatementGenerated", EntityType: "customer_statement",
	}, func(tx pgx.Tx) (string, error) {
		out, err := s.fleet.GenerateStatement(r.Context(), tx, actor.TenantID, customerID, start, end, actor.UserID)
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown customer")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		st = out
		return out.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, statementMap(st))
}

func (s *Server) handleIssueStatement(w http.ResponseWriter, r *http.Request) {
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
	var st *fleet.Statement
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_statement.issued", EventType: "CustomerStatementIssued", EntityType: "customer_statement",
		EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		out, err := s.fleet.IssueStatement(r.Context(), tx, actor.TenantID, id)
		if errors.Is(err, fleet.ErrBadState) {
			writeError(w, http.StatusConflict, "only a draft statement can be issued")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		st = out
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, statementMap(st))
}

func (s *Server) handleListStatements(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	rows, err := s.fleet.ListStatements(r.Context(), actor.TenantID, customerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, statementMap(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// ---- Credit alerts (Stage 13) ----

func (s *Server) handleScanCreditAlerts(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	created := 0
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_credit_alert.scanned", EventType: "CustomerCreditAlertsScanned", EntityType: "customer_credit_alert",
		EntityID: actor.TenantID.String(),
	}, func(tx pgx.Tx) (string, error) {
		n, err := s.fleet.ScanCreditAlerts(r.Context(), tx, actor.TenantID)
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

func (s *Server) handleListCreditAlerts(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.fleet.ListAlerts(r.Context(), actor.TenantID, r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		a := rows[i]
		out = append(out, map[string]any{
			"id": a.ID, "customer_id": a.CustomerID, "alert_type": a.AlertType,
			"severity": a.Severity, "status": a.Status, "detail": a.Detail,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleTransitionCreditAlert(to string) http.HandlerFunc {
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
			Reason *string `json:"reason,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		ok := s.txAudit(w, r, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "customer_credit_alert." + to, EventType: "CustomerCreditAlert", EntityType: "customer_credit_alert",
			EntityID: id.String(),
		}, func(tx pgx.Tx) (string, error) {
			assignee := &actor.UserID
			if err := s.fleet.TransitionAlert(r.Context(), tx, actor.TenantID, id, to, req.Reason, assignee); errors.Is(err, fleet.ErrNotFound) {
				writeError(w, http.StatusNotFound, "alert not found")
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
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": to})
	}
}
