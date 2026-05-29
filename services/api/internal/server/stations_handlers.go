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
	"github.com/japharyroman/fuelgrid-os/internal/stations"
)

type stationDTO struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	CompanyID    uuid.UUID  `json:"company_id"`
	RegionID     *uuid.UUID `json:"region_id,omitempty"`
	Name         string     `json:"name"`
	Code         string     `json:"code"`
	AddressLine1 *string    `json:"address_line1,omitempty"`
	AddressLine2 *string    `json:"address_line2,omitempty"`
	City         *string    `json:"city,omitempty"`
	State        *string    `json:"state,omitempty"`
	Country      *string    `json:"country,omitempty"`
	Latitude     *float64   `json:"latitude,omitempty"`
	Longitude    *float64   `json:"longitude,omitempty"`
	Timezone     string     `json:"timezone"`
	Status       string     `json:"status"`
}

func toStationDTO(s *stations.Station) stationDTO {
	return stationDTO{
		ID: s.ID, TenantID: s.TenantID, CompanyID: s.CompanyID, RegionID: s.RegionID,
		Name: s.Name, Code: s.Code,
		AddressLine1: s.AddressLine1, AddressLine2: s.AddressLine2,
		City: s.City, State: s.State, Country: s.Country,
		Latitude: s.Latitude, Longitude: s.Longitude,
		Timezone: s.Timezone, Status: s.Status,
	}
}

func (s *Server) handleListStations(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var regionID *uuid.UUID
	if v := r.URL.Query().Get("region_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid region_id")
			return
		}
		regionID = &id
	}
	rows, err := s.stations.List(r.Context(), actor.TenantID, regionID)
	if err != nil {
		s.logger.Error("list stations", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]stationDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toStationDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// handleGetStation replaces the Stage-5 inline handler. Same contract.
func (s *Server) handleGetStation(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}
	st, err := s.stations.Get(r.Context(), actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		s.logger.Error("get station", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Station exists in this tenant; now enforce per-station read scope.
	if !s.authorizeStation(w, r, actor, "station.read", id) {
		return
	}
	writeJSON(w, http.StatusOK, toStationDTO(st))
}

type createStationRequest struct {
	CompanyID    uuid.UUID  `json:"company_id"`
	RegionID     *uuid.UUID `json:"region_id,omitempty"`
	Name         string     `json:"name"`
	Code         string     `json:"code"`
	AddressLine1 *string    `json:"address_line1,omitempty"`
	AddressLine2 *string    `json:"address_line2,omitempty"`
	City         *string    `json:"city,omitempty"`
	State        *string    `json:"state,omitempty"`
	Country      *string    `json:"country,omitempty"`
	Latitude     *float64   `json:"latitude,omitempty"`
	Longitude    *float64   `json:"longitude,omitempty"`
	Timezone     string     `json:"timezone,omitempty"`
}

func (s *Server) handleCreateStation(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createStationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" || req.Code == "" || req.CompanyID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "company_id, name, and code are required")
		return
	}

	ctx := r.Context()

	// Verify the parent company (and region, if supplied) belong to the
	// actor's tenant. The composite FKs in migration 0008 are the
	// backstop; these guards return clean 404s.
	if _, err := s.companies.Get(ctx, actor.TenantID, req.CompanyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "company not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if req.RegionID != nil {
		if _, err := s.regions.Get(ctx, actor.TenantID, *req.RegionID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "region not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	out, err := s.stations.Create(ctx, tx, actor.TenantID, stations.CreateInput{
		CompanyID: req.CompanyID, RegionID: req.RegionID,
		Name: req.Name, Code: req.Code,
		AddressLine1: req.AddressLine1, AddressLine2: req.AddressLine2,
		City: req.City, State: req.State, Country: req.Country,
		Latitude: req.Latitude, Longitude: req.Longitude,
		Timezone: req.Timezone,
	})
	if err != nil {
		s.logger.Error("create station", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "station.created", EventType: "StationCreated",
		EntityType: "station", EntityID: out.ID.String(),
		NewValue: toStationDTO(out),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toStationDTO(out))
}

type updateStationRequest struct {
	RegionID     *uuid.UUID `json:"region_id,omitempty"`
	Name         *string    `json:"name,omitempty"`
	Code         *string    `json:"code,omitempty"`
	AddressLine1 *string    `json:"address_line1,omitempty"`
	AddressLine2 *string    `json:"address_line2,omitempty"`
	City         *string    `json:"city,omitempty"`
	State        *string    `json:"state,omitempty"`
	Country      *string    `json:"country,omitempty"`
	Latitude     *float64   `json:"latitude,omitempty"`
	Longitude    *float64   `json:"longitude,omitempty"`
	Timezone     *string    `json:"timezone,omitempty"`
	Status       *string    `json:"status,omitempty"`
}

func (s *Server) handleUpdateStation(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req updateStationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := s.stations.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	after, err := s.stations.Update(ctx, tx, actor.TenantID, id, stations.UpdateInput{
		RegionID: req.RegionID, Name: req.Name, Code: req.Code,
		AddressLine1: req.AddressLine1, AddressLine2: req.AddressLine2,
		City: req.City, State: req.State, Country: req.Country,
		Latitude: req.Latitude, Longitude: req.Longitude,
		Timezone: req.Timezone, Status: req.Status,
	})
	if errors.Is(err, stations.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.logger.Error("update station", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "station.updated", EventType: "StationUpdated",
		EntityType: "station", EntityID: after.ID.String(),
		PreviousValue: toStationDTO(before), NewValue: toStationDTO(after),
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
	writeJSON(w, http.StatusOK, toStationDTO(after))
}

func (s *Server) handleDeleteStation(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := s.stations.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := s.stations.SoftDelete(ctx, tx, actor.TenantID, id); err != nil {
		if errors.Is(err, stations.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "station.deleted", EventType: "StationDeleted",
		EntityType: "station", EntityID: id.String(),
		PreviousValue: toStationDTO(before),
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
