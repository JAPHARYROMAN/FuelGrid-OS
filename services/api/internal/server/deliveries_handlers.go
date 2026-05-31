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
	ID                     uuid.UUID  `json:"id"`
	TenantID               uuid.UUID  `json:"tenant_id"`
	TankID                 uuid.UUID  `json:"tank_id"`
	SupplierRef            *string    `json:"supplier_ref,omitempty"`
	SupplierID             *uuid.UUID `json:"supplier_id,omitempty"`
	PurchaseOrderID        *uuid.UUID `json:"purchase_order_id,omitempty"`
	POLineID               *uuid.UUID `json:"po_line_id,omitempty"`
	VolumeLitres           float64    `json:"volume_litres"`
	DipBeforeLitres        *float64   `json:"dip_before_litres,omitempty"`
	DipAfterLitres         *float64   `json:"dip_after_litres,omitempty"`
	DipVarianceLitres      *float64   `json:"dip_variance_litres,omitempty"`
	LineUnitPrice          *string    `json:"line_unit_price,omitempty"`
	FreightAmount          string     `json:"freight_amount"`
	DutyAmount             string     `json:"duty_amount"`
	LeviesAmount           string     `json:"levies_amount"`
	LandedCostTotal        *string    `json:"landed_cost_total,omitempty"`
	LandedCostPerLitre     *string    `json:"landed_cost_per_litre,omitempty"`
	MatchStatus            string     `json:"match_status"`
	QuantityVarianceLitres *float64   `json:"quantity_variance_litres,omitempty"`
	ReceivedBy             uuid.UUID  `json:"received_by"`
	ReceivedAt             string     `json:"received_at"`
	Notes                  *string    `json:"notes,omitempty"`
}

func toDeliveryDTO(d *inventory.Delivery) deliveryDTO {
	return deliveryDTO{
		ID: d.ID, TenantID: d.TenantID, TankID: d.TankID,
		SupplierRef: d.SupplierRef, SupplierID: d.SupplierID,
		PurchaseOrderID: d.PurchaseOrderID, POLineID: d.POLineID,
		VolumeLitres:    d.VolumeLitres,
		DipBeforeLitres: d.DipBeforeLitres, DipAfterLitres: d.DipAfterLitres,
		DipVarianceLitres: d.DipVarianceLitres,
		LineUnitPrice:     d.LineUnitPrice, FreightAmount: d.FreightAmount,
		DutyAmount: d.DutyAmount, LeviesAmount: d.LeviesAmount,
		LandedCostTotal: d.LandedCostTotal, LandedCostPerLitre: d.LandedCostPerLitre,
		MatchStatus: d.MatchStatus, QuantityVarianceLitres: d.QuantityVarianceLitres,
		ReceivedBy: d.ReceivedBy,
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
		// MD boundary: delivery volume input is still a float (inventory wave
		// owns that). Parse the product's exact-decimal loss tolerance for this
		// advisory dip-mismatch check only.
		dipMismatch = math.Abs(variance) > req.VolumeLitres*dispDecimal(prod.LossTolerancePercent)/100
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
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.inventory.ListDeliveriesForTankPage(r.Context(), actor.TenantID, tankID, limit+1, offset)
	if err != nil {
		s.logger.Error("list tank deliveries", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writePagedDeliveries(w, rows, limit, offset)
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
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.inventory.ListDeliveriesForStationPage(r.Context(), actor.TenantID, stationID, limit+1, offset)
	if err != nil {
		s.logger.Error("list station deliveries", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writePagedDeliveries(w, rows, limit, offset)
}

// writePagedDeliveries trims the limit+1 fetch, maps to DTOs, and writes the
// standard paged envelope with a precise has_more flag.
func (s *Server) writePagedDeliveries(w http.ResponseWriter, rows []inventory.Delivery, limit, offset int) {
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]deliveryDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toDeliveryDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleGetDeliveryReceipt(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid delivery id")
		return
	}
	d, err := s.inventory.GetDelivery(r.Context(), actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "delivery not found")
		return
	}
	if err != nil {
		s.logger.Error("get delivery", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tank, err := s.tanks.Get(r.Context(), actor.TenantID, d.TankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "inventory.read", tank.StationID) {
		return
	}
	writeJSON(w, http.StatusOK, toDeliveryDTO(d))
}
