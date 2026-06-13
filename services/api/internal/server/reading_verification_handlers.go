package server

// Supervisor verification of closing meter readings — the dual-value model
// (Mobile Attendant App, Phase 0). The attendant's original meter_readings
// row is NEVER mutated; every supervisor decision lands in
// reading_verifications with the submitted figure snapshotted next to the
// final approved figure. Both endpoints ride the station-scoped
// reading.override permission (0021) — the same supervisor authority over a
// station's readings — and enforce separation of duties: the verifier must
// not be the reading's recorder.

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
	"github.com/japharyroman/fuelgrid-os/internal/readings"
)

// requireClosingReadingsVerified is the shift-approval gate (Mobile Attendant
// Phase 0, extended for the PRD §7.8/§9.5 closeout): every ACTIVE closing
// reading of the shift must be in a TERMINAL-GOOD verification state
// {approved, corrected}. A reading with no verification yet, or one held by a
// 'rejected' (attendant must re-capture) or 'flagged' (under investigation)
// verdict, blocks approval. Returns true when the gate passes; otherwise writes
// a 409 with a machine-readable code so clients can branch to the right flow:
//
//	readings_unverified        — readings still need a supervisor verdict.
//	readings_rejected_pending  — a rejected reading is awaiting attendant re-capture.
//	readings_flagged_pending   — a flagged reading is under investigation.
//
// Rejected/flagged take precedence over plain unverified in the reported code
// because a hold is a more specific (and more actionable) blocker. Runs through
// any Querier so the approval handler can re-check inside its FOR UPDATE tx.
func (s *Server) requireClosingReadingsVerified(w http.ResponseWriter, ctx context.Context, q database.Querier, tenantID, shiftID uuid.UUID) bool {
	unverified, rejected, flagged, err := s.readings.ClosingVerificationGateCounts(ctx, q, tenantID, shiftID)
	if err != nil {
		s.logger.Error("approval verification gate", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	switch {
	case rejected > 0:
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":          "a rejected closing reading is awaiting attendant re-capture before approving",
			"code":           "readings_rejected_pending",
			"status":         http.StatusConflict,
			"rejected_count": rejected,
		})
		return false
	case flagged > 0:
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":         "a flagged closing reading is under investigation before approving",
			"code":          "readings_flagged_pending",
			"status":        http.StatusConflict,
			"flagged_count": flagged,
		})
		return false
	case unverified > 0:
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":            "verify the shift's closing readings before approving",
			"code":             "readings_unverified",
			"status":           http.StatusConflict,
			"unverified_count": unverified,
		})
		return false
	}
	return true
}

// readingVerificationDTO mirrors a verification row; the three reading
// figures are exact decimal STRINGS (numeric(14,3) -> text), never Go float.
type readingVerificationDTO struct {
	ID                        uuid.UUID `json:"id"`
	TenantID                  uuid.UUID `json:"tenant_id"`
	StationID                 uuid.UUID `json:"station_id"`
	ShiftID                   uuid.UUID `json:"shift_id"`
	NozzleID                  uuid.UUID `json:"nozzle_id"`
	ReadingID                 uuid.UUID `json:"reading_id"`
	AttendantSubmittedReading string    `json:"attendant_submitted_reading"`
	SupervisorVerifiedReading *string   `json:"supervisor_verified_reading,omitempty"`
	FinalApprovedReading      string    `json:"final_approved_reading"`
	Status                    string    `json:"status"`
	Reason                    *string   `json:"reason,omitempty"`
	VerifiedBy                uuid.UUID `json:"verified_by"`
	VerifiedAt                string    `json:"verified_at"`
}

func toReadingVerificationDTO(v *readings.Verification) readingVerificationDTO {
	return readingVerificationDTO{
		ID: v.ID, TenantID: v.TenantID, StationID: v.StationID,
		ShiftID: v.ShiftID, NozzleID: v.NozzleID, ReadingID: v.ReadingID,
		AttendantSubmittedReading: v.AttendantSubmittedReading,
		SupervisorVerifiedReading: v.SupervisorVerifiedReading,
		FinalApprovedReading:      v.FinalApprovedReading,
		Status:                    v.Status, Reason: v.Reason,
		VerifiedBy: v.VerifiedBy, VerifiedAt: v.VerifiedAt.Format(time.RFC3339),
	}
}

