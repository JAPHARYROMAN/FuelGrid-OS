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
// Phase 0): every ACTIVE closing reading of the shift must carry a
// verification row. Returns true when the gate passes; otherwise writes a 409
// with the machine-readable code "readings_unverified" (plus the count) so
// clients can branch to the verification flow. Runs through any Querier so
// the approval handler can re-check inside its FOR UPDATE tx.
func (s *Server) requireClosingReadingsVerified(w http.ResponseWriter, ctx context.Context, q database.Querier, tenantID, shiftID uuid.UUID) bool {
	n, err := s.readings.UnverifiedClosingCountForShift(ctx, q, tenantID, shiftID)
	if err != nil {
		s.logger.Error("approval verification gate", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	if n > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":            "verify the shift's closing readings before approving",
			"code":             "readings_unverified",
			"status":           http.StatusConflict,
			"unverified_count": n,
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
			if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
				TenantID: actor.TenantID, ActorID: actor.UserID,
				Action: "reading_verification.approved", EventType: "ReadingVerificationApproved",
				EntityType: "reading_verification", EntityID: v.ID.String(),
				NewValue: toReadingVerificationDTO(v),
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

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	verified := req.VerifiedReading.String()
	v, err := s.readings.InsertVerification(ctx, tx, actor.TenantID, readings.VerificationInput{
		StationID: shift.StationID, ShiftID: shift.ID, NozzleID: reading.NozzleID,
		ReadingID:                 reading.ID,
		AttendantSubmittedReading: reading.Reading,
		SupervisorVerifiedReading: &verified,
		FinalApprovedReading:      verified,
		Status:                    "corrected",
		Reason:                    &reason,
		VerifiedBy:                actor.UserID,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "reading is already verified")
		return
	}
	if err != nil {
		s.logger.Error("verify-correct reading", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	auditNew := map[string]any{"verification": toReadingVerificationDTO(v)}
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
