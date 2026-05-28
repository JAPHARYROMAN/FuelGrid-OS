package server

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/inventory"
)

type deliveryDTO struct {
	ID                uuid.UUID `json:"id"`
	TenantID          uuid.UUID `json:"tenant_id"`
	TankID            uuid.UUID `json:"tank_id"`
	SupplierRef       *string   `json:"supplier_ref,omitempty"`
	VolumeLitres      float64   `json:"volume_litres"`
	DipBeforeLitres   *float64  `json:"dip_before_litres,omitempty"`
	DipAfterLitres    *float64  `json:"dip_after_litres,omitempty"`
	DipVarianceLitres *float64  `json:"dip_variance_litres,omitempty"`
	ReceivedBy        uuid.UUID `json:"received_by"`
	ReceivedAt        string    `json:"received_at"`
	Notes             *string   `json:"notes,omitempty"`
}

func toDeliveryDTO(d *inventory.Delivery) deliveryDTO {
	return deliveryDTO{
		ID: d.ID, TenantID: d.TenantID, TankID: d.TankID,
		SupplierRef: d.SupplierRef, VolumeLitres: d.VolumeLitres,
		DipBeforeLitres: d.DipBeforeLitres, DipAfterLitres: d.DipAfterLitres,
		DipVarianceLitres: d.DipVarianceLitres, ReceivedBy: d.ReceivedBy,
		ReceivedAt: d.ReceivedAt.Format(time.RFC3339), Notes: d.Notes,
	}
}

type receiveDeliveryRequest struct {
	SupplierRef     *string  `json:"supplier_ref,omitempty"`
	VolumeLitres    float64  `json:"volume_litres"`
	DipBeforeLitres *float64 `json:"dip_before_litres,omitempty"`
	DipAfterLitres  *float64 `json:"dip_after_litres,omitempty"`
	Notes           *string  `json:"notes,omitempty"`
}

func (s *Server) handleReceiveDelivery(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid tank id")
		return
	}
	var req receiveDeliveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.VolumeLitres <= 0 {
		writeError(w, http.StatusBadRequest, "volume_litres must be positive")
		return
	}
	if (req.DipBeforeLitres != nil && *req.DipBeforeLitres < 0) || (req.DipAfterLitres != nil && *req.DipAfterLitres < 0) {
		writeError(w, http.StatusBadRequest, "dip volumes must be non-negative")
		return
	}

	ctx := r.Context()
	tank, err := s.tanks.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tank not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "delivery.receive", tank.StationID) {
		return
	}

	// Cross-check: when both dips are present, compare the declared volume to
	// the measured level change and flag a mismatch against the product's loss
	// tolerance.
	var dipVariance *float64
	var dipMismatch bool
	if req.DipBeforeLitres != nil && req.DipAfterLitres != nil {
		variance := req.VolumeLitres - (*req.DipAfterLitres - *req.DipBeforeLitres)
		dipVariance = &variance
		prod, err := s.products.Get(ctx, actor.TenantID, tank.ProductID)
		if err != nil {
			s.logger.Error("delivery: product lookup", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		dipMismatch = math.Abs(variance) > req.VolumeLitres*prod.LossTolerancePercent/100
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	delivery, movement, err := s.inventory.ReceiveDelivery(ctx, tx, actor.TenantID, inventory.ReceiveInput{
		TankID: tank.ID, SupplierRef: req.SupplierRef, VolumeLitres: req.VolumeLitres,
		DipBeforeLitres: req.DipBeforeLitres, DipAfterLitres: req.DipAfterLitres,
		DipVarianceLitres: dipVariance, ReceivedBy: actor.UserID, Notes: req.Notes,
	})
	if errors.Is(err, inventory.ErrNoOpeningBalance) {
		writeError(w, http.StatusConflict, "tank has no opening balance; set one before receiving deliveries")
		return
	}
	if err != nil {
		s.logger.Error("receive delivery", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "delivery.received", EventType: "DeliveryReceived",
		EntityType: "delivery", EntityID: delivery.ID.String(),
		NewValue: toDeliveryDTO(delivery),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("receive delivery: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"delivery":     toDeliveryDTO(delivery),
		"movement":     toStockMovementDTO(movement),
		"dip_mismatch": dipMismatch,
	})
}

func (s *Server) handleListTankDeliveries(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tankID, ok := s.tankForInventoryRead(w, r, actor)
	if !ok {
		return
	}
	rows, err := s.inventory.ListDeliveriesForTank(r.Context(), actor.TenantID, tankID)
	if err != nil {
		s.logger.Error("list tank deliveries", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, deliveryListResponse(rows))
}

func (s *Server) handleListStationDeliveries(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	rows, err := s.inventory.ListDeliveriesForStation(r.Context(), actor.TenantID, stationID)
	if err != nil {
		s.logger.Error("list station deliveries", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, deliveryListResponse(rows))
}

func deliveryListResponse(rows []inventory.Delivery) map[string]any {
	out := make([]deliveryDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toDeliveryDTO(&rows[i]))
	}
	return map[string]any{"items": out, "count": len(out)}
}
