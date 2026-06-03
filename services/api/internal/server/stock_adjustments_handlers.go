package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/inventory"
)

// signedDecimalPattern matches a signed, non-zero-shaped decimal with up to 3
// fractional places (the stock ledger's numeric(14,3) precision). Sign is
// optional; the leading-zero forms ("0", "-0.000") are screened out separately
// so a request can never carry a no-op delta.
var signedDecimalPattern = regexp.MustCompile(`^-?\d+(\.\d{1,3})?$`)

// validSignedDelta reports whether s is a well-formed signed decimal that is
// not zero in any spelling.
func validSignedDelta(s string) bool {
	s = strings.TrimSpace(s)
	if !signedDecimalPattern.MatchString(s) {
		return false
	}
	// Reject zero ("0", "0.0", "-0.000", …): strip sign, dot and zeros — empty
	// means the value was all zeros.
	return strings.Trim(strings.TrimPrefix(s, "-"), "0.") != ""
}

type stockAdjustmentDTO struct {
	ID             uuid.UUID  `json:"id"`
	TankID         uuid.UUID  `json:"tank_id"`
	DeltaLitres    string     `json:"delta_litres"`
	Reason         string     `json:"reason"`
	Classification string     `json:"classification"`
	Status         string     `json:"status"`
	BalanceBefore  *string    `json:"balance_before,omitempty"`
	BalanceAfter   *string    `json:"balance_after,omitempty"`
	MovementID     *uuid.UUID `json:"movement_id,omitempty"`
	RequestedBy    uuid.UUID  `json:"requested_by"`
	ApprovedBy     *uuid.UUID `json:"approved_by,omitempty"`
	PostedBy       *uuid.UUID `json:"posted_by,omitempty"`
	RejectedBy     *uuid.UUID `json:"rejected_by,omitempty"`
	DecisionNote   *string    `json:"decision_note,omitempty"`
	RequestedAt    string     `json:"requested_at"`
	DecidedAt      *string    `json:"decided_at,omitempty"`
	PostedAt       *string    `json:"posted_at,omitempty"`
}

func toStockAdjustmentDTO(a *inventory.Adjustment) stockAdjustmentDTO {
	dto := stockAdjustmentDTO{
		ID: a.ID, TankID: a.TankID, DeltaLitres: a.DeltaLitres, Reason: a.Reason,
		Classification: a.Classification, Status: a.Status,
		BalanceBefore: a.BalanceBefore, BalanceAfter: a.BalanceAfter, MovementID: a.MovementID,
		RequestedBy: a.RequestedBy, ApprovedBy: a.ApprovedBy, PostedBy: a.PostedBy,
		RejectedBy: a.RejectedBy, DecisionNote: a.DecisionNote,
		RequestedAt: a.RequestedAt.Format(time.RFC3339),
	}
	if a.DecidedAt != nil {
		s := a.DecidedAt.Format(time.RFC3339)
		dto.DecidedAt = &s
	}
	if a.PostedAt != nil {
		s := a.PostedAt.Format(time.RFC3339)
		dto.PostedAt = &s
	}
	return dto
}

// adjustmentLifecycleError maps an inventory lifecycle error to an HTTP
// response; returns ok=false (after writing) when err is non-nil.
func (s *Server) adjustmentLifecycleError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, inventory.ErrAdjustmentNotFound):
		writeError(w, http.StatusNotFound, "stock adjustment not found")
	case errors.Is(err, inventory.ErrAdjustmentSelfApprove):
		writeError(w, http.StatusForbidden, "separation of duties: you cannot decide a stock adjustment you requested")
	case errors.Is(err, inventory.ErrAdjustmentBadState):
		writeError(w, http.StatusConflict, "stock adjustment is not in the required state for this action")
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
	return false
}

