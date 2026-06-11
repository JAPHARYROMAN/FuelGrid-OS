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
	"github.com/japharyroman/fuelgrid-os/internal/operations"
)

type shiftExceptionDTO struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	ShiftID    uuid.UUID  `json:"shift_id"`
	Type       string     `json:"type"`
	Severity   string     `json:"severity"`
	Detail     *string    `json:"detail,omitempty"`
	Status     string     `json:"status"`
	RaisedAt   string     `json:"raised_at"`
	ResolvedBy *uuid.UUID `json:"resolved_by,omitempty"`
	ResolvedAt *string    `json:"resolved_at,omitempty"`
}

func toShiftExceptionDTO(e *operations.ShiftException) shiftExceptionDTO {
	return shiftExceptionDTO{
		ID: e.ID, TenantID: e.TenantID, ShiftID: e.ShiftID,
		Type: e.Type, Severity: e.Severity, Detail: e.Detail, Status: e.Status,
		RaisedAt: e.RaisedAt.Format(time.RFC3339), ResolvedBy: e.ResolvedBy,
		ResolvedAt: fmtTime(e.ResolvedAt),
	}
}

type approveShiftRequest struct {
	Status string `json:"status"`
}

// handleApproveShift transitions a closed shift to approved. It refuses
// while any unresolved exception remains on the shift.
func (s *Server) handleApproveShift(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req approveShiftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Status != "approved" {
		writeError(w, http.StatusBadRequest, "status must be approved (close is via POST /close)")
		return
	}

	before, ok := s.shiftForWrite(w, r, actor, "shift.approve", false)
	if !ok {
		return
	}
	if before.Status != "closed" {
		writeError(w, http.StatusConflict, "only a closed shift can be approved")
		return
	}
	// Separation of duties (OPS-002): the approver must not be whoever closed
	// the shift, so cash collection and its sign-off are not the same person.
	// A system_admin may override this during owner-operated/backfill flows.
	isSystemAdmin, ok := s.actorIsSystemAdmin(w, r, actor)
	if !ok {
		return
	}
	if before.ClosedBy != nil && *before.ClosedBy == actor.UserID && !isSystemAdmin {
		writeError(w, http.StatusForbidden, "separation of duties: you cannot approve a shift you closed")
		return
	}

	ctx := r.Context()
	open, err := s.operations.OpenExceptionCountForShift(ctx, s.deps.DB, actor.TenantID, before.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if open > 0 {
		writeError(w, http.StatusConflict, "resolve the shift's open exceptions before approving")
		return
	}
	// Mobile Attendant Phase 0 gate: every ACTIVE closing reading must carry a
	// supervisor verification (dual-value model) before the shift can be
	// approved — expected collection must derive from verified figures.
	if !s.requireClosingReadingsVerified(w, ctx, s.deps.DB, actor.TenantID, before.ID) {
		return
	}
	// Handover-chain gate: a cash submission must carry a non-rejected
	// collection receipt before its shift can be approved.
	if !s.requireCollectionReceiptConfirmed(w, ctx, s.deps.DB, actor.TenantID, before.ID) {
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the shift row, then re-validate its status and open-exception count
	// inside the tx. Cash submission takes FOR SHARE on this same row before it
	// raises an exception, so a late exception cannot slip past approval and a
	// second approval cannot race this one (OPS-009).
	locked, err := s.operations.GetShiftForUpdate(ctx, tx, actor.TenantID, before.ID)
	if errors.Is(err, operations.ErrShiftNotFound) {
		writeError(w, http.StatusNotFound, "shift not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if locked.Status != "closed" {
		writeError(w, http.StatusConflict, "only a closed shift can be approved")
		return
	}
	openInTx, err := s.operations.OpenExceptionCountForShift(ctx, tx, actor.TenantID, before.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if openInTx > 0 {
		writeError(w, http.StatusConflict, "resolve the shift's open exceptions before approving")
		return
	}
	// Re-check the verification gate under the shift's FOR UPDATE lock, so a
	// reading captured/corrected concurrently cannot slip past approval
	// unverified.
	if !s.requireClosingReadingsVerified(w, ctx, tx, actor.TenantID, before.ID) {
		return
	}
	// Re-check the receipt gate under the same lock: cash submission takes
	// FOR SHARE on the shift row, so a submission landing concurrently cannot
	// slip past approval unconfirmed.
	if !s.requireCollectionReceiptConfirmed(w, ctx, tx, actor.TenantID, before.ID) {
		return
	}

	after, err := s.operations.ApproveShift(ctx, tx, actor.TenantID, before.ID, actor.UserID)
	if err != nil {
		s.logger.Error("approve shift", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Phase 4: post the shift's metered sales to the stock ledger in the same
	// tx, so sales and the inventory position commit atomically. Tanks not yet
	// onboarded (no opening balance) are skipped, not blocking — approval stays
	// independent of inventory rollout.
	if !s.postShiftSales(w, r, actor, tx, before.ID) {
		return
	}

	// Phase 6: recognize priced revenue for the shift in the same tx —
	// value the metered litres at the resolved price into sale records.
	if !s.recognizeShiftRevenue(w, r, actor, tx, before.ID) {
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift.approved", EventType: "ShiftApproved",
		EntityType: "shift", EntityID: after.ID.String(),
		PreviousValue: toShiftDTO(before), NewValue: toShiftDTO(after),
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
	writeJSON(w, http.StatusOK, toShiftDTO(after))
}

// postShiftSales aggregates a shift's metered litres-sold per tank and posts
// them to the stock ledger inside the approval tx, auditing each as a
// stock_movement.posted (source=sales). It returns false (after writing the
// response) only on an internal error; skipped un-opened tanks are logged but
// do not block approval. Idempotent via PostSalesForShift.
func (s *Server) postShiftSales(w http.ResponseWriter, r *http.Request, actor identity.Actor, tx pgx.Tx, shiftID uuid.UUID) bool {
	ctx := r.Context()
	saleRows, err := s.operations.LitresSoldPerTankForShift(ctx, actor.TenantID, shiftID)
	if err != nil {
		s.logger.Error("approve shift: aggregate sales", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	lines := make([]inventory.SaleLine, 0, len(saleRows))
	for _, ts := range saleRows {
		lines = append(lines, inventory.SaleLine{TankID: ts.TankID, LitresSold: ts.LitresSold})
	}

	posted, skipped, err := s.inventory.PostSalesForShift(ctx, tx, actor.TenantID, shiftID, actor.UserID, lines)
	if err != nil {
		s.logger.Error("approve shift: post sales", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	if len(skipped) > 0 {
		s.logger.Warn("shift sales not posted: tanks have no opening balance",
			"shift", shiftID, "tanks", skipped)
	}
	for i := range posted {
		if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "stock_movement.posted", EventType: "StockMovementPosted",
			EntityType: "stock_movement", EntityID: posted[i].ID.String(),
			NewValue: toStockMovementDTO(&posted[i]),
			IP:       clientIP(r), UserAgent: r.UserAgent(),
			RequestID: chimiddleware.GetReqID(ctx),
		}); err != nil {
			s.logger.Error("approve shift: sales audit", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return false
		}
	}
	return true
}

func (s *Server) handleListShiftExceptions(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid shift id")
		return
	}
	ctx := r.Context()
	shift, err := s.operations.GetShift(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "shift not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", shift.StationID) {
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.operations.ListExceptionsForShiftPage(ctx, actor.TenantID, id, limit+1, offset)
	if err != nil {
		s.logger.Error("list shift exceptions", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]shiftExceptionDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toShiftExceptionDTO(&rows[i]))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

type resolveExceptionRequest struct {
	Status string `json:"status"`
}

func (s *Server) handleResolveShiftException(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid exception id")
		return
	}
	var req resolveExceptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Status != "resolved" {
		writeError(w, http.StatusBadRequest, "status must be resolved")
		return
	}

	ctx := r.Context()
	exc, err := s.operations.GetException(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "exception not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Authorize against the exception's shift station.
	shift, err := s.operations.GetShift(ctx, actor.TenantID, exc.ShiftID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !s.authorizeStation(w, r, actor, "shift.approve", shift.StationID) {
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	resolved, err := s.operations.ResolveException(ctx, tx, actor.TenantID, id, actor.UserID)
	if errors.Is(err, operations.ErrExceptionNotFound) {
		writeError(w, http.StatusConflict, "exception is already resolved")
		return
	}
	if err != nil {
		s.logger.Error("resolve shift exception", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift_exception.resolved", EventType: "ShiftExceptionResolved",
		EntityType: "shift_exception", EntityID: resolved.ID.String(),
		PreviousValue: toShiftExceptionDTO(exc), NewValue: toShiftExceptionDTO(resolved),
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
	writeJSON(w, http.StatusOK, toShiftExceptionDTO(resolved))
}
