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
	"github.com/japharyroman/fuelgrid-os/internal/regions"
)

type regionDTO struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	CompanyID uuid.UUID `json:"company_id"`
	Name      string    `json:"name"`
	Code      *string   `json:"code,omitempty"`
	Status    string    `json:"status"`
}

func toRegionDTO(r *regions.Region) regionDTO {
	return regionDTO{
		ID: r.ID, TenantID: r.TenantID, CompanyID: r.CompanyID,
		Name: r.Name, Code: r.Code, Status: r.Status,
	}
}

func (s *Server) handleListRegions(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var companyID *uuid.UUID
	if v := r.URL.Query().Get("company_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid company_id")
			return
		}
		companyID = &id
	}
	rows, err := s.regions.List(r.Context(), actor.TenantID, companyID)
	if err != nil {
		s.logger.Error("list regions", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]regionDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toRegionDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

type createRegionRequest struct {
	CompanyID uuid.UUID `json:"company_id"`
	Name      string    `json:"name"`
	Code      *string   `json:"code,omitempty"`
}

func (s *Server) handleCreateRegion(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createRegionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" || req.CompanyID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "company_id and name are required")
		return
	}

	ctx := r.Context()

	// The composite FK added in migration 0008 rejects a cross-tenant
	// company link at the DB layer; this guard turns that into a clean
	// 404 instead of a 500 and avoids leaking whether the id exists in
	// another tenant.
	if _, err := s.companies.Get(ctx, actor.TenantID, req.CompanyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "company not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	out, err := s.regions.Create(ctx, tx, actor.TenantID, regions.CreateInput{
		CompanyID: req.CompanyID, Name: req.Name, Code: req.Code,
	})
	if err != nil {
		s.logger.Error("create region", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "region.created", EventType: "RegionCreated",
		EntityType: "region", EntityID: out.ID.String(),
		NewValue:  toRegionDTO(out),
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toRegionDTO(out))
}

type updateRegionRequest struct {
	Name   *string `json:"name,omitempty"`
	Code   *string `json:"code,omitempty"`
	Status *string `json:"status,omitempty"`
}

func (s *Server) handleUpdateRegion(w http.ResponseWriter, r *http.Request) {
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
	var req updateRegionRequest
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

	before, err := s.regions.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	after, err := s.regions.Update(ctx, tx, actor.TenantID, id, regions.UpdateInput{
		Name: req.Name, Code: req.Code, Status: req.Status,
	})
	if errors.Is(err, regions.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.logger.Error("update region", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "region.updated", EventType: "RegionUpdated",
		EntityType: "region", EntityID: after.ID.String(),
		PreviousValue: toRegionDTO(before), NewValue: toRegionDTO(after),
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
	writeJSON(w, http.StatusOK, toRegionDTO(after))
}

func (s *Server) handleDeleteRegion(w http.ResponseWriter, r *http.Request) {
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
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := s.regions.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := s.regions.SoftDelete(ctx, tx, actor.TenantID, id); err != nil {
		if errors.Is(err, regions.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "region.deleted", EventType: "RegionDeleted",
		EntityType: "region", EntityID: id.String(),
		PreviousValue: toRegionDTO(before),
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