// stockAdjustmentStation loads an adjustment and the station of its tank, and
// authorizes the actor for perm against that station. Returns ok=false after
// writing the response.
func (s *Server) stockAdjustmentStation(w http.ResponseWriter, r *http.Request, actor identity.Actor, id uuid.UUID, perm string) (adj *inventory.Adjustment, ok bool) {
	a, err := s.inventory.GetAdjustment(r.Context(), actor.TenantID, id)
	if errors.Is(err, inventory.ErrAdjustmentNotFound) {
		writeError(w, http.StatusNotFound, "stock adjustment not found")
		return nil, false
	}
	if err != nil {
		s.logger.Error("stock adjustment: load", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	tank, err := s.tanks.Get(r.Context(), actor.TenantID, a.TankID)
	if err != nil {
		s.logger.Error("stock adjustment: load tank", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	if !s.authorizeStation(w, r, actor, perm, tank.StationID) {
		return nil, false
	}
	return a, true
}

// handleRequestStockAdjustment records a new stock adjustment (requested state)
// for a tank, gated by the station-scoped stock.adjust permission.
func (s *Server) handleRequestStockAdjustment(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		TankID         uuid.UUID `json:"tank_id"`
		DeltaLitres    string    `json:"delta_litres"`
		Reason         string    `json:"reason"`
		Classification string    `json:"classification"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.TankID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "tank_id is required")
		return
	}
	if !validSignedDelta(req.DeltaLitres) {
		writeError(w, http.StatusBadRequest, "delta_litres must be a non-zero signed decimal")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	if !inventory.ValidClassification(req.Classification) {
		writeError(w, http.StatusBadRequest, "classification must be one of evaporation|measurement_error|theft|spillage|temperature|data_entry|other")
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

	var adj *inventory.Adjustment
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "stock_adjustment.requested", EventType: "StockAdjustmentRequested", EntityType: "stock_adjustment",
	}, func(tx pgx.Tx) (string, error) {
		out, err := s.inventory.RequestAdjustment(ctx, tx, actor.TenantID, inventory.AdjustmentInput{
			TankID: tank.ID, DeltaLitres: req.DeltaLitres, Reason: strings.TrimSpace(req.Reason),
			Classification: req.Classification, RequestedBy: actor.UserID,
		})
		if err != nil {
			if isForeignKeyViolation(err) {
				writeError(w, http.StatusBadRequest, "unknown tank")
				return "", err
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		adj = out
		return out.ID.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toStockAdjustmentDTO(adj))
}

// handleListStockAdjustments lists the tenant's stock adjustments, optionally
// filtered by status and tank_id query params. Gated tenant-wide by the held
// stock.adjust permission (the per-station read scoping rides the actor's
// station set already, and adjustments carry no money — the list is a queue
// view).
func (s *Server) handleListStockAdjustments(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.inventory.ListAdjustmentsPage(r.Context(), actor.TenantID, r.URL.Query().Get("status"), tankID, limit+1, offset)
	if err != nil {
		s.logger.Error("list stock adjustments", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]stockAdjustmentDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toStockAdjustmentDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

// handleGetStockAdjustment returns one adjustment, gated by stock.adjust at the
// tank's station.
func (s *Server) handleGetStockAdjustment(w http.ResponseWriter, r *http.Request) {
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
	adj, ok := s.stockAdjustmentStation(w, r, actor, id, "stock.adjust")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toStockAdjustmentDTO(adj))
}

// handleApproveStockAdjustment moves requested -> approved (separation of
// duties enforced in the repo), gated by stock.approve_adjustment at the tank's
// station.
func (s *Server) handleApproveStockAdjustment(w http.ResponseWriter, r *http.Request) {
	s.decideStockAdjustment(w, r, "approved", func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, note *string) (*inventory.Adjustment, error) {
		return s.inventory.ApproveAdjustment(r.Context(), tx, actor.TenantID, id, actor.UserID, note)
	})
}

// handleRejectStockAdjustment moves requested|approved -> rejected, gated by
// stock.approve_adjustment at the tank's station.
func (s *Server) handleRejectStockAdjustment(w http.ResponseWriter, r *http.Request) {
	s.decideStockAdjustment(w, r, "rejected", func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, note *string) (*inventory.Adjustment, error) {
		return s.inventory.RejectAdjustment(r.Context(), tx, actor.TenantID, id, actor.UserID, note)
	})
}

// decideStockAdjustment runs an approve/reject transition inside an audited tx,
// authorizing stock.approve_adjustment against the adjustment's tank station.
func (s *Server) decideStockAdjustment(w http.ResponseWriter, r *http.Request, action string, fn func(tx pgx.Tx, actor identity.Actor, id uuid.UUID, note *string) (*inventory.Adjustment, error)) {
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
	if _, ok := s.stockAdjustmentStation(w, r, actor, id, "stock.approve_adjustment"); !ok {
		return
	}
	var req struct {
		Note *string `json:"note,omitempty"`
	}
	// A body is optional; ignore a decode error on an empty body.
	_ = json.NewDecoder(r.Body).Decode(&req)

	var adj *inventory.Adjustment
	ok := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "stock_adjustment." + action, EventType: "StockAdjustment" + action, EntityType: "stock_adjustment", EntityID: id.String(),
	}, func(tx pgx.Tx) (string, error) {
		out, err := fn(tx, actor, id, req.Note)
		if err != nil {
			s.adjustmentLifecycleError(w, err)
			return "", err
		}
		adj = out
		return id.String(), nil
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toStockAdjustmentDTO(adj))
}

// handlePostStockAdjustment posts an approved adjustment to the tank's ledger:
// it appends a single 'adjustment' movement and flips the lifecycle to posted,
// linking the movement and snapshotting before/after book stock. Idempotent and
// immutable (only an approved adjustment posts; posted never re-posts). Gated by
// stock.approve_adjustment at the tank's station.
func (s *Server) handlePostStockAdjustment(w http.ResponseWriter, r *http.Request) {
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
	if _, ok := s.stockAdjustmentStation(w, r, actor, id, "stock.approve_adjustment"); !ok {
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	adj, m, err := s.inventory.PostAdjustment(ctx, tx, actor.TenantID, id, actor.UserID)
	if !s.adjustmentLifecycleError(w, err) {
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "stock_adjustment.posted", EventType: "StockAdjustmentPosted",
		EntityType: "stock_adjustment", EntityID: id.String(),
		NewValue: map[string]any{
			"movement_id": m.ID, "delta_litres": adj.DeltaLitres,
			"balance_before": adj.BalanceBefore, "balance_after": adj.BalanceAfter,
		},
		IP: clientIP(r), UserAgent: r.UserAgent(), RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("post stock adjustment: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toStockAdjustmentDTO(adj))
}
