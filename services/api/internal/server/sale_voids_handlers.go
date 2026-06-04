package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/revenue"
)

// saleVoidDTO is the wire shape of a sale-void lifecycle row. The reversal_*
// fields are present only once the void is approved (the row is then the
// reversal record); money/litres are exact decimal STRINGS (numeric -> text).
type saleVoidDTO struct {
	ID             uuid.UUID  `json:"id"`
	SaleID         uuid.UUID  `json:"sale_id"`
	Status         string     `json:"status"`
	Reason         string     `json:"reason"`
	ReversalLitres *string    `json:"reversal_litres,omitempty"`
	ReversalGross  *string    `json:"reversal_gross,omitempty"`
	ReversalTax    *string    `json:"reversal_tax,omitempty"`
	ReversalNet    *string    `json:"reversal_net,omitempty"`
	ReversalCogs   *string    `json:"reversal_cogs,omitempty"`
	ReversalMargin *string    `json:"reversal_margin,omitempty"`
	RequestedBy    uuid.UUID  `json:"requested_by"`
	DecidedBy      *uuid.UUID `json:"decided_by,omitempty"`
	DecisionNote   *string    `json:"decision_note,omitempty"`
	RequestedAt    string     `json:"requested_at"`
	DecidedAt      *string    `json:"decided_at,omitempty"`
}

func toSaleVoidDTO(v *revenue.SaleVoid) saleVoidDTO {
	dto := saleVoidDTO{
		ID: v.ID, SaleID: v.SaleID, Status: v.Status, Reason: v.Reason,
		ReversalLitres: v.ReversalLitres, ReversalGross: v.ReversalGross,
		ReversalTax: v.ReversalTax, ReversalNet: v.ReversalNet,
		ReversalCogs: v.ReversalCogs, ReversalMargin: v.ReversalMargin,
		RequestedBy: v.RequestedBy, DecidedBy: v.DecidedBy, DecisionNote: v.DecisionNote,
		RequestedAt: v.RequestedAt.Format(time.RFC3339),
	}
	if v.DecidedAt != nil {
		s := v.DecidedAt.Format(time.RFC3339)
		dto.DecidedAt = &s
	}
	return dto
}

