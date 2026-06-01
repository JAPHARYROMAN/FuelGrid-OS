package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/payments"
	"github.com/japharyroman/fuelgrid-os/internal/payments/mpesa"
	"github.com/japharyroman/fuelgrid-os/internal/revenue"
)

// registerPaymentsRoutes mounts the M-Pesa (Daraja) collection surface. It runs
// inside the admin-console group (requireAuth + rateLimitPerTenant). Writes are
// gated by the station-scoped payment.mpesa.manage permission, authorized
// in-handler against the target station; reads ride payment.record (held).
//
// The Daraja result callback is mounted SEPARATELY (registerPaymentsWebhook),
// outside the authenticated group, because Safaricom posts it with no session.
func (s *Server) registerPaymentsRoutes(r chi.Router) {
	r.With(s.requirePermissionHeld("payment.record")).
		Get("/payments/mpesa/transactions", s.handleListMpesaTransactions)
	r.With(s.requirePermissionHeld("payment.mpesa.manage")).
		Post("/payments/mpesa/stk-push", s.handleInitiateMpesaSTKPush)
	r.With(s.requirePermissionHeld("payment.mpesa.manage")).
		Post("/payments/mpesa/transactions/{id}/reconcile", s.handleReconcileMpesa)
}

// registerPaymentsWebhook mounts the unauthenticated Daraja result callback. It
// is keyed by the globally-unique checkout id (no session, no tenant GUC) and
// runs on the owner pool, so it lives outside every auth group.
func (s *Server) registerPaymentsWebhook(r chi.Router) {
	r.Post("/payments/mpesa/callback", s.handleMpesaCallback)
}

// mpesaTransactionDTO is the wire shape for one M-Pesa transaction.
type mpesaTransactionDTO struct {
	ID                     uuid.UUID  `json:"id"`
	StationID              uuid.UUID  `json:"station_id"`
	CheckoutRequestID      string     `json:"checkout_request_id"`
	MerchantRequestID      *string    `json:"merchant_request_id,omitempty"`
	Amount                 string     `json:"amount"`
	Phone                  string     `json:"phone"`
	Status                 string     `json:"status"`
	ResultCode             *int       `json:"result_code,omitempty"`
	MpesaReceipt           *string    `json:"mpesa_receipt,omitempty"`
	AccountReference       *string    `json:"account_reference,omitempty"`
	Description            *string    `json:"description,omitempty"`
	ReconciledRevenueDayID *uuid.UUID `json:"reconciled_revenue_day_id,omitempty"`
	ReconciledAt           *string    `json:"reconciled_at,omitempty"`
	CreatedAt              string     `json:"created_at"`
	UpdatedAt              string     `json:"updated_at"`
}

func toMpesaDTO(m *payments.MpesaTransaction) mpesaTransactionDTO {
	dto := mpesaTransactionDTO{
		ID: m.ID, StationID: m.StationID, CheckoutRequestID: m.CheckoutRequestID,
		MerchantRequestID: m.MerchantRequestID, Amount: m.Amount, Phone: m.Phone,
		Status: m.Status, ResultCode: m.ResultCode, MpesaReceipt: m.MpesaReceipt,
		AccountReference: m.AccountReference, Description: m.Description,
		ReconciledRevenueDayID: m.ReconciledRevenueDayID,
		CreatedAt:              m.CreatedAt.Format(time.RFC3339),
		UpdatedAt:              m.UpdatedAt.Format(time.RFC3339),
	}
	if m.ReconciledAt != nil {
		ra := m.ReconciledAt.Format(time.RFC3339)
		dto.ReconciledAt = &ra
	}
	return dto
}

