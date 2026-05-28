package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/fleet"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

func authorizationMap(a *fleet.Authorization) map[string]any {
	return map[string]any{
		"id": a.ID, "customer_id": a.CustomerID, "vehicle_id": a.VehicleID, "driver_id": a.DriverID,
		"credential_id": a.CredentialID, "station_id": a.StationID, "product_id": a.ProductID,
		"requested_amount": a.RequestedAmount, "approved_amount": a.ApprovedAmount, "odometer": a.Odometer,
		"status": a.Status, "source": a.Source, "consumed_by": a.ConsumedBy,
	}
}

func (s *Server) handleRequestAuthorization(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		CustomerID      uuid.UUID  `json:"customer_id"`
		VehicleID       *uuid.UUID `json:"vehicle_id,omitempty"`
		DriverID        *uuid.UUID `json:"driver_id,omitempty"`
		CredentialID    *uuid.UUID `json:"credential_id,omitempty"`
		StationID       uuid.UUID  `json:"station_id"`
		ProductID       *uuid.UUID `json:"product_id,omitempty"`
		RequestedAmount string     `json:"requested_amount"`
		Odometer        *string    `json:"odometer,omitempty"`
		Source          string     `json:"source,omitempty"`
		Override        bool       `json:"override,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == uuid.Nil || req.StationID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "customer_id and station_id are required")
		return
	}
	if v, ok := parseDecimal(req.RequestedAmount); !ok || v <= 0 {
		writeError(w, http.StatusBadRequest, "requested_amount must be a positive decimal")
		return
	}
	if req.Override && !s.actorHolds(r.Context(), actor, "fuel_authorization.override") {
		writeError(w, http.StatusForbidden, "override requires fuel_authorization.override")
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	auth, decision, err := s.fleet.RequestAuthorization(ctx, tx, actor.TenantID, fleet.AuthRequest{
		CustomerID: req.CustomerID, VehicleID: req.VehicleID, DriverID: req.DriverID, CredentialID: req.CredentialID,
		StationID: req.StationID, ProductID: req.ProductID, RequestedAmount: req.RequestedAmount,
		Odometer: req.Odometer, Source: req.Source,
	}, actor.UserID, req.Override)
	if errors.Is(err, fleet.ErrDenied) {
		// Persist the denial (committed) before returning the explanation.
		_ = tx.Commit(ctx)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "authorization denied", "rule_code": decision.RuleCode, "detail": decision.Detail,
		})
		return
	}
	if err != nil {
		if isForeignKeyViolation(err) {
			writeError(w, http.StatusBadRequest, "unknown customer, station, vehicle, driver, or credential")
			return
		}
		s.logger.Error("request authorization", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "fuel_authorization.approved", EventType: "FuelAuthorizationApproved", EntityType: "fuel_authorization",
		EntityID: auth.ID.String(), NewValue: map[string]any{"approved_amount": auth.ApprovedAmount, "override": req.Override},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, authorizationMap(auth))
}

func (s *Server) handleListAuthorizations(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.fleet.ListAuthorizations(r.Context(), actor.TenantID, queryUUID(r, "customer_id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, authorizationMap(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleGetAuthorization(w http.ResponseWriter, r *http.Request) {
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
	a, err := s.fleet.GetAuthorization(r.Context(), actor.TenantID, id)
	if errors.Is(err, fleet.ErrNotFound) {
		writeError(w, http.StatusNotFound, "authorization not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, authorizationMap(a))
}

// handleFulfillAuthorization links an approved authorization to its Phase-6
// sale exactly once (Stage 9 consumption).
func (s *Server) handleFulfillAuthorization(w http.ResponseWriter, r *http.Request) {
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
		ConsumedBy uuid.UUID `json:"consumed_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ConsumedBy == uuid.Nil {
		writeError(w, http.StatusBadRequest, "consumed_by (sale id) is required")
		return
	}
	var auth *fleet.Authorization
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "fuel_authorization.fulfilled", EventType: "FuelAuthorizationFulfilled", EntityType: "fuel_authorization",
		EntityID: id.String(), NewValue: map[string]any{"consumed_by": req.ConsumedBy},
	}, func(tx pgx.Tx) (string, error) {
		a, err := s.fleet.FulfillAuthorization(r.Context(), tx, actor.TenantID, id, req.ConsumedBy)
		if errors.Is(err, fleet.ErrConsumed) {
			writeError(w, http.StatusConflict, "authorization is not approved or already consumed")
			return "", err
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		auth = a
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, authorizationMap(auth))
}

func (s *Server) handleAuthorizationStatus(to string) http.HandlerFunc {
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
		var auth *fleet.Authorization
		ok := s.txAudit(w, r, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "fuel_authorization." + to, EventType: "FuelAuthorizationStatus", EntityType: "fuel_authorization",
			EntityID: id.String(),
		}, func(tx pgx.Tx) (string, error) {
			a, err := s.fleet.SetAuthorizationStatus(r.Context(), tx, actor.TenantID, id, to)
			if errors.Is(err, fleet.ErrBadState) {
				writeError(w, http.StatusConflict, "authorization cannot transition from its current state")
				return "", err
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return "", err
			}
			auth = a
			return id.String(), nil
		})
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, authorizationMap(auth))
	}
}

// ---- Fuel limits ----

func (s *Server) handleListFuelLimits(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.fleet.ListLimits(r.Context(), actor.TenantID, queryUUID(r, "customer_id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
}

func (s *Server) handleCreateFuelLimit(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		CustomerID *uuid.UUID `json:"customer_id,omitempty"`
		VehicleID  *uuid.UUID `json:"vehicle_id,omitempty"`
		ProductID  *uuid.UUID `json:"product_id,omitempty"`
		Scope      string     `json:"scope,omitempty"`
		Period     string     `json:"period,omitempty"`
		MaxAmount  *string    `json:"max_amount,omitempty"`
		MaxLitres  *string    `json:"max_litres,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var limitID uuid.UUID
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "fuel_limit.created", EventType: "FuelLimitCreated", EntityType: "fuel_limit",
	}, func(tx pgx.Tx) (string, error) {
		id, err := s.fleet.CreateLimit(r.Context(), tx, actor.TenantID, req.CustomerID, req.VehicleID, req.ProductID, req.Scope, req.Period, req.MaxAmount, req.MaxLitres)
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown customer or vehicle")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		limitID = id
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": limitID})
}
