package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
	"github.com/japharyroman/fuelgrid-os/internal/payments"
	"github.com/japharyroman/fuelgrid-os/internal/receivables"
)

type paymentDTO struct {
	ID         uuid.UUID  `json:"id"`
	StationID  uuid.UUID  `json:"station_id"`
	ShiftID    *uuid.UUID `json:"shift_id,omitempty"`
	CustomerID *uuid.UUID `json:"customer_id,omitempty"`
	TenderType string     `json:"tender_type"`
	Amount     string     `json:"amount"`
	Reference  *string    `json:"reference,omitempty"`
	ReceivedBy uuid.UUID  `json:"received_by"`
	ReceivedAt string     `json:"received_at"`
	Status     string     `json:"status"`
	Notes      *string    `json:"notes,omitempty"`
}

func toPaymentDTO(p *payments.Payment) paymentDTO {
	return paymentDTO{
		ID: p.ID, StationID: p.StationID, ShiftID: p.ShiftID, CustomerID: p.CustomerID,
		TenderType: p.TenderType, Amount: p.Amount, Reference: p.Reference,
		ReceivedBy: p.ReceivedBy, ReceivedAt: p.ReceivedAt.Format(time.RFC3339),
		Status: p.Status, Notes: p.Notes,
	}
}

var validTenders = map[string]bool{"cash": true, "mobile_money": true, "card": true, "credit": true, "voucher": true}

type recordPaymentRequest struct {
	TenderType   string     `json:"tender_type"`
	Amount       string     `json:"amount"`
	Reference    *string    `json:"reference,omitempty"`
	CustomerID   *uuid.UUID `json:"customer_id,omitempty"`
	AllowOverLimit bool     `json:"allow_over_limit"`
	Notes        *string    `json:"notes,omitempty"`
}

func (s *Server) handleRecordPayment(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	shiftID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid shift id")
		return
	}
	var req recordPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !validTenders[req.TenderType] {
		writeError(w, http.StatusBadRequest, "tender_type must be cash|mobile_money|card|credit|voucher")
		return
	}
	if amt, ok := parseDecimal(req.Amount); !ok || amt < 0 {
		writeError(w, http.StatusBadRequest, "amount must be a non-negative decimal")
		return
	}
	if req.TenderType == "credit" && req.CustomerID == nil {
		writeError(w, http.StatusBadRequest, "credit tender requires a customer_id")
		return
	}

	ctx := r.Context()
	shift, err := s.operations.GetShift(ctx, actor.TenantID, shiftID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "shift not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "payment.record", shift.StationID) {
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	p, err := s.payments.Record(ctx, tx, actor.TenantID, payments.RecordInput{
		StationID: shift.StationID, ShiftID: &shiftID, CustomerID: req.CustomerID,
		TenderType: req.TenderType, Amount: req.Amount, Reference: req.Reference,
		ReceivedBy: actor.UserID, Notes: req.Notes,
	})
	if isForeignKeyViolation(err) {
		writeError(w, http.StatusBadRequest, "unknown customer for this tenant")
		return
	}
	if err != nil {
		s.logger.Error("record payment", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// A credit tender allocated to a customer posts an AR charge.
	if req.TenderType == "credit" && req.CustomerID != nil {
		allowOver := req.AllowOverLimit && s.actorHolds(ctx, actor, "credit.override_limit")
		srcType := "shift"
		if _, err := s.receivables.PostCharge(ctx, tx, actor.TenantID, *req.CustomerID, req.Amount,
			&srcType, &shiftID, actor.UserID, req.Notes, allowOver); errors.Is(err, receivables.ErrCreditLimit) {
			writeError(w, http.StatusUnprocessableEntity, "charge would exceed the customer's credit limit")
			return
		} else if err != nil {
			s.logger.Error("record payment: ar charge", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "payment.recorded", EventType: "PaymentRecorded",
		EntityType: "payment", EntityID: p.ID.String(),
		NewValue: toPaymentDTO(p),
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
	writeJSON(w, http.StatusCreated, toPaymentDTO(p))
}

func (s *Server) handleListShiftPayments(w http.ResponseWriter, r *http.Request) {
	actor, shift, ok := s.shiftForRevenueRead(w, r)
	if !ok {
		return
	}
	rows, err := s.payments.ListForShift(r.Context(), actor.TenantID, shift.ID)
	if err != nil {
		s.logger.Error("list shift payments", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]paymentDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toPaymentDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleShiftPaymentReconciliation(w http.ResponseWriter, r *http.Request) {
	actor, shift, ok := s.shiftForRevenueRead(w, r)
	if !ok {
		return
	}
	rec, err := s.payments.ReconcileShift(r.Context(), s.deps.DB, actor.TenantID, shift.ID)
	if err != nil {
		s.logger.Error("shift payment reconciliation", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	overThreshold := false
	if v, ok := parseDecimal(rec.Variance); ok {
		if v < 0 {
			v = -v
		}
		overThreshold = v > 1 // flag variances over 1 unit of currency
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"shift_id":       shift.ID,
		"tendered":       rec.Tendered,
		"recognized":     rec.Recognized,
		"variance":       rec.Variance,
		"over_threshold": overThreshold,
	})
}

// shiftForRevenueRead loads the URL shift and authorizes revenue.read against
// its station.
func (s *Server) shiftForRevenueRead(w http.ResponseWriter, r *http.Request) (identity.Actor, *operations.Shift, bool) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return identity.Actor{}, nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid shift id")
		return identity.Actor{}, nil, false
	}
	shift, err := s.operations.GetShift(r.Context(), actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "shift not found")
		return identity.Actor{}, nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return identity.Actor{}, nil, false
	}
	if !s.authorizeStation(w, r, actor, "revenue.read", shift.StationID) {
		return identity.Actor{}, nil, false
	}
	return actor, shift, true
}

// actorHolds reports whether the actor holds a permission in any scope.
func (s *Server) actorHolds(ctx context.Context, actor identity.Actor, code string) bool {
	ps, err := s.policy.LoadFor(ctx, actor)
	if err != nil {
		return false
	}
	return ps.HasPermission(code)
}