// saleVoidLifecycleError maps a revenue sale-void lifecycle error to an HTTP
// response when err is non-nil.
func (s *Server) saleVoidLifecycleError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, revenue.ErrSaleNotFound):
		writeError(w, http.StatusNotFound, "sale not found")
	case errors.Is(err, revenue.ErrVoidNotFound):
		writeError(w, http.StatusNotFound, "sale void not found")
	case errors.Is(err, revenue.ErrVoidSelfApprove):
		writeError(w, http.StatusForbidden, "separation of duties: you cannot decide a sale void you requested")
	case errors.Is(err, revenue.ErrVoidActiveExists):
		writeError(w, http.StatusConflict, "this sale already has an active void")
	case errors.Is(err, revenue.ErrVoidBadState):
		writeError(w, http.StatusConflict, "sale void is not in the required state for this action")
	default:
		s.logger.Error("sale void lifecycle", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// saleStation loads a sale and authorizes the actor for perm against the sale's
// station. Returns ok=false after writing the response.
func (s *Server) saleStation(w http.ResponseWriter, r *http.Request, actor identity.Actor, saleID uuid.UUID, perm string) bool {
	sl, err := s.revenue.GetSale(r.Context(), actor.TenantID, saleID)
	if errors.Is(err, revenue.ErrSaleNotFound) {
		writeError(w, http.StatusNotFound, "sale not found")
		return false
	}
	if err != nil {
		s.logger.Error("sale void: load sale", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	return s.authorizeStation(w, r, actor, perm, sl.StationID)
}

// voidStation loads a void, then its sale, and authorizes the actor for perm
// against the sale's station. Returns ok=false after writing the response.
func (s *Server) voidStation(w http.ResponseWriter, r *http.Request, actor identity.Actor, voidID uuid.UUID, perm string) bool {
	sv, err := s.revenue.GetVoid(r.Context(), actor.TenantID, voidID)
	if errors.Is(err, revenue.ErrVoidNotFound) {
		writeError(w, http.StatusNotFound, "sale void not found")
		return false
	}
	if err != nil {
		s.logger.Error("sale void: load void", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	return s.saleStation(w, r, actor, sv.SaleID, perm)
}

// handleGetSaleVoid returns the current (non-rejected) void status of a sale,
// gated by sale.void.request at the sale's station. 404 when the sale has no
// active void.
func (s *Server) handleGetSaleVoid(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	saleID, err := uuid.Parse(chi.URLParam(r, "saleID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid sale id")
		return
	}
	if !s.saleStation(w, r, actor, saleID, "sale.void.request") {
		return
	}
	v, err := s.revenue.VoidForSale(r.Context(), actor.TenantID, saleID)
	if errors.Is(err, revenue.ErrVoidNotFound) {
		writeError(w, http.StatusNotFound, "no active void for this sale")
		return
	}
	if err != nil {
		s.logger.Error("get sale void", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toSaleVoidDTO(v))
}

// handleRequestSaleVoid records a new void (requested state) for a sale, gated
// by the station-scoped sale.void.request at the sale's station.
func (s *Server) handleRequestSaleVoid(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	saleID, err := uuid.Parse(chi.URLParam(r, "saleID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid sale id")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}

	if !s.saleStation(w, r, actor, saleID, "sale.void.request") {
		return
	}

	var sv *revenue.SaleVoid
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "sale.void_requested", EventType: "SaleVoidRequested", EntityType: "sale_void",
	}, func(tx pgx.Tx) (string, error) {
		out, err := s.revenue.RequestVoid(r.Context(), tx, actor.TenantID, saleID, actor.UserID, strings.TrimSpace(req.Reason))
		if err != nil {
			s.saleVoidLifecycleError(w, err)
			return "", err
		}
		sv = out
		return out.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toSaleVoidDTO(sv))
}

// handleApproveSaleVoid moves requested -> approved and records the reversal
// (the sale's amounts negated), gated by sale.void.approve at the sale's
// station. Separation of duties (requester != approver) is enforced in the
// repo. Idempotent: a second approve of an already-approved void is a 409.
func (s *Server) handleApproveSaleVoid(w http.ResponseWriter, r *http.Request) {
	s.decideSaleVoid(w, r, "approved",
		"sale.void_approved", "SaleVoidApproved",
		func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, note *string) (*revenue.SaleVoid, error) {
			return s.revenue.ApproveVoid(r.Context(), tx, actor.TenantID, id, actor.UserID, note)
		})
}

// handleRejectSaleVoid moves requested -> rejected, gated by sale.void.approve
// at the sale's station. The requester cannot reject their own void (403).
func (s *Server) handleRejectSaleVoid(w http.ResponseWriter, r *http.Request) {
	s.decideSaleVoid(w, r, "rejected",
		"sale.void_rejected", "SaleVoidRejected",
		func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, note *string) (*revenue.SaleVoid, error) {
			return s.revenue.RejectVoid(r.Context(), tx, actor.TenantID, id, actor.UserID, note)
		})
}

// decideSaleVoid runs an approve/reject transition inside an audited tx,
// authorizing sale.void.approve against the void's sale station. On approve it
// additionally audits the reversal records created (sale.reversal_created /
// payment.reversal_created) so the financial reversal is traceable.
func (s *Server) decideSaleVoid(w http.ResponseWriter, r *http.Request, action, auditAction, eventType string, fn func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, note *string) (*revenue.SaleVoid, error)) {
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
	if !s.voidStation(w, r, actor, id, "sale.void.approve") {
		return
	}
	var req struct {
		Note *string `json:"note,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional

	var sv *revenue.SaleVoid
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: auditAction, EventType: eventType, EntityType: "sale_void", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		out, err := fn(tx, actor, id, req.Note)
		if err != nil {
			s.saleVoidLifecycleError(w, err)
			return "", err
		}
		sv = out
		// On approve the void becomes the reversal record: audit the recognized
		// revenue reversal and the payment-effect reversal in the same tx so the
		// financial trail is complete.
		if action == "approved" {
			if err := audit.WriteWithOutbox(r.Context(), tx, audit.TxRecord{
				TenantID: actor.TenantID, ActorID: actor.UserID,
				Action: "sale.reversal_created", EventType: "SaleReversalCreated",
				EntityType: "sale", EntityID: out.SaleID.String(),
				NewValue: map[string]any{
					"sale_void_id": out.ID, "sale_id": out.SaleID,
					"reversal_gross": out.ReversalGross, "reversal_net": out.ReversalNet,
					"reversal_tax": out.ReversalTax, "reversal_cogs": out.ReversalCogs,
					"reversal_margin": out.ReversalMargin, "reversal_litres": out.ReversalLitres,
				},
				IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(r.Context()),
			}); err != nil {
				return "", err
			}
			if err := audit.WriteWithOutbox(r.Context(), tx, audit.TxRecord{
				TenantID: actor.TenantID, ActorID: actor.UserID,
				Action: "payment.reversal_created", EventType: "PaymentReversalCreated",
				EntityType: "sale", EntityID: out.SaleID.String(),
				NewValue: map[string]any{
					"sale_void_id": out.ID, "sale_id": out.SaleID,
					// Payments are recorded per shift, not allocated per sale, so the
					// reversal of the sale's payment effect is the net revenue it
					// removed from the shift's reconciliation basis.
					"reversal_gross": out.ReversalGross,
				},
				IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(r.Context()),
			}); err != nil {
				return "", err
			}
		}
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toSaleVoidDTO(sv))
}

// handleListSaleVoids lists the tenant's sale voids, optionally filtered by
// status. Gated tenant-wide by the held sale.void.approve permission — the
// approver queue view; per-sale station scoping rides the actor's station set.
func (s *Server) handleListSaleVoids(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.revenue.ListVoidsPage(r.Context(), actor.TenantID, r.URL.Query().Get("status"), limit+1, offset)
	if err != nil {
		s.logger.Error("list sale voids", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]saleVoidDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toSaleVoidDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}
