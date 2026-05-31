package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/receivables"
)

type customerDTO struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"tenant_id"`
	Code             string    `json:"code"`
	Name             string    `json:"name"`
	ContactName      *string   `json:"contact_name,omitempty"`
	ContactPhone     *string   `json:"contact_phone,omitempty"`
	ContactEmail     *string   `json:"contact_email,omitempty"`
	CreditLimit      string    `json:"credit_limit"`
	Status           string    `json:"status"`
	LegalName        *string   `json:"legal_name,omitempty"`
	TradingName      *string   `json:"trading_name,omitempty"`
	TaxID            *string   `json:"tax_id,omitempty"`
	BillingAddress   *string   `json:"billing_address,omitempty"`
	AccountType      string    `json:"account_type"`
	DefaultTermsDays int       `json:"default_terms_days"`
	Notes            *string   `json:"notes,omitempty"`
}

func toCustomerDTO(c *receivables.Customer) customerDTO {
	return customerDTO{
		ID: c.ID, TenantID: c.TenantID, Code: c.Code, Name: c.Name,
		ContactName: c.ContactName, ContactPhone: c.ContactPhone, ContactEmail: c.ContactEmail,
		CreditLimit: c.CreditLimit, Status: c.Status,
		LegalName: c.LegalName, TradingName: c.TradingName, TaxID: c.TaxID, BillingAddress: c.BillingAddress,
		AccountType: c.AccountType, DefaultTermsDays: c.DefaultTermsDays, Notes: c.Notes,
	}
}

type arEntryDTO struct {
	ID            uuid.UUID  `json:"id"`
	CustomerID    uuid.UUID  `json:"customer_id"`
	EntryType     string     `json:"entry_type"`
	Amount        string     `json:"amount"`
	BalanceAfter  string     `json:"balance_after"`
	SourceRefType *string    `json:"source_ref_type,omitempty"`
	SourceRefID   *uuid.UUID `json:"source_ref_id,omitempty"`
	RecordedAt    string     `json:"recorded_at"`
	Notes         *string    `json:"notes,omitempty"`
}

func toAREntryDTO(e *receivables.AREntry) arEntryDTO {
	return arEntryDTO{
		ID: e.ID, CustomerID: e.CustomerID, EntryType: e.EntryType, Amount: e.Amount,
		BalanceAfter: e.BalanceAfter, SourceRefType: e.SourceRefType, SourceRefID: e.SourceRefID,
		RecordedAt: e.RecordedAt.Format(time.RFC3339), Notes: e.Notes,
	}
}

type customerRequest struct {
	Code             string  `json:"code"`
	Name             string  `json:"name"`
	ContactName      *string `json:"contact_name,omitempty"`
	ContactPhone     *string `json:"contact_phone,omitempty"`
	ContactEmail     *string `json:"contact_email,omitempty"`
	CreditLimit      string  `json:"credit_limit"`
	LegalName        *string `json:"legal_name,omitempty"`
	TradingName      *string `json:"trading_name,omitempty"`
	TaxID            *string `json:"tax_id,omitempty"`
	BillingAddress   *string `json:"billing_address,omitempty"`
	AccountType      string  `json:"account_type,omitempty"`
	DefaultTermsDays *int    `json:"default_terms_days,omitempty"`
	Notes            *string `json:"notes,omitempty"`
}

func (req customerRequest) toInput() receivables.CustomerInput {
	return receivables.CustomerInput{
		Code: req.Code, Name: req.Name, ContactName: req.ContactName,
		ContactPhone: req.ContactPhone, ContactEmail: req.ContactEmail, CreditLimit: req.CreditLimit,
		LegalName: req.LegalName, TradingName: req.TradingName, TaxID: req.TaxID,
		BillingAddress: req.BillingAddress, AccountType: req.AccountType,
		DefaultTermsDays: req.DefaultTermsDays, Notes: req.Notes,
	}
}

func (s *Server) handleListCustomers(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.receivables.ListCustomersPage(r.Context(), actor.TenantID, limit+1, offset)
	if err != nil {
		s.logger.Error("list customers", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]customerDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toCustomerDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

func (s *Server) handleCreateCustomer(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req customerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Code == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "code and name are required")
		return
	}
	if req.CreditLimit != "" {
		if v, ok := parseDecimal(req.CreditLimit); !ok || v < 0 {
			writeError(w, http.StatusBadRequest, "credit_limit must be a non-negative decimal")
			return
		}
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	c, err := s.receivables.CreateCustomer(ctx, tx, actor.TenantID, req.toInput())
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a customer with that code already exists")
		return
	}
	if err != nil {
		s.logger.Error("create customer", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer.created", EventType: "CustomerCreated",
		EntityType: "customer", EntityID: c.ID.String(), NewValue: toCustomerDTO(c),
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toCustomerDTO(c))
}

func (s *Server) handleUpdateCustomer(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	var req customerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.CreditLimit != "" {
		if v, ok := parseDecimal(req.CreditLimit); !ok || v < 0 {
			writeError(w, http.StatusBadRequest, "credit_limit must be a non-negative decimal")
			return
		}
	}
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	c, err := s.receivables.UpdateCustomer(ctx, tx, actor.TenantID, id, req.toInput())
	if errors.Is(err, receivables.ErrNotFound) {
		writeError(w, http.StatusNotFound, "customer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer.updated", EventType: "CustomerUpdated",
		EntityType: "customer", EntityID: c.ID.String(), NewValue: toCustomerDTO(c),
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toCustomerDTO(c))
}

func (s *Server) handleCustomerStatement(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	ctx := r.Context()
	c, err := s.receivables.GetCustomer(ctx, actor.TenantID, id)
	if errors.Is(err, receivables.ErrNotFound) {
		writeError(w, http.StatusNotFound, "customer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	entries, err := s.receivables.Statement(ctx, actor.TenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	balance, err := s.receivables.Balance(ctx, actor.TenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]arEntryDTO, 0, len(entries))
	for i := range entries {
		out = append(out, toAREntryDTO(&entries[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"customer": toCustomerDTO(c), "balance": balance, "entries": out,
	})
}

type customerPaymentRequest struct {
	Amount    string  `json:"amount"`
	Reference *string `json:"reference,omitempty"`
	Notes     *string `json:"notes,omitempty"`
}

func (s *Server) handleRecordCustomerPayment(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	var req customerPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if v, ok := parseDecimal(req.Amount); !ok || v <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be a positive decimal")
		return
	}
	ctx := r.Context()
	if _, err := s.receivables.GetCustomer(ctx, actor.TenantID, id); errors.Is(err, receivables.ErrNotFound) {
		writeError(w, http.StatusNotFound, "customer not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	srcType := "customer_payment"
	e, err := s.receivables.PostPayment(ctx, tx, actor.TenantID, id, req.Amount, &srcType, nil, actor.UserID, req.Notes)
	if err != nil {
		s.logger.Error("record customer payment", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "customer_payment.recorded", EventType: "CustomerPaymentRecorded",
		EntityType: "ar_entry", EntityID: e.ID.String(), NewValue: toAREntryDTO(e),
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toAREntryDTO(e))
}
