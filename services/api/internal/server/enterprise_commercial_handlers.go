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
	"github.com/japharyroman/fuelgrid-os/internal/enterprise"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

func rolloutMap(p *enterprise.PriceRollout) map[string]any {
	return map[string]any{
		"id": p.ID, "product_id": p.ProductID, "scope_type": p.ScopeType, "scope_id": p.ScopeID,
		"unit_price": p.UnitPrice, "effective_from": p.EffectiveFrom.Format(dateLayout),
		"status": p.Status, "stations_applied": p.StationsApplied,
	}
}

// ---- Central pricing (Stage 7) ----

func (s *Server) handleCreatePriceRollout(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		ProductID     uuid.UUID  `json:"product_id"`
		ScopeType     string     `json:"scope_type"`
		ScopeID       *uuid.UUID `json:"scope_id,omitempty"`
		UnitPrice     string     `json:"unit_price"`
		EffectiveFrom string     `json:"effective_from,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProductID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "product_id and unit_price are required")
		return
	}
	if v, ok := parseDecimal(req.UnitPrice); !ok || v < 0 {
		writeError(w, http.StatusBadRequest, "unit_price must be a non-negative decimal")
		return
	}
	from := time.Now()
	if req.EffectiveFrom != "" {
		if t, derr := time.Parse(dateLayout, req.EffectiveFrom); derr == nil {
			from = t
		}
	}
	var p *enterprise.PriceRollout
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "central_price_rollout.created", EventType: "CentralPriceRolloutCreated", EntityType: "central_price_rollout",
	}, func(tx pgx.Tx) (string, error) {
		out, err := s.enterprise.CreatePriceRollout(r.Context(), tx, actor.TenantID, req.ProductID, req.ScopeType, req.ScopeID, req.UnitPrice, from, actor.UserID)
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown product")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		p = out
		return out.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, rolloutMap(p))
}

func (s *Server) handleListPriceRollouts(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.enterprise.ListPriceRollouts(r.Context(), actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, rolloutMap(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleApprovePriceRollout(w http.ResponseWriter, r *http.Request) {
	s.rolloutTransition(w, r, "approve")
}

func (s *Server) handleActivatePriceRollout(w http.ResponseWriter, r *http.Request) {
	s.rolloutTransition(w, r, "activate")
}

func (s *Server) rolloutTransition(w http.ResponseWriter, r *http.Request, action string) {
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
	var p *enterprise.PriceRollout
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "central_price_rollout." + action, EventType: "CentralPriceRollout", EntityType: "central_price_rollout",
		EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		var out *enterprise.PriceRollout
		var err error
		if action == "approve" {
			out, err = s.enterprise.ApprovePriceRollout(r.Context(), tx, actor.TenantID, id)
		} else {
			out, err = s.enterprise.ActivatePriceRollout(r.Context(), tx, actor.TenantID, id, actor.UserID)
		}
		if errors.Is(err, enterprise.ErrBadState) {
			writeError(w, http.StatusConflict, "rollout cannot transition from its current state")
			return "", err
		}
		if errors.Is(err, enterprise.ErrNotFound) {
			writeError(w, http.StatusNotFound, "rollout not found")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		p = out
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, rolloutMap(p))
}

// ---- Central procurement (Stage 8) ----

func (s *Server) handleCreateProcurementPlan(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Name  string `json:"name"`
		Lines []struct {
			StationID    uuid.UUID `json:"station_id"`
			ProductID    uuid.UUID `json:"product_id"`
			TargetLitres string    `json:"target_litres"`
		} `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	var planID uuid.UUID
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "central_procurement_plan.created", EventType: "CentralProcurementPlanCreated", EntityType: "central_procurement_plan",
	}, func(tx pgx.Tx) (string, error) {
		id, err := s.enterprise.CreatePlan(r.Context(), tx, actor.TenantID, req.Name, actor.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		for _, ln := range req.Lines {
			if _, err := s.enterprise.AddPlanLine(r.Context(), tx, actor.TenantID, id, ln.StationID, ln.ProductID, ln.TargetLitres); err != nil {
				if isForeignKeyViolation(err) {
					writeError(w, http.StatusBadRequest, "unknown station or product")
					return "", err
				}
				writeError(w, http.StatusInternalServerError, "internal error")
				return "", err
			}
		}
		planID = id
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": planID})
}

func (s *Server) handleListProcurementPlans(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.enterprise.ListPlans(r.Context(), actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
}

func (s *Server) handleReleaseProcurementPlan(w http.ResponseWriter, r *http.Request) {
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
	released := 0
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "central_procurement_plan.released", EventType: "CentralProcurementPlanReleased", EntityType: "central_procurement_plan",
		EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		n, err := s.enterprise.ReleasePlan(r.Context(), tx, actor.TenantID, id)
		if errors.Is(err, enterprise.ErrBadState) {
			writeError(w, http.StatusConflict, "plan cannot be released from its current state")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		released = n
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "released_lines": released})
}

// ---- Stock transfers (Stage 9) ----

func transferMap(t *enterprise.Transfer) map[string]any {
	return map[string]any{
		"id": t.ID, "from_tank_id": t.FromTankID, "to_tank_id": t.ToTankID,
		"product_id": t.ProductID, "litres": t.Litres, "status": t.Status,
	}
}

func (s *Server) handleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		FromTankID uuid.UUID `json:"from_tank_id"`
		ToTankID   uuid.UUID `json:"to_tank_id"`
		ProductID  uuid.UUID `json:"product_id"`
		Litres     string    `json:"litres"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FromTankID == uuid.Nil || req.ToTankID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "from_tank_id, to_tank_id, product_id, litres are required")
		return
	}
	if v, ok := parseDecimal(req.Litres); !ok || v <= 0 {
		writeError(w, http.StatusBadRequest, "litres must be a positive decimal")
		return
	}
	var t *enterprise.Transfer
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "stock_transfer.created", EventType: "StockTransferCreated", EntityType: "stock_transfer_order",
	}, func(tx pgx.Tx) (string, error) {
		out, err := s.enterprise.CreateTransfer(r.Context(), tx, actor.TenantID, req.FromTankID, req.ToTankID, req.ProductID, req.Litres, actor.UserID)
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown tank or product")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		t = out
		return out.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, transferMap(t))
}

func (s *Server) handleListTransfers(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.enterprise.ListTransfers(r.Context(), actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, transferMap(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleApproveTransfer(w http.ResponseWriter, r *http.Request) {
	s.transferTransition(w, r, "approve")
}

func (s *Server) handleReceiveTransfer(w http.ResponseWriter, r *http.Request) {
	s.transferTransition(w, r, "receive")
}

func (s *Server) transferTransition(w http.ResponseWriter, r *http.Request, action string) {
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
	var t *enterprise.Transfer
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "stock_transfer." + action, EventType: "StockTransfer", EntityType: "stock_transfer_order", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		var out *enterprise.Transfer
		var err error
		if action == "approve" {
			out, err = s.enterprise.ApproveTransfer(r.Context(), tx, actor.TenantID, id)
		} else {
			out, err = s.enterprise.ReceiveTransfer(r.Context(), tx, actor.TenantID, id, actor.UserID)
		}
		if errors.Is(err, enterprise.ErrBadState) {
			writeError(w, http.StatusConflict, "transfer cannot transition from its current state")
			return "", err
		}
		if errors.Is(err, enterprise.ErrInsufficientStock) {
			writeError(w, http.StatusUnprocessableEntity, "source tank has insufficient stock")
			return "", err
		}
		if errors.Is(err, enterprise.ErrNotFound) {
			writeError(w, http.StatusNotFound, "transfer not found")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		t = out
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, transferMap(t))
}
