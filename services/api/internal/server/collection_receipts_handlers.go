package server

// Collection receipts + the handover chain's read surface (Mobile Attendant
// App, Phase 0). A supervisor (cash.confirm) confirms the physical cash
// handed over for a closed shift: the receipt snapshots the expected amount
// and the attendant's submitted total, records what was actually received,
// and computes the difference (received − expected) in SQL numeric.
// Separation of duties: the receiver must not be the cash submission's
// submitter (a system_admin may override during owner-operated backfill
// flows, mirroring the shift-approval SoD). Shift approval is gated on a
// non-rejected receipt existing for the shift's cash submission.

import (
	"context"
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
	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
)

// collectionReceiptDTO mirrors a collection receipt; every money field is the
// exact decimal string from the DB (numeric(14,2) -> "x.xx") — no Go float.
type collectionReceiptDTO struct {
	ID                      uuid.UUID `json:"id"`
	TenantID                uuid.UUID `json:"tenant_id"`
	StationID               uuid.UUID `json:"station_id"`
	ShiftID                 uuid.UUID `json:"shift_id"`
	CashSubmissionID        uuid.UUID `json:"cash_submission_id"`
	ExpectedAmount          string    `json:"expected_amount"`
	AttendantSubmittedTotal string    `json:"attendant_submitted_total"`
	SupervisorReceivedTotal string    `json:"supervisor_received_total"`
	Difference              string    `json:"difference"`
	Status                  string    `json:"status"`
	Reason                  *string   `json:"reason,omitempty"`
	SupervisorComment       *string   `json:"supervisor_comment,omitempty"`
	ReceivedBy              uuid.UUID `json:"received_by"`
	ReceivedAt              string    `json:"received_at"`
}

func toCollectionReceiptDTO(c *operations.CollectionReceipt) collectionReceiptDTO {
	return collectionReceiptDTO{
		ID: c.ID, TenantID: c.TenantID, StationID: c.StationID,
		ShiftID: c.ShiftID, CashSubmissionID: c.CashSubmissionID,
		ExpectedAmount:          c.ExpectedAmount,
		AttendantSubmittedTotal: c.AttendantSubmittedTotal,
		SupervisorReceivedTotal: c.SupervisorReceivedTotal,
		Difference:              c.Difference,
		Status:                  c.Status, Reason: c.Reason, SupervisorComment: c.SupervisorComment,
		ReceivedBy: c.ReceivedBy, ReceivedAt: c.ReceivedAt.Format(time.RFC3339),
	}
}

// requireCollectionReceiptConfirmed is the shift-approval gate (Mobile
// Attendant Phase 0, handover chain): when the shift has a cash submission,
// a non-rejected collection receipt must exist for it. Returns true when the
// gate passes; otherwise writes a 409 with the machine-readable code
// "collection_unconfirmed". Runs through any Querier so the approval handler
// can re-check inside its FOR UPDATE tx.
func (s *Server) requireCollectionReceiptConfirmed(w http.ResponseWriter, ctx context.Context, q database.Querier, tenantID, shiftID uuid.UUID) bool {
	awaiting, err := s.operations.CashSubmissionAwaitingReceipt(ctx, q, tenantID, shiftID)
	if err != nil {
		s.logger.Error("approval receipt gate", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	if awaiting {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":  "confirm the shift's cash submission before approving",
			"code":   "collection_unconfirmed",
			"status": http.StatusConflict,
		})
		return false
	}
	return true
}

