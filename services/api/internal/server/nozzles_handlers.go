package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/nozzles"
)

type nozzleDTO struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	StationID uuid.UUID `json:"station_id"`
	PumpID    uuid.UUID `json:"pump_id"`
	TankID    uuid.UUID `json:"tank_id"`
	ProductID uuid.UUID `json:"product_id"`
	Number    int       `json:"number"`
	// DefaultPrice is an exact decimal STRING (numeric(14,2)).
	DefaultPrice       string `json:"default_price"`
	MeterDecimalPlaces int    `json:"meter_decimal_places"`
	Status             string `json:"status"`
}

func toNozzleDTO(n *nozzles.Nozzle) nozzleDTO {
	return nozzleDTO{
		ID: n.ID, TenantID: n.TenantID, StationID: n.StationID,
		PumpID: n.PumpID, TankID: n.TankID, ProductID: n.ProductID,
		Number: n.Number, DefaultPrice: n.DefaultPrice,
		MeterDecimalPlaces: n.MeterDecimalPlaces, Status: n.Status,
	}
}

func (s *Server) handleListNozzles(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	filter, ok := s.stationReadFilter(w, r, actor)
	if !ok {
		return
	}
	var pumpID *uuid.UUID
	if v := r.URL.Query().Get("pump_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid pump_id")
			return
		}
		pumpID = &id
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.nozzles.ListPage(r.Context(), actor.TenantID, filter, pumpID, limit+1, offset)
	if err != nil {
		s.logger.Error("list nozzles", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]nozzleDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toNozzleDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

type createNozzleRequest struct {
	PumpID             uuid.UUID    `json:"pump_id"`
	TankID             uuid.UUID    `json:"tank_id"`
	Number             int          `json:"number"`
	DefaultPrice       decimalInput `json:"default_price,omitempty"`
	MeterDecimalPlaces *int         `json:"meter_decimal_places,omitempty"`
}

func (s *Server) handleCreateNozzle(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createNozzleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.PumpID == uuid.Nil || req.TankID == uuid.Nil || req.Number <= 0 {
		writeError(w, http.StatusBadRequest, "pump_id, tank_id, and a positive number are required")
		return
	}
	if req.DefaultPrice.Set() && !req.DefaultPrice.Valid() {
		writeError(w, http.StatusBadRequest, "default_price must be a non-negative decimal")
		return
	}
	meterDP := 2
	if req.MeterDecimalPlaces != nil {
		meterDP = *req.MeterDecimalPlaces
	}
	if meterDP < 0 || meterDP > 4 {
		writeError(w, http.StatusBadRequest, "meter_decimal_places must be between 0 and 4")
		return
	}

	ctx := r.Context()

	// Resolve the pump (gives us the station + authorization target).
	pump, err := s.pumps.Get(ctx, actor.TenantID, req.PumpID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "pump not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.authorizeStation(w, r, actor, "pumps.manage", pump.StationID) {
		return
	}

	// Resolve the tank; its station must match the pump's, and its product
	// is what the nozzle dispenses (the DB FKs enforce this too).
	tank, err := s.tanks.Get(ctx, actor.TenantID, req.TankID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tank not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if tank.StationID != pump.StationID {
		writeError(w, http.StatusBadRequest, "tank and pump must be at the same station")
		return
	}

	// Default price (exact decimal string) falls back to the product's default,
	// then to "0". No float ever touches the price.
	price := "0"
	if req.DefaultPrice.Set() {
		price = req.DefaultPrice.String()
	} else if product, err := s.products.Get(ctx, actor.TenantID, tank.ProductID); err == nil {
		price = product.DefaultPrice
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	n, err := s.nozzles.Create(ctx, tx, actor.TenantID, nozzles.CreateInput{
		StationID: pump.StationID, PumpID: pump.ID, TankID: tank.ID,
		ProductID: tank.ProductID, Number: req.Number,
		DefaultPrice: price, MeterDecimalPlaces: meterDP,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a nozzle with that number already exists on this pump")
		return
	}
	if err != nil {
		s.logger.Error("create nozzle", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "nozzle.created", EventType: "NozzleCreated",
		EntityType: "nozzle", EntityID: n.ID.String(),
		NewValue: toNozzleDTO(n),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("create nozzle: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toNozzleDTO(n))
}

type updateNozzleRequest struct {
	TankID             *uuid.UUID   `json:"tank_id,omitempty"`
	Number             *int         `json:"number,omitempty"`
	DefaultPrice       decimalInput `json:"default_price,omitempty"`
	MeterDecimalPlaces *int         `json:"meter_decimal_places,omitempty"`
	Status             *string      `json:"status,omitempty"`
}

func (s *Server) handleUpdateNozzle(w http.ResponseWriter, r *http.Request) {
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
	var req updateNozzleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Number != nil && *req.Number <= 0 {
		writeError(w, http.StatusBadRequest, "number must be positive")
		return
	}
	if req.MeterDecimalPlaces != nil && (*req.MeterDecimalPlaces < 0 || *req.MeterDecimalPlaces > 4) {
		writeError(w, http.StatusBadRequest, "meter_decimal_places must be between 0 and 4")
		return
	}
	if req.DefaultPrice.Set() && !req.DefaultPrice.Valid() {
		writeError(w, http.StatusBadRequest, "default_price must be a non-negative decimal")
		return
	}

	ctx := r.Context()
	before, err := s.nozzles.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.authorizeStation(w, r, actor, "pumps.manage", before.StationID) {
		return
	}

	in := nozzles.UpdateInput{
		Number: req.Number, DefaultPrice: req.DefaultPrice.Ptr(),
		MeterDecimalPlaces: req.MeterDecimalPlaces, Status: req.Status,
	}

	// A tank reassignment re-derives the product from the new tank and keeps
	// the nozzle on its current station.
	if req.TankID != nil {
		tank, err := s.tanks.Get(ctx, actor.TenantID, *req.TankID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "tank not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if tank.StationID != before.StationID {
			writeError(w, http.StatusBadRequest, "the new tank must be at the nozzle's station")
			return
		}
		in.TankID = &tank.ID
		in.StationID = &tank.StationID
		in.ProductID = &tank.ProductID
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	after, err := s.nozzles.Update(ctx, tx, actor.TenantID, id, in)
	if errors.Is(err, nozzles.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a nozzle with that number already exists on this pump")
		return
	}
	if err != nil {
		s.logger.Error("update nozzle", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "nozzle.updated", EventType: "NozzleUpdated",
		EntityType: "nozzle", EntityID: after.ID.String(),
		PreviousValue: toNozzleDTO(before), NewValue: toNozzleDTO(after),
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
	writeJSON(w, http.StatusOK, toNozzleDTO(after))
}

func (s *Server) handleDeleteNozzle(w http.ResponseWriter, r *http.Request) {
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

	ctx := r.Context()
	before, err := s.nozzles.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.authorizeStation(w, r, actor, "pumps.manage", before.StationID) {
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.nozzles.SoftDelete(ctx, tx, actor.TenantID, id); err != nil {
		if errors.Is(err, nozzles.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "nozzle.deleted", EventType: "NozzleDeleted",
		EntityType: "nozzle", EntityID: id.String(),
		PreviousValue: toNozzleDTO(before),
		IP:            clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
