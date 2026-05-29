package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
	"github.com/japharyroman/fuelgrid-os/internal/readings"
)

// cashVarianceThreshold is the absolute shortage/excess (in the tenant's
// money units) that auto-raises a cash_variance exception at submission.
// A per-station configurable threshold is future work; this is a sane
// fixed default.
const cashVarianceThreshold = 1000.0

type closeLineDTO struct {
	NozzleID       uuid.UUID `json:"nozzle_id"`
	OpeningReading float64   `json:"opening_reading"`
	ClosingReading float64   `json:"closing_reading"`
	LitresSold     float64   `json:"litres_sold"`
	UnitPrice      float64   `json:"unit_price"`
	ExpectedValue  float64   `json:"expected_value"`
}

func toCloseLineDTO(l *operations.CloseLine) closeLineDTO {
	return closeLineDTO{
		NozzleID: l.NozzleID, OpeningReading: l.OpeningReading, ClosingReading: l.ClosingReading,
		LitresSold: l.LitresSold, UnitPrice: l.UnitPrice, ExpectedValue: l.ExpectedValue,
	}
}

type cashSubmissionDTO struct {
	ID                uuid.UUID `json:"id"`
	ShiftID           uuid.UUID `json:"shift_id"`
	ExpectedCash      float64   `json:"expected_cash"`
	CashAmount        float64   `json:"cash_amount"`
	MobileMoneyAmount float64   `json:"mobile_money_amount"`
	CardAmount        float64   `json:"card_amount"`
	CreditAmount      float64   `json:"credit_amount"`
	SubmittedTotal    float64   `json:"submitted_total"`
	Variance          float64   `json:"variance"`
	SubmittedBy       uuid.UUID `json:"submitted_by"`
	SubmittedAt       string    `json:"submitted_at"`
	Notes             *string   `json:"notes,omitempty"`
}

func toCashSubmissionDTO(c *operations.CashSubmission) cashSubmissionDTO {
	return cashSubmissionDTO{
		ID: c.ID, ShiftID: c.ShiftID, ExpectedCash: c.ExpectedCash,
		CashAmount: c.CashAmount, MobileMoneyAmount: c.MobileMoneyAmount,
		CardAmount: c.CardAmount, CreditAmount: c.CreditAmount,
		SubmittedTotal: c.SubmittedTotal, Variance: c.Variance,
		SubmittedBy: c.SubmittedBy, SubmittedAt: c.SubmittedAt.Format(time.RFC3339), Notes: c.Notes,
	}
}

func sumExpected(lines []operations.CloseLine) float64 {
	var total float64
	for i := range lines {
		total += lines[i].ExpectedValue
	}
	return total
}

