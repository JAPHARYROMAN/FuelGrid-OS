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
	"github.com/japharyroman/fuelgrid-os/internal/companies"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

type companyDTO struct {
	ID             uuid.UUID `json:"id"`
	TenantID       uuid.UUID `json:"tenant_id"`
	Name           string    `json:"name"`
	LegalName      *string   `json:"legal_name,omitempty"`
	RegistrationNo *string   `json:"registration_no,omitempty"`
	TaxID          *string   `json:"tax_id,omitempty"`
	Currency       string    `json:"currency"`
	Timezone       string    `json:"timezone"`
	Status         string    `json:"status"`
}

func toCompanyDTO(c *companies.Company) companyDTO {
	return companyDTO{
		ID: c.ID, TenantID: c.TenantID, Name: c.Name,
		LegalName: c.LegalName, RegistrationNo: c.RegistrationNo, TaxID: c.TaxID,
		Currency: c.Currency, Timezone: c.Timezone, Status: c.Status,
	}
}

func (s *Server) handleListCompanies(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.companies.ListPage(r.Context(), actor.TenantID, limit+1, offset)
	if err != nil {
		s.logger.Error("list companies", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]companyDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toCompanyDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

type createCompanyRequest struct {
	Name           string  `json:"name"`
	LegalName      *string `json:"legal_name,omitempty"`
	RegistrationNo *string `json:"registration_no,omitempty"`
	TaxID          *string `json:"tax_id,omitempty"`
	Currency       string  `json:"currency,omitempty"`
	Timezone       string  `json:"timezone,omitempty"`
}

func (s *Server) handleCreateCompany(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createCompanyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	c, err := s.companies.Create(ctx, tx, actor.TenantID, companies.CreateInput{
		Name: req.Name, LegalName: req.LegalName,
		RegistrationNo: req.RegistrationNo, TaxID: req.TaxID,
		Currency: req.Currency, Timezone: req.Timezone,
	})
	if err != nil {
		s.logger.Error("create company", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID:   actor.TenantID,
		ActorID:    actor.UserID,
		Action:     "company.created",
		EventType:  "CompanyCreated",
		EntityType: "company",
		EntityID:   c.ID.String(),
		NewValue:   toCompanyDTO(c),
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
		RequestID:  chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("create company: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toCompanyDTO(c))
}

type updateCompanyRequest struct {
	Name           *string `json:"name,omitempty"`
	LegalName      *string `json:"legal_name,omitempty"`
	RegistrationNo *string `json:"registration_no,omitempty"`
	TaxID          *string `json:"tax_id,omitempty"`
	Currency       *string `json:"currency,omitempty"`
	Timezone       *string `json:"timezone,omitempty"`
	Status         *string `json:"status,omitempty"`
}

func (s *Server) handleUpdateCompany(w http.ResponseWriter, r *http.Request) {
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
	var req updateCompanyRequest
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

	before, err := s.companies.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && before == nil) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	after, err := s.companies.Update(ctx, tx, actor.TenantID, id, companies.UpdateInput{
		Name: req.Name, LegalName: req.LegalName,
		RegistrationNo: req.RegistrationNo, TaxID: req.TaxID,
		Currency: req.Currency, Timezone: req.Timezone, Status: req.Status,
	})
	if errors.Is(err, companies.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.logger.Error("update company", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID:      actor.TenantID,
		ActorID:       actor.UserID,
		Action:        "company.updated",
		EventType:     "CompanyUpdated",
		EntityType:    "company",
		EntityID:      after.ID.String(),
		PreviousValue: toCompanyDTO(before),
		NewValue:      toCompanyDTO(after),
		IP:            clientIP(r),
		UserAgent:     r.UserAgent(),
		RequestID:     chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toCompanyDTO(after))
}

func (s *Server) handleDeleteCompany(w http.ResponseWriter, r *http.Request) {
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

	before, err := s.companies.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && before == nil) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := s.companies.SoftDelete(ctx, tx, actor.TenantID, id); err != nil {
		if errors.Is(err, companies.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID:      actor.TenantID,
		ActorID:       actor.UserID,
		Action:        "company.deleted",
		EventType:     "CompanyDeleted",
		EntityType:    "company",
		EntityID:      id.String(),
		PreviousValue: toCompanyDTO(before),
		IP:            clientIP(r),
		UserAgent:     r.UserAgent(),
		RequestID:     chimiddleware.GetReqID(ctx),
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
