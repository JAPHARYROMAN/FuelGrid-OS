package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
)

// cashVarianceThreshold is the absolute shortage/excess (in the tenant's
// money units) that auto-raises a cash_variance exception at submission, as an
// exact decimal string — the |variance| > threshold compare is done in SQL
// numeric, never Go float. A per-station configurable threshold is future
// work; this is a sane fixed default.
const cashVarianceThreshold = "1000.00"

// closeLineDTO mirrors a shift close line; every money/litre field is the exact
// decimal string from the DB (litres numeric(14,3) -> "x.xxx"; price/value
// numeric(14,2) -> "x.xx") — no Go float (MD-5/OPS-001).
type closeLineDTO struct {
	NozzleID       uuid.UUID `json:"nozzle_id"`
	OpeningReading string    `json:"opening_reading"`
	ClosingReading string    `json:"closing_reading"`
	LitresSold     string    `json:"litres_sold"`
	UnitPrice      string    `json:"unit_price"`
	ExpectedValue  string    `json:"expected_value"`
}

func toCloseLineDTO(l *operations.CloseLine) closeLineDTO {
	return closeLineDTO{
		NozzleID: l.NozzleID, OpeningReading: l.OpeningReading, ClosingReading: l.ClosingReading,
		LitresSold: l.LitresSold, UnitPrice: l.UnitPrice, ExpectedValue: l.ExpectedValue,
	}
}

// cashSubmissionDTO mirrors a cash submission; every money field is the exact
// decimal string (numeric(14,2) -> "x.xx") from the DB — no Go float (MD-5).
type cashSubmissionDTO struct {
	ID                uuid.UUID `json:"id"`
	ShiftID           uuid.UUID `json:"shift_id"`
	ExpectedCash      string    `json:"expected_cash"`
	CashAmount        string    `json:"cash_amount"`
	MobileMoneyAmount string    `json:"mobile_money_amount"`
	CardAmount        string    `json:"card_amount"`
	CreditAmount      string    `json:"credit_amount"`
	SubmittedTotal    string    `json:"submitted_total"`
	Variance          string    `json:"variance"`
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

	// Index meter readings (nozzle -> opening/closing) as exact decimal strings;
	// all close-line litres/value arithmetic happens in SQL numeric, never Go
	// float (MD-5/OPS-001).
	type ends struct{ opening, closing *string }
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
	// Meter rollbacks (closing < opening) are detected in SQL numeric, not by
	// parsing the readings to float.
	rollbackList, err := s.readings.RollbackNozzlesForShift(ctx, actor.TenantID, shift.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	rolledBack := make(map[uuid.UUID]bool, len(rollbackList))
	for _, id := range rollbackList {
		rolledBack[id] = true
	}

	var (
		lineInputs      []operations.CloseLineInput
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
		if rolledBack[nozzleID] {
			rollbackNozzles = append(rollbackNozzles, nozzleID)
			continue
		}
		// Readings and the nozzle's default price stay exact decimal strings;
		// litres_sold and expected_value are computed in SQL on insert.
		lineInputs = append(lineInputs, operations.CloseLineInput{
			ShiftID: shift.ID, NozzleID: nozzleID,
			OpeningReading: *e.opening, ClosingReading: *e.closing,
			UnitPrice: nozzle.DefaultPrice,
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

	// Lock the shift row and re-validate it is still open inside the tx, before
	// inserting any close lines. A second concurrent close blocks here, then
	// sees 'closed' and is rejected — so a shift cannot be closed twice and its
	// per-nozzle close-line snapshot cannot be written twice (OPS-008). The
	// narrower window where a meter/dip correction commits between the reads
	// above and this lock is tracked as a follow-up (the record paths must take
	// FOR SHARE on the shift to fully serialize).
	if locked, err := s.operations.GetShiftForUpdate(ctx, tx, actor.TenantID, shift.ID); errors.Is(err, operations.ErrShiftNotFound) {
		writeError(w, http.StatusNotFound, "shift not found")
		return
	} else if err != nil {
		s.logger.Error("close shift: lock", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if locked.Status != "open" {
		writeError(w, http.StatusConflict, "shift is not open")
		return
	}

	lines := make([]operations.CloseLine, 0, len(lineInputs))
	for i := range lineInputs {
		line, err := s.operations.InsertCloseLine(ctx, tx, actor.TenantID, lineInputs[i])
		if err != nil {
			s.logger.Error("close shift: line", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		lines = append(lines, *line)
	}
	closed, err := s.operations.CloseShift(ctx, tx, actor.TenantID, shift.ID, actor.UserID)
	if err != nil {
		s.logger.Error("close shift", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Total expected cash is SUM(expected_value) computed in SQL numeric. Sum
	// inside the tx so the just-inserted (uncommitted) lines are visible.
	expected, err := s.operations.SumExpectedForShift(ctx, tx, actor.TenantID, shift.ID)
	if err != nil {
		s.logger.Error("close shift: expected", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
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
	expected, err := s.operations.SumExpectedForShift(ctx, s.operations.Pool(), actor.TenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	body := map[string]any{
		"shift": toShiftDTO(shift), "lines": lineDTOs, "expected_cash": expected,
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

// cashSubmissionRequest carries the tender breakdown as exact decimal strings
// (numeric(14,2)); submitted_total and variance are computed in SQL. Omitted
// tenders default to "0" so a single-tender submission stays terse.
type cashSubmissionRequest struct {
	CashAmount        string  `json:"cash_amount"`
	MobileMoneyAmount string  `json:"mobile_money_amount"`
	CardAmount        string  `json:"card_amount"`
	CreditAmount      string  `json:"credit_amount"`
	Notes             *string `json:"notes,omitempty"`
}

// tenderOrZero defaults an omitted tender to "0" and validates the rest is a
// non-negative decimal (the leading-sign-free decimalPattern already rejects
// negatives). Returns the trimmed value and whether it is acceptable.
func tenderOrZero(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "0", true
	}
	return v, validDecimal(v)
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
	cashAmt, ok1 := tenderOrZero(req.CashAmount)
	mmAmt, ok2 := tenderOrZero(req.MobileMoneyAmount)
	cardAmt, ok3 := tenderOrZero(req.CardAmount)
	creditAmt, ok4 := tenderOrZero(req.CreditAmount)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		writeError(w, http.StatusBadRequest, "tender amounts must be non-negative decimals")
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
	// Expected cash is SUM(expected_value) of the (already committed) close
	// lines, computed in SQL numeric.
	expected, err := s.operations.SumExpectedForShift(ctx, s.operations.Pool(), actor.TenantID, shift.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

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

	// submitted_total (sum of tenders) and variance (total - expected) are
	// computed in SQL numeric inside the insert; no Go float money.
	sub, err := s.operations.InsertCashSubmission(ctx, tx, actor.TenantID, operations.CashSubmissionInput{
		ShiftID: shift.ID, ExpectedCash: expected,
		CashAmount: cashAmt, MobileMoneyAmount: mmAmt,
		CardAmount: cardAmt, CreditAmount: creditAmt,
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
	// block approval until a supervisor resolves it. The |variance| > threshold
	// compare is done in SQL numeric so it fires on the exact decimal figure.
	over, err := s.operations.VarianceExceeds(ctx, sub.Variance, cashVarianceThreshold)
	if err != nil {
		s.logger.Error("submit cash: variance threshold", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if over {
		exc, err := s.operations.RaiseException(ctx, tx, actor.TenantID, shift.ID,
			"cash_variance", "high",
			fmt.Sprintf("cash variance %s exceeds threshold %s", sub.Variance, cashVarianceThreshold))
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
