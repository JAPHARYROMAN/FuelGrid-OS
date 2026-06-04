package server

import (
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
	"github.com/japharyroman/fuelgrid-os/internal/inventory"
)

// openingStockRequestDTO is the wire shape of an opening-stock lifecycle row
// (Feature 1.6: draft -> approve(lock) / reject). litres is an exact-decimal
// string; the movement link and balance snapshot appear once approved (locked).
type openingStockRequestDTO struct {
	ID           uuid.UUID  `json:"id"`
	TankID       uuid.UUID  `json:"tank_id"`
	Litres       string     `json:"litres"`
	Notes        *string    `json:"notes,omitempty"`
	Status       string     `json:"status"`
	MovementID   *uuid.UUID `json:"movement_id,omitempty"`
	BalanceAfter *string    `json:"balance_after,omitempty"`
	RequestedBy  uuid.UUID  `json:"requested_by"`
	ApprovedBy   *uuid.UUID `json:"approved_by,omitempty"`
	RejectedBy   *uuid.UUID `json:"rejected_by,omitempty"`
	DecisionNote *string    `json:"decision_note,omitempty"`
	RequestedAt  string     `json:"requested_at"`
	DecidedAt    *string    `json:"decided_at,omitempty"`
}

func toOpeningStockRequestDTO(o *inventory.OpeningRequest) openingStockRequestDTO {
	dto := openingStockRequestDTO{
		ID: o.ID, TankID: o.TankID, Litres: o.Litres, Notes: o.Notes, Status: o.Status,
		MovementID: o.MovementID, BalanceAfter: o.BalanceAfter,
		RequestedBy: o.RequestedBy, ApprovedBy: o.ApprovedBy, RejectedBy: o.RejectedBy,
		DecisionNote: o.DecisionNote, RequestedAt: o.RequestedAt.Format(time.RFC3339),
	}
	if o.DecidedAt != nil {
		s := o.DecidedAt.Format(time.RFC3339)
		dto.DecidedAt = &s
	}
	return dto
}

// openingRequestLifecycleError maps an opening-stock lifecycle error to an HTTP
// response; returns ok=false (after writing) when err is non-nil.
func (s *Server) openingRequestLifecycleError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, inventory.ErrOpeningRequestNotFound):
		writeError(w, http.StatusNotFound, "opening stock request not found")
	case errors.Is(err, inventory.ErrOpeningRequestSelfApprove):
		writeError(w, http.StatusForbidden, "separation of duties: you cannot decide an opening stock request you entered")
	case errors.Is(err, inventory.ErrOpeningRequestBadState):
		writeError(w, http.StatusConflict, "opening stock request is not in the required state for this action")
	case errors.Is(err, inventory.ErrOpeningRequestExists):
		writeError(w, http.StatusConflict, "this tank already has a live or locked opening stock")
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
	return false
}