// handleGetCollectionReceipt is the supervisor's station-scoped read of a
// shift's collection receipt (station.read; Mobile Attendant Phase 5) — the
// review surface's receipt status. 404 while no receipt exists yet.
func (s *Server) handleGetCollectionReceipt(w http.ResponseWriter, r *http.Request) {
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
	receipt, err := s.operations.GetCollectionReceiptForShift(ctx, actor.TenantID, shift.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no collection receipt for this shift yet")
		return
	}
	if err != nil {
		s.logger.Error("get collection receipt", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toCollectionReceiptDTO(receipt))
}

type confirmCashSubmissionRequest struct {
	ReceivedTotal decimalInput `json:"received_total"`
	// Status is "received" (default) or "rejected"; a non-zero difference
	// upgrades an accepted handover to approved_with_difference server-side.
	Status            string  `json:"status,omitempty"`
	Reason            string  `json:"reason,omitempty"`
	SupervisorComment *string `json:"supervisor_comment,omitempty"`
}

// handleConfirmCashSubmission records the supervisor's collection receipt for
// a closed shift's cash submission (cash.confirm, station-scoped).
func (s *Server) handleConfirmCashSubmission(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req confirmCashSubmissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !req.ReceivedTotal.Valid() {
		writeError(w, http.StatusBadRequest, "received_total must be a non-negative decimal")
		return
	}
	if req.Status != "" && req.Status != "received" && req.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "status must be received or rejected")
		return
	}
	reason := strings.TrimSpace(req.Reason)

	shift, ok := s.shiftForWrite(w, r, actor, "cash.confirm", false)
	if !ok {
		return
	}
	ctx := r.Context()
	if shift.Status != "closed" {
		writeError(w, http.StatusConflict, "cash is confirmed on a closed shift")
		return
	}

	sub, err := s.operations.GetCashSubmission(ctx, actor.TenantID, shift.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "no cash submission to confirm for this shift")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Separation of duties: whoever submitted the cash cannot also confirm
	// receiving it. A system_admin may override during owner-operated
	// backfill flows (mirrors the shift-approval SoD).
	isSystemAdmin, ok := s.actorIsSystemAdmin(w, r, actor)
	if !ok {
		return
	}
	if sub.SubmittedBy == actor.UserID && !isSystemAdmin {
		writeError(w, http.StatusForbidden,
			"separation of duties: you cannot confirm a cash submission you made")
		return
	}

	// difference = received − expected, computed in SQL numeric on the exact
	// decimal strings; the status follows the difference unless rejected.
	_, zero, err := s.operations.DecimalDifference(ctx, req.ReceivedTotal.String(), sub.ExpectedCash)
	if err != nil {
		s.logger.Error("confirm cash: difference", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	status := "received"
	switch {
	case req.Status == "rejected":
		status = "rejected"
	case !zero:
		status = "approved_with_difference"
	}
	if status != "received" && reason == "" {
		writeError(w, http.StatusBadRequest,
			"reason is required when the received total differs from expected or the handover is rejected")
		return
	}
	var reasonPtr *string
	if reason != "" {
		reasonPtr = &reason
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Re-check the shift is still closed under FOR SHARE (conflicts with the
	// FOR UPDATE an approval takes), so a receipt cannot land on a shift that
	// was just approved.
	lockedStatus, err := s.operations.LockShiftStatusForShare(ctx, tx, actor.TenantID, shift.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if lockedStatus != "closed" {
		writeError(w, http.StatusConflict, "cash is confirmed on a closed shift")
		return
	}

	receipt, err := s.operations.InsertCollectionReceipt(ctx, tx, actor.TenantID, operations.CollectionReceiptInput{
		StationID: shift.StationID, ShiftID: shift.ID, CashSubmissionID: sub.ID,
		ExpectedAmount:          sub.ExpectedCash,
		AttendantSubmittedTotal: sub.SubmittedTotal,
		SupervisorReceivedTotal: req.ReceivedTotal.String(),
		Status:                  status, Reason: reasonPtr, SupervisorComment: req.SupervisorComment,
		ReceivedBy: actor.UserID,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "this cash submission already has a collection receipt")
		return
	}
	if err != nil {
		s.logger.Error("confirm cash submission", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// The outbox payload is the receipt DTO plus, additively, the cash
	// submission's submitter — the notification subscriber targets that
	// attendant's feed with the received-vs-expected outcome (Phase 7).
	receiptPayload := struct {
		collectionReceiptDTO
		SubmittedBy uuid.UUID `json:"submitted_by"`
	}{toCollectionReceiptDTO(receipt), sub.SubmittedBy}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "cash.collection_confirmed", EventType: "CashCollectionConfirmed",
		EntityType: "collection_receipt", EntityID: receipt.ID.String(),
		PreviousValue: toCashSubmissionDTO(sub), NewValue: receiptPayload,
		IP: clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("confirm cash submission: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toCollectionReceiptDTO(receipt))
}

// expectedOpeningDTO is one assigned nozzle's expected opening meter — the
// previous shift's final approved closing (verification figure when present,
// raw closing otherwise; null when the nozzle has no prior closing).
type expectedOpeningDTO struct {
	AssignmentID    uuid.UUID  `json:"assignment_id"`
	NozzleID        uuid.UUID  `json:"nozzle_id"`
	AttendantID     uuid.UUID  `json:"attendant_id"`
	ExpectedReading *string    `json:"expected_opening_reading,omitempty"`
	Source          *string    `json:"source,omitempty"`
	SourceShiftID   *uuid.UUID `json:"source_shift_id,omitempty"`
	SourceReadingID *uuid.UUID `json:"source_reading_id,omitempty"`
}

// handleExpectedOpeningReadings returns, per assigned nozzle, the expected
// opening meter for the shift. Readable by the shift's attendants
// (self-scoped) and by station.read holders.
func (s *Server) handleExpectedOpeningReadings(w http.ResponseWriter, r *http.Request) {
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
	// Attendants on the shift read their own expected openings; everyone else
	// needs station.read at the shift's station.
	onShift, err := s.operations.IsAttendantOnShift(ctx, actor.TenantID, shift.ID, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !onShift && !s.authorizeStation(w, r, actor, "station.read", shift.StationID) {
		return
	}

	rows, err := s.operations.ExpectedOpeningsForShift(ctx, actor.TenantID, shift)
	if err != nil {
		s.logger.Error("expected opening readings", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]expectedOpeningDTO, 0, len(rows))
	for i := range rows {
		items = append(items, expectedOpeningDTO{
			AssignmentID: rows[i].AssignmentID, NozzleID: rows[i].NozzleID,
			AttendantID:     rows[i].AttendantID,
			ExpectedReading: rows[i].ExpectedReading, Source: rows[i].Source,
			SourceShiftID: rows[i].SourceShiftID, SourceReadingID: rows[i].SourceReadingID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}