// handleListMpesaTransactions lists a tenant's M-Pesa transactions, newest
// first, paged. Optional ?station_id and ?status filters narrow the set.
func (s *Server) handleListMpesaTransactions(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if s.payments == nil {
		writeError(w, http.StatusServiceUnavailable, "payments unavailable")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	var filter payments.MpesaFilter
	if raw := r.URL.Query().Get("station_id"); raw != "" {
		sid, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid station_id")
			return
		}
		filter.StationID = &sid
	}
	if status := r.URL.Query().Get("status"); status != "" {
		filter.Status = status
	}

	rows, err := s.payments.ListMpesa(r.Context(), actor.TenantID, filter, limit+1, offset)
	if err != nil {
		s.logger.Error("list mpesa transactions", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]mpesaTransactionDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toMpesaDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

type initiateMpesaRequest struct {
	StationID        uuid.UUID `json:"station_id"`
	Phone            string    `json:"phone"`
	Amount           string    `json:"amount"`
	AccountReference string    `json:"account_reference,omitempty"`
	Description      string    `json:"description,omitempty"`
}

// handleInitiateMpesaSTKPush initiates a Lipa na M-Pesa Online prompt on the
// payer's phone and records a 'pending' transaction. Station authz is enforced
// in-handler (the station lives in the body). When the M-Pesa client is
// disabled (credentials unset) it returns 503 rather than a 500.
func (s *Server) handleInitiateMpesaSTKPush(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if s.payments == nil || s.mpesa == nil {
		writeError(w, http.StatusServiceUnavailable, "payments unavailable")
		return
	}
	var req initiateMpesaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.StationID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "station_id is required")
		return
	}
	if req.Phone == "" {
		writeError(w, http.StatusBadRequest, "phone is required")
		return
	}
	// Validate amount as a positive decimal up front for a clean 400.
	if amt, ok := parseDecimal(req.Amount); !ok || amt <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be a positive decimal")
		return
	}
	if !s.authorizeStation(w, r, actor, "payment.mpesa.manage", req.StationID) {
		return
	}

	// Initiate the push with Daraja first; only persist a row once we have a
	// checkout id to correlate the eventual callback against.
	res, err := s.mpesa.STKPush(r.Context(), mpesa.STKPushInput{
		Phone:            req.Phone,
		Amount:           req.Amount,
		AccountReference: req.AccountReference,
		Description:      req.Description,
	})
	switch {
	case errors.Is(err, mpesa.ErrDisabled):
		writeError(w, http.StatusServiceUnavailable, "M-Pesa is not configured")
		return
	case errors.Is(err, mpesa.ErrConfig):
		writeError(w, http.StatusServiceUnavailable, "M-Pesa shortcode/passkey/callback not configured")
		return
	case err != nil:
		s.logger.Error("mpesa stk push", "error", err)
		writeError(w, http.StatusBadGateway, "M-Pesa request failed")
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	m, err := s.payments.InitiateMpesa(ctx, tx, actor.TenantID, payments.InitiateMpesaInput{
		StationID:         req.StationID,
		CheckoutRequestID: res.CheckoutRequestID,
		MerchantRequestID: res.MerchantRequestID,
		Amount:            req.Amount,
		Phone:             req.Phone,
		AccountReference:  req.AccountReference,
		Description:       req.Description,
	})
	if isForeignKeyViolation(err) {
		writeError(w, http.StatusBadRequest, "unknown station for this tenant")
		return
	}
	if err != nil {
		s.logger.Error("record mpesa transaction", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "payment.mpesa.initiated", EventType: "MpesaStkPushInitiated",
		EntityType: "mpesa_transaction", EntityID: m.ID.String(),
		NewValue: toMpesaDTO(m),
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
	writeJSON(w, http.StatusCreated, map[string]any{
		"transaction":      toMpesaDTO(m),
		"customer_message": res.CustomerMessage,
	})
}

// handleMpesaCallback is the unauthenticated Daraja result webhook. It parses
// and persists the terminal result keyed by checkout id, then ALWAYS acks 200
// with Daraja's success envelope so Safaricom stops retrying — even on a
// transaction we don't recognise (which we log and swallow). It runs on the
// owner pool (no session, no tenant scope).
func (s *Server) handleMpesaCallback(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Warn("mpesa callback: read body", "error", err)
		writeMpesaAck(w)
		return
	}
	parsed, err := mpesa.ParseCallback(raw)
	if err != nil {
		s.logger.Warn("mpesa callback: parse", "error", err)
		writeMpesaAck(w)
		return
	}

	status := "failed"
	if parsed.Success {
		status = "paid"
	}
	if s.payments != nil && s.deps.DB != nil {
		if _, err := s.payments.SettleMpesaByCheckoutID(r.Context(), s.deps.DB, payments.SettleMpesaInput{
			CheckoutRequestID: parsed.CheckoutRequestID,
			Status:            status,
			ResultCode:        parsed.ResultCode,
			MpesaReceipt:      parsed.MpesaReceipt,
			RawPayload:        json.RawMessage(raw),
		}); err != nil {
			if errors.Is(err, payments.ErrMpesaNotFound) {
				s.logger.Warn("mpesa callback for unknown checkout id",
					"checkout_request_id", parsed.CheckoutRequestID)
			} else {
				s.logger.Error("mpesa callback: settle", "error", err,
					"checkout_request_id", parsed.CheckoutRequestID)
			}
		}
	}
	// Daraja expects this exact ack shape; anything else triggers retries.
	writeMpesaAck(w)
}

func writeMpesaAck(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ResultCode": 0,
		"ResultDesc": "Accepted",
	})
}