// handleCloseShift validates that every assigned nozzle has an
// opening+closing meter reading and every tank behind them has a closing
// dip, snapshots a per-nozzle close line, totals expected cash, and flips
// the shift to closed — all in one transaction.
func (s *Server) handleCloseShift(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	shift, ok := s.shiftForWrite(w, r, actor, "shift.close", true)
	if !ok {
		return
	}
	ctx := r.Context()

	assignments, err := s.operations.ListNozzleAssignments(ctx, actor.TenantID, shift.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// A shift with no nozzle assignments has no sales to reconcile; closing it
	// would produce an approved, zero-expected-cash record with no lines and
	// no exception (audit P2). Require at least one assignment.
	if len(assignments) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "shift has no nozzle assignments; assign at least one before closing")
		return
	}
	meterRows, err := s.readings.ListActiveForShift(ctx, actor.TenantID, shift.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	dipRows, err := s.readings.ListDipsForShift(ctx, actor.TenantID, shift.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Index meter readings (nozzle -> opening/closing) and tanks with a
	// closing dip.
	type ends struct{ opening, closing *float64 }
	meterByNozzle := map[uuid.UUID]*ends{}
	for i := range meterRows {
		e := meterByNozzle[meterRows[i].NozzleID]
		if e == nil {
			e = &ends{}
			meterByNozzle[meterRows[i].NozzleID] = e
		}
		v := meterRows[i].Reading
		if meterRows[i].ReadingType == "opening" {
			e.opening = &v
		} else {
			e.closing = &v
		}
	}
	closingDipTanks := map[uuid.UUID]bool{}
	for i := range dipRows {
		if dipRows[i].ReadingType == "closing" {
			closingDipTanks[dipRows[i].TankID] = true
		}
	}

	var (
		lines           []operations.CloseLine
		missingMeter    []uuid.UUID
		rollbackNozzles []uuid.UUID
		missingDip      []uuid.UUID
		dipTanksSeen    = map[uuid.UUID]bool{}
	)
	for i := range assignments {
		nozzleID := assignments[i].NozzleID
		nozzle, err := s.nozzles.Get(ctx, actor.TenantID, nozzleID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		// Closing dip required for each tank behind an assigned nozzle.
		if !dipTanksSeen[nozzle.TankID] {
			dipTanksSeen[nozzle.TankID] = true
			if !closingDipTanks[nozzle.TankID] {
				missingDip = append(missingDip, nozzle.TankID)
			}
		}
		e := meterByNozzle[nozzleID]
		if e == nil || e.opening == nil || e.closing == nil {
			missingMeter = append(missingMeter, nozzleID)
			continue
		}
		litres, err := readings.LitresDispensed(*e.opening, *e.closing)
		if err != nil {
			rollbackNozzles = append(rollbackNozzles, nozzleID)
			continue
		}
		lines = append(lines, operations.CloseLine{
			ShiftID: shift.ID, NozzleID: nozzleID,
			OpeningReading: *e.opening, ClosingReading: *e.closing,
			LitresSold: litres, UnitPrice: nozzle.DefaultPrice,
			ExpectedValue: litres * nozzle.DefaultPrice,
		})
	}

	if len(missingMeter) > 0 || len(missingDip) > 0 || len(rollbackNozzles) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":                  "shift is missing readings required to close",
			"missing_meter_nozzles":  missingMeter,
			"missing_dip_tanks":      missingDip,
			"meter_rollback_nozzles": rollbackNozzles,
		})
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for i := range lines {
		if err := s.operations.InsertCloseLine(ctx, tx, actor.TenantID, lines[i]); err != nil {
			s.logger.Error("close shift: line", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	closed, err := s.operations.CloseShift(ctx, tx, actor.TenantID, shift.ID, actor.UserID)
	if err != nil {
		s.logger.Error("close shift", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	expected := sumExpected(lines)
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "shift.closed", EventType: "ShiftClosed",
		EntityType: "shift", EntityID: closed.ID.String(),
		PreviousValue: toShiftDTO(shift),
		NewValue:      map[string]any{"shift": toShiftDTO(closed), "expected_cash": expected, "lines": len(lines)},
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

	lineDTOs := make([]closeLineDTO, 0, len(lines))
	for i := range lines {
		lineDTOs = append(lineDTOs, toCloseLineDTO(&lines[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"shift": toShiftDTO(closed), "lines": lineDTOs, "expected_cash": expected,
	})
}

func (s *Server) handleCloseSummary(w http.ResponseWriter, r *http.Request) {
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

	lines, err := s.operations.ListCloseLines(ctx, actor.TenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	lineDTOs := make([]closeLineDTO, 0, len(lines))
	for i := range lines {
		lineDTOs = append(lineDTOs, toCloseLineDTO(&lines[i]))
	}

	body := map[string]any{
		"shift": toShiftDTO(shift), "lines": lineDTOs, "expected_cash": sumExpected(lines),
		"cash_submission": nil,
	}
	cash, err := s.operations.GetCashSubmission(ctx, actor.TenantID, id)
	if err == nil {
		body["cash_submission"] = toCashSubmissionDTO(cash)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, body)
}

type cashSubmissionRequest struct {
	CashAmount        float64 `json:"cash_amount"`
	MobileMoneyAmount float64 `json:"mobile_money_amount"`
	CardAmount        float64 `json:"card_amount"`
	CreditAmount      float64 `json:"credit_amount"`
	Notes             *string `json:"notes,omitempty"`
}

func (s *Server) handleSubmitCash(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req cashSubmissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.CashAmount < 0 || req.MobileMoneyAmount < 0 || req.CardAmount < 0 || req.CreditAmount < 0 {
		writeError(w, http.StatusBadRequest, "tender amounts must be non-negative")
		return
	}

	// Self-scoped: an attendant (cash.submit) may submit only for a shift
	// they're on; a supervisor (cash.override) may submit for any shift.
	shift, _, ok := s.shiftForScopedWrite(w, r, actor, "cash.submit", "cash.override", false)
	if !ok {
		return
	}
	if shift.Status != "closed" {
		writeError(w, http.StatusConflict, "shift must be closed before cash is submitted")
		return
	}

	ctx := r.Context()
	lines, err := s.operations.ListCloseLines(ctx, actor.TenantID, shift.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	expected := sumExpected(lines)
	total := req.CashAmount + req.MobileMoneyAmount + req.CardAmount + req.CreditAmount

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the shift row FOR SHARE and re-check its status inside the tx. This
	// conflicts with the FOR UPDATE a shift approval takes, so the cash_variance
	// exception this may raise cannot be missed by a concurrent approval, and
	// cash cannot be submitted onto a shift that was just approved (OPS-009).
	lockedStatus, err := s.operations.LockShiftStatusForShare(ctx, tx, actor.TenantID, shift.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if lockedStatus != "closed" {
		writeError(w, http.StatusConflict, "shift must be closed before cash is submitted")
		return
	}

	sub, err := s.operations.InsertCashSubmission(ctx, tx, actor.TenantID, operations.CashSubmissionInput{
		ShiftID: shift.ID, ExpectedCash: expected,
		CashAmount: req.CashAmount, MobileMoneyAmount: req.MobileMoneyAmount,
		CardAmount: req.CardAmount, CreditAmount: req.CreditAmount,
		SubmittedTotal: total, Variance: total - expected,
		SubmittedBy: actor.UserID, Notes: req.Notes,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "cash has already been submitted for this shift")
		return
	}
	if err != nil {
		s.logger.Error("submit cash", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	reqID := chimiddleware.GetReqID(ctx)
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "cash.submitted", EventType: "CashSubmitted",
		EntityType: "cash_submission", EntityID: sub.ID.String(),
		NewValue: toCashSubmissionDTO(sub),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: reqID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// A cash variance over the threshold auto-raises an exception that will
	// block approval until a supervisor resolves it.
	if math.Abs(sub.Variance) > cashVarianceThreshold {
		exc, err := s.operations.RaiseException(ctx, tx, actor.TenantID, shift.ID,
			"cash_variance", "high",
			fmt.Sprintf("cash variance %.2f exceeds threshold %.2f", sub.Variance, cashVarianceThreshold))
		if err != nil {
			s.logger.Error("submit cash: raise exception", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "shift_exception.raised", EventType: "ShiftExceptionRaised",
			EntityType: "shift_exception", EntityID: exc.ID.String(),
			NewValue: toShiftExceptionDTO(exc),
			IP:       clientIP(r), UserAgent: r.UserAgent(),
			RequestID: reqID,
		}); err != nil {
			s.logger.Error("submit cash: audit exception", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toCashSubmissionDTO(sub))
}
