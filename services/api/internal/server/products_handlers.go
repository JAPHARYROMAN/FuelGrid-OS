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
	"github.com/japharyroman/fuelgrid-os/internal/products"
)

type productDTO struct {
	ID                   uuid.UUID `json:"id"`
	TenantID             uuid.UUID `json:"tenant_id"`
	Code                 string    `json:"code"`
	Name                 string    `json:"name"`
	Category             string    `json:"category"`
	Unit                 string    `json:"unit"`
	DefaultPrice         float64   `json:"default_price"`
	TaxRate              float64   `json:"tax_rate"`
	DensityKgM3          *float64  `json:"density_kg_m3,omitempty"`
	LossTolerancePercent float64   `json:"loss_tolerance_percent"`
	Color                string    `json:"color"`
	Status               string    `json:"status"`
}

func toProductDTO(p *products.Product) productDTO {
	return productDTO{
		ID: p.ID, TenantID: p.TenantID, Code: p.Code, Name: p.Name,
		Category: p.Category, Unit: p.Unit,
		DefaultPrice: p.DefaultPrice, TaxRate: p.TaxRate,
		DensityKgM3: p.DensityKgM3, LossTolerancePercent: p.LossTolerancePercent,
		Color: p.Color, Status: p.Status,
	}
}

func (s *Server) handleListProducts(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.products.List(r.Context(), actor.TenantID)
	if err != nil {
		s.logger.Error("list products", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]productDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toProductDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleGetProduct(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid product id")
		return
	}
	p, err := s.products.Get(r.Context(), actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		s.logger.Error("get product", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toProductDTO(p))
}

type createProductRequest struct {
	Code                 string   `json:"code"`
	Name                 string   `json:"name"`
	Category             string   `json:"category,omitempty"`
	Unit                 string   `json:"unit,omitempty"`
	DefaultPrice         float64  `json:"default_price,omitempty"`
	TaxRate              float64  `json:"tax_rate,omitempty"`
	DensityKgM3          *float64 `json:"density_kg_m3,omitempty"`
	LossTolerancePercent float64  `json:"loss_tolerance_percent,omitempty"`
	Color                string   `json:"color,omitempty"`
}

func (s *Server) handleCreateProduct(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Code == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "code and name are required")
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	p, err := s.products.Create(ctx, tx, actor.TenantID, products.CreateInput{
		Code: req.Code, Name: req.Name, Category: req.Category, Unit: req.Unit,
		DefaultPrice: req.DefaultPrice, TaxRate: req.TaxRate,
		DensityKgM3: req.DensityKgM3, LossTolerancePercent: req.LossTolerancePercent,
		Color: req.Color,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a product with that code already exists")
		return
	}
	if err != nil {
		s.logger.Error("create product", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "product.created", EventType: "ProductCreated",
		EntityType: "product", EntityID: p.ID.String(),
		NewValue: toProductDTO(p),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("create product: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toProductDTO(p))
}

type updateProductRequest struct {
	Code                 *string  `json:"code,omitempty"`
	Name                 *string  `json:"name,omitempty"`
	Category             *string  `json:"category,omitempty"`
	Unit                 *string  `json:"unit,omitempty"`
	DefaultPrice         *float64 `json:"default_price,omitempty"`
	TaxRate              *float64 `json:"tax_rate,omitempty"`
	DensityKgM3          *float64 `json:"density_kg_m3,omitempty"`
	LossTolerancePercent *float64 `json:"loss_tolerance_percent,omitempty"`
	Color                *string  `json:"color,omitempty"`
	Status               *string  `json:"status,omitempty"`
}

func (s *Server) handleUpdateProduct(w http.ResponseWriter, r *http.Request) {
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
	var req updateProductRequest
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

	before, err := s.products.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	after, err := s.products.Update(ctx, tx, actor.TenantID, id, products.UpdateInput{
		Code: req.Code, Name: req.Name, Category: req.Category, Unit: req.Unit,
		DefaultPrice: req.DefaultPrice, TaxRate: req.TaxRate,
		DensityKgM3: req.DensityKgM3, LossTolerancePercent: req.LossTolerancePercent,
		Color: req.Color, Status: req.Status,
	})
	if errors.Is(err, products.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a product with that code already exists")
		return
	}
	if err != nil {
		s.logger.Error("update product", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "product.updated", EventType: "ProductUpdated",
		EntityType: "product", EntityID: after.ID.String(),
		PreviousValue: toProductDTO(before), NewValue: toProductDTO(after),
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
	writeJSON(w, http.StatusOK, toProductDTO(after))
}

func (s *Server) handleDeleteProduct(w http.ResponseWriter, r *http.Request) {
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

	before, err := s.products.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Don't orphan tanks: a product still bound to live tanks can't be deleted.
	if n, err := s.tanks.CountActiveForProduct(ctx, actor.TenantID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if n > 0 {
		writeError(w, http.StatusConflict, "product is in use by tanks; remove or reassign them first")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.products.SoftDelete(ctx, tx, actor.TenantID, id); err != nil {
		if errors.Is(err, products.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "product.deleted", EventType: "ProductDeleted",
		EntityType: "product", EntityID: id.String(),
		PreviousValue: toProductDTO(before),
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