// openingRequestStation loads a request and the station of its tank, and
// authorizes the actor for perm against that station. Returns ok=false after
// writing the response.
func (s *Server) openingRequestStation(w http.ResponseWriter, r *http.Request, actor identity.Actor, id uuid.UUID, perm string) (req *inventory.OpeningRequest, ok bool) {
	o, err := s.inventory.GetOpeningRequest(r.Context(), actor.TenantID, id)
	if errors.Is(err, inventory.ErrOpeningRequestNotFound) {
		writeError(w, http.StatusNotFound, "opening stock request not found")
		return nil, false
	}
	if err != nil {
		s.logger.Error("opening stock request: load", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	tank, err := s.tanks.Get(r.Context(), actor.TenantID, o.TankID)
	if err != nil {
		s.logger.Error("opening stock request: load tank", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	if !s.authorizeStation(w, r, actor, perm, tank.StationID) {
		return nil, false
	}
	return o, true
}

// handleRequestOpeningStock records a new opening-stock request (draft state)
// for a tank, gated by the station-scoped stock.adjust permission.
func (s *Server) handleRequestOpeningStock(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		TankID  uuid.UUID    `json:"tank_id"`
		Litres  decimalInput `json:"litres"`
		FromDip bool         `json:"from_dip"`
		Notes   *string      `json:"notes,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.TankID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "tank_id is required")
		return
	}

	ctx := r.Context()
	tank, err := s.tanks.Get(ctx, actor.TenantID, req.TankID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tank not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "stock.adjust", tank.StationID) {
		return
	}

	// Resolve opening litres as an exact-decimal STRING: the tank's first dip
	// (already numeric text), or a validated non-negative explicit value.
	var litres string
	if req.FromDip {
		dip, derr := s.readings.FirstDipForTank(ctx, actor.TenantID, tank.ID)
		if errors.Is(derr, pgx.ErrNoRows) {
			writeError(w, http.StatusUnprocessableEntity, "tank has no dip reading to seed an opening balance")
			return
		}
		if derr != nil {
			s.logger.Error("opening stock request: first dip", "error", derr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		litres = dip.VolumeLitres
	} else {
		// decimalPattern is sign-free, so Valid() also rejects negatives.
		if !req.Litres.Valid() {
			writeError(w, http.StatusBadRequest, "litres must be provided and a non-negative decimal, or set from_dip")
			return
		}
		litres = req.Litres.String()
	}

	var or *inventory.OpeningRequest
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "opening_stock.recorded", EventType: "OpeningStockRecorded", EntityType: "opening_stock_request",
	}, func(tx pgx.Tx) (string, error) {
		out, rerr := s.inventory.RequestOpeningStock(ctx, tx, actor.TenantID, inventory.OpeningRequestInput{
			TankID: tank.ID, Litres: litres, Notes: req.Notes, RequestedBy: actor.UserID,
		})
		if rerr != nil {
			if errors.Is(rerr, inventory.ErrOpeningRequestExists) {
				writeError(w, http.StatusConflict, "this tank already has a live or locked opening stock")
				return "", rerr
			}
			if isForeignKeyViolation(rerr) {
				writeError(w, http.StatusBadRequest, "unknown tank")
				return "", rerr
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", rerr
		}
		or = out
		return out.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toOpeningStockRequestDTO(or))
}

// handleListOpeningStockRequests lists the tenant's opening-stock requests,
// optionally filtered by status and tank_id query params. Gated tenant-wide by
// the held stock.adjust permission (the list is a review queue carrying no
// money; the per-station check happens on get/decide).
func (s *Server) handleListOpeningStockRequests(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	var tankID *uuid.UUID
	if raw := r.URL.Query().Get("tank_id"); raw != "" {
		id, perr := uuid.Parse(raw)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "tank_id must be a uuid")
			return
		}
		tankID = &id
	}
	rows, err := s.inventory.ListOpeningRequestsPage(r.Context(), actor.TenantID, r.URL.Query().Get("status"), tankID, limit+1, offset)
	if err != nil {
		s.logger.Error("list opening stock requests", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]openingStockRequestDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toOpeningStockRequestDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

// handleGetOpeningStockRequest returns one request, gated by stock.adjust at the
// tank's station.
func (s *Server) handleGetOpeningStockRequest(w http.ResponseWriter, r *http.Request) {
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
	or, ok := s.openingRequestStation(w, r, actor, id, "stock.adjust")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toOpeningStockRequestDTO(or))
}

// handleApproveOpeningStock approves a draft request: posts the genesis opening
// movement to the tank's ledger and LOCKS the request. Gated by
// stock.approve_adjustment at the tank's station (separation of duties enforced
// in the repo). Posting the movement and locking commit atomically.
func (s *Server) handleApproveOpeningStock(w http.ResponseWriter, r *http.Request) {
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
	if _, ok := s.openingRequestStation(w, r, actor, id, "stock.approve_adjustment"); !ok {
		return
	}
	var body struct {
		Note *string `json:"note,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	or, m, err := s.inventory.ApproveOpeningStock(ctx, tx, actor.TenantID, id, actor.UserID, body.Note)
	if !s.openingRequestLifecycleError(w, err) {
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "opening_stock.approved", EventType: "OpeningStockApproved",
		EntityType: "opening_stock_request", EntityID: id.String(),
		NewValue: map[string]any{
			"movement_id": m.ID, "litres": or.Litres, "balance_after": or.BalanceAfter,
		},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("approve opening stock: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toOpeningStockRequestDTO(or))
}

// handleRejectOpeningStock moves a draft request -> rejected with a reason,
// gated by stock.approve_adjustment at the tank's station.
func (s *Server) handleRejectOpeningStock(w http.ResponseWriter, r *http.Request) {
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
	if _, ok := s.openingRequestStation(w, r, actor, id, "stock.approve_adjustment"); !ok {
		return
	}
	var body struct {
		Note *string `json:"note,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	var or *inventory.OpeningRequest
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "opening_stock.rejected", EventType: "OpeningStockRejected",
		EntityType: "opening_stock_request", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		out, rerr := s.inventory.RejectOpeningStock(r.Context(), tx, actor.TenantID, id, actor.UserID, body.Note)
		if rerr != nil {
			s.openingRequestLifecycleError(w, rerr)
			return "", rerr
		}
		or = out
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toOpeningStockRequestDTO(or))
}