type reconcileMpesaRequest struct {
	RevenueDayID uuid.UUID `json:"revenue_day_id"`
}

// handleReconcileMpesa matches a PAID M-Pesa transaction to a revenue day's
// mobile-money tender, recording the link. Station authz is enforced against
// the transaction's station; the revenue day must belong to the same tenant.
func (s *Server) handleReconcileMpesa(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if s.payments == nil || s.revenue == nil {
		writeError(w, http.StatusServiceUnavailable, "payments unavailable")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid transaction id")
		return
	}
	var req reconcileMpesaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.RevenueDayID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "revenue_day_id is required")
		return
	}

	ctx := r.Context()
	txn, err := s.payments.GetMpesa(ctx, actor.TenantID, id)
	if errors.Is(err, payments.ErrMpesaNotFound) {
		writeError(w, http.StatusNotFound, "transaction not found")
		return
	}
	if err != nil {
		s.logger.Error("get mpesa transaction", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "payment.mpesa.manage", txn.StationID) {
		return
	}

	// The revenue day must exist for this tenant; mismatched-tenant ids surface
	// as not-found (RLS + the tenant-scoped lookup) rather than 403.
	day, err := s.revenue.GetDayByID(ctx, actor.TenantID, req.RevenueDayID)
	if errors.Is(err, revenue.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "unknown revenue day for this tenant")
		return
	}
	if err != nil {
		s.logger.Error("reconcile mpesa: load revenue day", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if day.StationID != txn.StationID {
		writeError(w, http.StatusUnprocessableEntity, "revenue day belongs to a different station than the transaction")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	updated, err := s.payments.ReconcileMpesa(ctx, tx, actor.TenantID, id, req.RevenueDayID)
	switch {
	case errors.Is(err, payments.ErrMpesaNotPaid):
		writeError(w, http.StatusUnprocessableEntity, "only a paid transaction can be reconciled")
		return
	case errors.Is(err, payments.ErrMpesaNotFound):
		writeError(w, http.StatusNotFound, "transaction not found")
		return
	case isForeignKeyViolation(err):
		writeError(w, http.StatusBadRequest, "unknown revenue day for this tenant")
		return
	case err != nil:
		s.logger.Error("reconcile mpesa transaction", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "payment.mpesa.reconciled", EventType: "MpesaTransactionReconciled",
		EntityType: "mpesa_transaction", EntityID: updated.ID.String(),
		PreviousValue: toMpesaDTO(txn),
		NewValue:      toMpesaDTO(updated),
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
	writeJSON(w, http.StatusOK, toMpesaDTO(updated))
}