// shiftForVerification loads the shift and authorizes the actor for
// reading.override at its station. Returns the shift + ok; writes the error
// response on failure.
func (s *Server) shiftForVerification(w http.ResponseWriter, r *http.Request, actor identity.Actor) (*operations.Shift, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid shift id")
		return nil, false
	}
	shift, err := s.operations.GetShift(r.Context(), actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "shift not found")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	if !s.authorizeStation(w, r, actor, "reading.override", shift.StationID) {
		return nil, false
	}
	return shift, true
}

// handleListReadingVerifications is the supervisor's station-scoped read of a
// shift's verification set (station.read; Mobile Attendant Phase 5) — the
// review surface renders pending vs verified from this list joined to the
// shift's meter readings, including both values + reason on corrections.
func (s *Server) handleListReadingVerifications(w http.ResponseWriter, r *http.Request) {
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
	all, err := s.readings.ListVerificationsForShift(ctx, actor.TenantID, shift.ID)
	if err != nil {
		s.logger.Error("list reading verifications", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]readingVerificationDTO, 0, len(all))
	for i := range all {
		items = append(items, toReadingVerificationDTO(&all[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

// handleVerifyShiftReadings batch-approves every ACTIVE closing reading of
// the shift as-is (final = submitted, status approved). Idempotent: readings
// that already carry a verification are skipped; the response always returns
// the shift's full verification set. Separation of duties: the batch is
// refused outright when it contains a reading the actor recorded.
func (s *Server) handleVerifyShiftReadings(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	shift, ok := s.shiftForVerification(w, r, actor)
	if !ok {
		return
	}
	ctx := r.Context()

	pending, err := s.readings.UnverifiedClosingForShift(ctx, actor.TenantID, shift.ID)
	if err != nil {
		s.logger.Error("verify shift readings: pending", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Separation of duties (mirrors the no-self-approve rule on inventory
	// adjustments): you cannot verify a reading you recorded.
	for i := range pending {
		if pending[i].RecordedBy == actor.UserID {
			writeError(w, http.StatusForbidden,
				"separation of duties: you cannot verify readings you recorded")
			return
		}
	}

	newlyVerified := 0
	if len(pending) > 0 {
		tx, err := s.deps.DB.Begin(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()

		for i := range pending {
			m := &pending[i]
			v, err := s.readings.InsertVerification(ctx, tx, actor.TenantID, readings.VerificationInput{
				StationID: shift.StationID, ShiftID: shift.ID, NozzleID: m.NozzleID,
				ReadingID:                 m.ID,
				AttendantSubmittedReading: m.Reading,
				FinalApprovedReading:      m.Reading,
				Status:                    "approved",
				VerifiedBy:                actor.UserID,
			})
			if isUniqueViolation(err) {
				// Raced a concurrent verification of the same reading; it is no
				// longer pending, which is exactly what this batch wanted.
				continue
			}
			if err != nil {
				s.logger.Error("verify shift readings", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			newlyVerified++
			// recorded_by rides the payload additively so the notification
			// subscriber can target the recorder's feed ("your readings were
			// approved", Phase 7 wiring) without re-reading the meter row.
			approvedPayload := map[string]any{
				"verification": toReadingVerificationDTO(v),
				"recorded_by":  m.RecordedBy,
			}
			if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
				TenantID: actor.TenantID, ActorID: actor.UserID,
				Action: "reading_verification.approved", EventType: "ReadingVerificationApproved",
				EntityType: "reading_verification", EntityID: v.ID.String(),
				NewValue: approvedPayload,
				IP:       clientIP(r), UserAgent: r.UserAgent(),
				RequestID: chimiddleware.GetReqID(ctx),
			}); err != nil {
				s.logger.Error("verify shift readings: audit", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}
		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	all, err := s.readings.ListVerificationsForShift(ctx, actor.TenantID, shift.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]readingVerificationDTO, 0, len(all))
	for i := range all {
		items = append(items, toReadingVerificationDTO(&all[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "count": len(items), "newly_verified": newlyVerified,
	})
}

type verifyCorrectRequest struct {
	VerifiedReading decimalInput `json:"verified_reading"`
	Reason          string       `json:"reason"`
}

// handleVerifyCorrectReading records a supervisor correction of one closing
// reading: status corrected, supervisor + final = the verified figure, the
// attendant's submission snapshotted unchanged, reason mandatory. The
// meter_readings row is never touched. If the shift is already closed, the
// affected shift_close_lines row is recomputed (closing, litres_sold,
// expected_value) inside the same tx — the originals stay recoverable from
// the verification snapshot.
func (s *Server) handleVerifyCorrectReading(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	readingID, err := uuid.Parse(chi.URLParam(r, "readingID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reading id")
		return
	}
	var req verifyCorrectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !req.VerifiedReading.Valid() {
		writeError(w, http.StatusBadRequest, "verified_reading must be a non-negative decimal")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required when correcting a reading")
		return
	}

	shift, ok := s.shiftForVerification(w, r, actor)
	if !ok {
		return
	}
	ctx := r.Context()

	// Once approved, the shift's sales and revenue are posted from the frozen
	// lines; a correction now would desync approved facts.
	if shift.Status == "approved" {
		writeError(w, http.StatusConflict, "shift is already approved")
		return
	}

	reading, err := s.readings.Get(ctx, actor.TenantID, readingID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "reading not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if reading.ShiftID != shift.ID {
		writeError(w, http.StatusBadRequest, "reading does not belong to this shift")
		return
	}
	if reading.ReadingType != "closing" {
		writeError(w, http.StatusUnprocessableEntity, "only closing readings are verified")
		return
	}
	if reading.Status != "active" {
		writeError(w, http.StatusConflict, "reading is superseded; verify its active correction instead")
		return
	}
	// Separation of duties: the verifier must not be the recorder.
	if reading.RecordedBy == actor.UserID {
		writeError(w, http.StatusForbidden,
			"separation of duties: you cannot verify a reading you recorded")
		return
	}
	// The corrected figure must respect the nozzle's meter precision, like
	// every other reading write.
	nozzle, err := s.nozzles.Get(ctx, actor.TenantID, reading.NozzleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := readings.ValidateScale(dispDecimal(req.VerifiedReading.String()), nozzle.MeterDecimalPlaces); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "verified_reading has more decimals than the nozzle's meter precision")
		return
	}
	// A corrected closing below the nozzle's opening would drive litres
	// negative — reject it whether or not the shift has closed yet (the
	// closed path's recompute re-checks on the frozen line). The compare is
	// SQL numeric on the exact decimal strings.
	if opening, err := s.readings.ActiveForShiftNozzle(ctx, actor.TenantID, shift.ID, reading.NozzleID, "opening"); err == nil {
		below, lerr := s.operations.DecimalLess(ctx, req.VerifiedReading.String(), opening.Reading)
		if lerr != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if below {
			writeError(w, http.StatusUnprocessableEntity,
				"verified_reading is below the shift's opening reading")
			return
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	verified := req.VerifiedReading.String()
	// A correction may also CLEAR a non-terminal hold (rejected/flagged) on the
	// same active reading — that is how a supervisor resolves a flag (or a
	// rejection they decide to fix themselves rather than wait for re-capture):
	// the hold verification is overwritten with the corrected verdict. A
	// terminal verification ({approved, corrected}) stays immutable → 409.
	v, err := s.upsertVerificationClearingHold(ctx, tx, actor.TenantID, readings.VerificationInput{
		StationID: shift.StationID, ShiftID: shift.ID, NozzleID: reading.NozzleID,
		ReadingID:                 reading.ID,
		AttendantSubmittedReading: reading.Reading,
		SupervisorVerifiedReading: &verified,
		FinalApprovedReading:      verified,
		Status:                    "corrected",
		Reason:                    &reason,
		VerifiedBy:                actor.UserID,
	})
	if errors.Is(err, readings.ErrTerminalVerification) {
		writeError(w, http.StatusConflict, "reading is already verified")
		return
	}
	if err != nil {
		s.logger.Error("verify-correct reading", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// recorded_by (the attendant who submitted the reading) rides the payload
	// additively so the notification subscriber can target the recorder's feed
	// (Mobile Attendant Phase 7) without re-reading the meter row.
	auditNew := map[string]any{
		"verification": toReadingVerificationDTO(v),
		"recorded_by":  reading.RecordedBy,
	}
	// A closed shift has a frozen close line for the nozzle; rewrite it from
	// the corrected closing so expected collection follows the approved
	// figure. The pre-correction line stays derivable: opening is unchanged
	// and the original closing is the verification's submitted snapshot.
	if shift.Status == "closed" {
		line, err := s.operations.RecomputeCloseLineClosing(ctx, tx, actor.TenantID, shift.ID, reading.NozzleID, verified)
		if err != nil && !errors.Is(err, operations.ErrCloseLineNotFound) {
			s.logger.Error("verify-correct reading: recompute line", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if line != nil {
			// Reject a correction that would drive litres negative (closing
			// below the frozen opening). The sign check is on the exact
			// decimal string — no float math.
			if strings.HasPrefix(line.LitresSold, "-") {
				writeError(w, http.StatusUnprocessableEntity,
					"verified_reading is below the shift's opening reading")
				return
			}
			auditNew["recomputed_close_line"] = toCloseLineDTO(line)
		}
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "reading_verification.corrected", EventType: "ReadingVerificationCorrected",
		EntityType: "reading_verification", EntityID: v.ID.String(),
		PreviousValue: toMeterReadingDTO(reading), NewValue: auditNew,
		IP: clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("verify-correct reading: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toReadingVerificationDTO(v))
}

type readingVerdictRequest struct {
	Reason string `json:"reason"`
}

// loadClosingForVerdict resolves the shift (authorizing reading.override at its
// station) and the target reading for a hold verdict (reject/flag), running the
// shared validations: the reading must belong to the shift, be an ACTIVE
// closing, and the actor must not be its recorder (separation of duties). It
// writes the error response and returns ok=false on any failure. A hold (or a
// per-reading approval) may be recorded while the shift is open or closed — it
// is the approval that the gate blocks, not the verdict — but NOT once the shift
// is approved: its sales/revenue are posted from the frozen lines, so a verdict
// then would desync approved facts (mirrors the verify-correct approved guard).
func (s *Server) loadClosingForVerdict(w http.ResponseWriter, r *http.Request, actor identity.Actor, verb string) (*operations.Shift, *readings.MeterReading, bool) {
	readingID, err := uuid.Parse(chi.URLParam(r, "readingID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reading id")
		return nil, nil, false
	}
	shift, ok := s.shiftForVerification(w, r, actor)
	if !ok {
		return nil, nil, false
	}
	if shift.Status == "approved" {
		writeError(w, http.StatusConflict, "shift is already approved")
		return nil, nil, false
	}
	ctx := r.Context()
	reading, err := s.readings.Get(ctx, actor.TenantID, readingID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "reading not found")
		return nil, nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, nil, false
	}
	if reading.ShiftID != shift.ID {
		writeError(w, http.StatusBadRequest, "reading does not belong to this shift")
		return nil, nil, false
	}
	if reading.ReadingType != "closing" {
		writeError(w, http.StatusUnprocessableEntity, "only closing readings are verified")
		return nil, nil, false
	}
	if reading.Status != "active" {
		writeError(w, http.StatusConflict, "reading is superseded; "+verb+" its active correction instead")
		return nil, nil, false
	}
	// Separation of duties: the verifier must not be the recorder.
	if reading.RecordedBy == actor.UserID {
		writeError(w, http.StatusForbidden,
			"separation of duties: you cannot "+verb+" a reading you recorded")
		return nil, nil, false
	}
	return shift, reading, true
}

// upsertVerificationClearingHold inserts a verification for a reading, or — when
// the reading already carries a NON-TERMINAL hold (rejected/flagged) —
// overwrites that hold row in place with the new verdict. A reading with a
// TERMINAL verification ({approved, corrected}) is immutable: the call returns
// readings.ErrTerminalVerification and changes nothing. This is the single
// place that encodes "a hold can be re-decided, a final verdict cannot" so
// every verdict path (approve-single, correct, reject, flag) shares it.
func (s *Server) upsertVerificationClearingHold(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in readings.VerificationInput) (*readings.Verification, error) {
	// Try the insert inside a SAVEPOINT (pgx nested Begin) so a unique-violation
	// failure rolls back just the attempt — without it the violation aborts the
	// whole outer tx (SQLSTATE 25P02) and the follow-up UPDATE can't run.
	sp, err := tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.readings.InsertVerification(ctx, sp, tenantID, in)
	if err == nil {
		if cerr := sp.Commit(ctx); cerr != nil {
			return nil, cerr
		}
		return v, nil
	}
	_ = sp.Rollback(ctx)
	if !isUniqueViolation(err) {
		return nil, err
	}
	// A verification already exists for this reading. Overwrite it only if it is
	// a hold; ReplaceHoldVerification returns ErrTerminalVerification otherwise.
	return s.readings.ReplaceHoldVerification(ctx, tx, tenantID, in)
}

// recordReadingVerdict writes one hold verification (rejected/flagged) and the
// caller's prepared audit/outbox record in a single tx, then returns the created
// row. final = the attendant's submission (the figure is unchanged — a hold
// neither approves nor overrides it; downstream money math is gated off until
// the hold clears). The caller builds `rec` with the literal Action + EventType
// for its verdict (so the producer-guard test sees a real `EventType: "X"`
// emitter); recorded_by rides the outbox payload additively so the notification
// subscriber can target the attendant who submitted the reading.
func (s *Server) recordReadingVerdict(
	w http.ResponseWriter, r *http.Request, actor identity.Actor,
	shift *operations.Shift, reading *readings.MeterReading,
	status, reason string, rec audit.TxRecord,
) {
	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// A verdict may land on a reading that already carries a NON-TERMINAL hold:
	// re-deciding a flag as a rejection, re-flagging, etc. In that case the hold
	// row is overwritten in place (the audit/outbox trail keeps the history). A
	// reading whose hold was already cleared by a TERMINAL verdict
	// ({approved, corrected}) is immutable → 409.
	v, err := s.upsertVerificationClearingHold(ctx, tx, actor.TenantID, readings.VerificationInput{
		StationID: shift.StationID, ShiftID: shift.ID, NozzleID: reading.NozzleID,
		ReadingID:                 reading.ID,
		AttendantSubmittedReading: reading.Reading,
		FinalApprovedReading:      reading.Reading,
		Status:                    status,
		Reason:                    &reason,
		VerifiedBy:                actor.UserID,
	})
	if errors.Is(err, readings.ErrTerminalVerification) {
		writeError(w, http.StatusConflict, "reading is already verified")
		return
	}
	if err != nil {
		s.logger.Error("record reading verdict", "error", err, "status", status)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Fill in the fields that depend on the freshly-inserted verification row.
	// The caller supplied Action + EventType (as literals) and the common audit
	// metadata. recorded_by rides the outbox payload additively so the
	// notification subscriber can target the attendant's feed.
	rec.EntityID = v.ID.String()
	rec.PreviousValue = toMeterReadingDTO(reading)
	rec.NewValue = map[string]any{
		"verification": toReadingVerificationDTO(v),
		"recorded_by":  reading.RecordedBy,
	}
	if err := audit.WriteWithOutbox(ctx, tx, rec); err != nil {
		s.logger.Error("record reading verdict: audit", "error", err, "status", status)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toReadingVerificationDTO(v))
}

// handleRejectReading records a supervisor REJECTION of one closing reading
// (reading.override; PRD §7.8 "supervisors can approve, reject, or correct
// readings"). A rejection is a NON-terminal hold: the meter reading is never
// mutated, the verification snapshots the submission with status 'rejected' and
// the mandatory reason, and it (a) blocks shift approval (the gate reports
// readings_rejected_pending) and (b) unlocks the attendant's Phase 3
// closing-submission lock for that nozzle so they can re-capture. The
// re-captured closing supersedes the rejected one (the rejection stays on the
// superseded row as history) and is unverified again, so re-verification
// proceeds normally.
func (s *Server) handleRejectReading(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req readingVerdictRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required when rejecting a reading")
		return
	}
	shift, reading, ok := s.loadClosingForVerdict(w, r, actor, "reject")
	if !ok {
		return
	}
	s.recordReadingVerdict(w, r, actor, shift, reading, "rejected", reason, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "reading_verification.rejected", EventType: "ReadingVerificationRejected",
		EntityType: "reading_verification",
		IP:         clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(r.Context()),
	})
}

// handleFlagReading records a supervisor FLAG of one closing reading for
// investigation (reading.override; PRD §9.5). Like a rejection it is a hold
// that blocks approval (the gate reports readings_flagged_pending) and never
// mutates the reading, but it does NOT unlock attendant resubmission — a flag
// is the supervisor's own investigation, cleared by a terminal verdict
// (correcting or, after re-capture, re-approving the reading).
func (s *Server) handleFlagReading(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req readingVerdictRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required when flagging a reading")
		return
	}
	shift, reading, ok := s.loadClosingForVerdict(w, r, actor, "flag")
	if !ok {
		return
	}
	s.recordReadingVerdict(w, r, actor, shift, reading, "flagged", reason, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "reading_verification.flagged", EventType: "ReadingVerificationFlagged",
		EntityType: "reading_verification",
		IP:         clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(r.Context()),
	})
}

// handleApproveReading approves ONE active closing reading as-submitted
// (reading.override). It is the per-reading counterpart to the batch
// /readings/verify, and — crucially — the path that CLEARS a hold without a
// figure change: a supervisor who flags a reading for investigation and then
// finds the attendant's figure was right approves it here, overwriting the
// flagged verdict with a terminal 'approved'. final = the attendant's
// submission (unchanged). An already-terminal verification (approved/corrected)
// is immutable → 409. SoD: the verifier must not be the recorder.
func (s *Server) handleApproveReading(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	shift, reading, ok := s.loadClosingForVerdict(w, r, actor, "approve")
	if !ok {
		return
	}
	ctx := r.Context()

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	v, err := s.upsertVerificationClearingHold(ctx, tx, actor.TenantID, readings.VerificationInput{
		StationID: shift.StationID, ShiftID: shift.ID, NozzleID: reading.NozzleID,
		ReadingID:                 reading.ID,
		AttendantSubmittedReading: reading.Reading,
		FinalApprovedReading:      reading.Reading,
		Status:                    "approved",
		VerifiedBy:                actor.UserID,
	})
	if errors.Is(err, readings.ErrTerminalVerification) {
		writeError(w, http.StatusConflict, "reading is already verified")
		return
	}
	if err != nil {
		s.logger.Error("approve reading", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// recorded_by rides the payload additively so the notification subscriber can
	// target the recorder's feed ("your reading was approved", Phase 7).
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "reading_verification.approved", EventType: "ReadingVerificationApproved",
		EntityType: "reading_verification", EntityID: v.ID.String(),
		PreviousValue: toMeterReadingDTO(reading),
		NewValue: map[string]any{
			"verification": toReadingVerificationDTO(v),
			"recorded_by":  reading.RecordedBy,
		},
		IP: clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("approve reading: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toReadingVerificationDTO(v))
}
