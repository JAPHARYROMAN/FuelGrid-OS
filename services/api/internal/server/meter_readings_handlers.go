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
	"github.com/japharyroman/fuelgrid-os/internal/readings"
)

type meterReadingDTO struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	ShiftID      uuid.UUID  `json:"shift_id"`
	NozzleID     uuid.UUID  `json:"nozzle_id"`
	ReadingType  string     `json:"reading_type"`
	Reading      float64    `json:"reading"`
	RecordedBy   uuid.UUID  `json:"recorded_by"`
	RecordedAt   string     `json:"recorded_at"`
	SupersedesID *uuid.UUID `json:"supersedes_id,omitempty"`
	Status       string     `json:"status"`
}

func toMeterReadingDTO(m *readings.MeterReading) meterReadingDTO {
	return meterReadingDTO{
		ID: m.ID, TenantID: m.TenantID, ShiftID: m.ShiftID, NozzleID: m.NozzleID,
		ReadingType: m.ReadingType, Reading: m.Reading,
		RecordedBy: m.RecordedBy, RecordedAt: m.RecordedAt.Format(time.RFC3339),
		SupersedesID: m.SupersedesID, Status: m.Status,
	}
}

// dispensedDTO is the per-nozzle litres figure for nozzles that have both an
// active opening and closing reading.
type dispensedDTO struct {
	NozzleID        uuid.UUID `json:"nozzle_id"`
	Opening         float64   `json:"opening"`
	Closing         float64   `json:"closing"`
	LitresDispensed float64   `json:"litres_dispensed"`
}

func (s *Server) handleListMeterReadings(w http.ResponseWriter, r *http.Request) {
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

	rows, err := s.readings.ListActiveForShift(ctx, actor.TenantID, id)
	if err != nil {
		s.logger.Error("list meter readings", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	items := make([]meterReadingDTO, 0, len(rows))
	type pair struct{ opening, closing *float64 }
	byNozzle := map[uuid.UUID]*pair{}
	for i := range rows {
		items = append(items, toMeterReadingDTO(&rows[i]))
		p := byNozzle[rows[i].NozzleID]
		if p == nil {
			p = &pair{}
			byNozzle[rows[i].NozzleID] = p
		}
		v := rows[i].Reading
		if rows[i].ReadingType == "opening" {
			p.opening = &v
		} else {
			p.closing = &v
		}
	}

	dispensed := make([]dispensedDTO, 0, len(byNozzle))
	for nozzleID, p := range byNozzle {
		if p.opening == nil || p.closing == nil {
			continue
		}
		litres, err := readings.LitresDispensed(*p.opening, *p.closing)
		if err != nil {
			// A rolled-back meter shouldn't 500 the whole list; surface the
			// pair without a computed figure by skipping it.
			continue
		}
		dispensed = append(dispensed, dispensedDTO{
			NozzleID: nozzleID, Opening: *p.opening, Closing: *p.closing, LitresDispensed: litres,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "count": len(items), "dispensed": dispensed,
	})
}

type captureMeterReadingRequest struct {
	NozzleID    uuid.UUID `json:"nozzle_id"`
	ReadingType string    `json:"reading_type"`
	Reading     float64   `json:"reading"`
}

func (s *Server) handleCaptureMeterReading(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req captureMeterReadingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.NozzleID == uuid.Nil || (req.ReadingType != "opening" && req.ReadingType != "closing") {
		writeError(w, http.StatusBadRequest, "nozzle_id and reading_type (opening|closing) are required")
		return
	}
	if req.Reading < 0 {
		writeError(w, http.StatusBadRequest, "reading must be non-negative")
		return
	}

	// Capture only during an open shift. Attendants (reading.edit) are
	// self-scoped to their own assigned nozzles; supervisors (reading.override)
	// may write any nozzle assigned on the shift.
	shift, override, ok := s.shiftForScopedWrite(w, r, actor, "reading.edit", "reading.override", true)
	if !ok {
		return
	}

	ctx := r.Context()
	nozzle, err := s.nozzles.Get(ctx, actor.TenantID, req.NozzleID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "nozzle not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if nozzle.StationID != shift.StationID {
		writeError(w, http.StatusBadRequest, "nozzle is at a different station than the shift")
		return
	}
	if !s.requireNozzleAssigned(w, ctx, actor, shift.ID, req.NozzleID, override) {
		return
	}
	if err := readings.ValidateScale(req.Reading, nozzle.MeterDecimalPlaces); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "reading has more decimals than the nozzle's meter precision")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	reading, err := s.readings.Capture(ctx, tx, actor.TenantID, readings.CaptureInput{
		ShiftID: shift.ID, NozzleID: req.NozzleID, ReadingType: req.ReadingType,
		Reading: req.Reading, RecordedBy: actor.UserID,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a "+req.ReadingType+" reading already exists for this nozzle; correct it instead")
		return
	}
	if err != nil {
		s.logger.Error("capture meter reading", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "meter_reading.captured", EventType: "MeterReadingCaptured",
		EntityType: "meter_reading", EntityID: reading.ID.String(),
		NewValue: toMeterReadingDTO(reading),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("capture meter reading: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toMeterReadingDTO(reading))
}

type correctMeterReadingRequest struct {
	Reading float64 `json:"reading"`
}

func (s *Server) handleCorrectMeterReading(w http.ResponseWriter, r *http.Request) {
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
	var req correctMeterReadingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Reading < 0 {
		writeError(w, http.StatusBadRequest, "reading must be non-negative")
		return
	}

	// Corrections only while the shift is open. Once closed, shift_close_lines
	// and expected cash are frozen, so a correction would desync approved
	// facts (audit P1) — block it. Self-scoped like capture.
	shift, override, ok := s.shiftForScopedWrite(w, r, actor, "reading.edit", "reading.override", true)
	if !ok {
		return
	}

	ctx := r.Context()
	old, err := s.readings.Get(ctx, actor.TenantID, readingID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "reading not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if old.ShiftID != shift.ID {
		writeError(w, http.StatusBadRequest, "reading does not belong to this shift")
		return
	}
	if old.Status != "active" {
		writeError(w, http.StatusConflict, "reading is already superseded")
		return
	}
	if !s.requireNozzleAssigned(w, ctx, actor, shift.ID, old.NozzleID, override) {
		return
	}

	nozzle, err := s.nozzles.Get(ctx, actor.TenantID, old.NozzleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := readings.ValidateScale(req.Reading, nozzle.MeterDecimalPlaces); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "reading has more decimals than the nozzle's meter precision")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.readings.Supersede(ctx, tx, actor.TenantID, old.ID); err != nil {
		if errors.Is(err, readings.ErrNotFound) {
			writeError(w, http.StatusConflict, "reading is already superseded")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	fresh, err := s.readings.Capture(ctx, tx, actor.TenantID, readings.CaptureInput{
		ShiftID: shift.ID, NozzleID: old.NozzleID, ReadingType: old.ReadingType,
		Reading: req.Reading, RecordedBy: actor.UserID, SupersedesID: &old.ID,
	})
	if err != nil {
		s.logger.Error("correct meter reading", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "meter_reading.corrected", EventType: "MeterReadingCorrected",
		EntityType: "meter_reading", EntityID: fresh.ID.String(),
		PreviousValue: toMeterReadingDTO(old), NewValue: toMeterReadingDTO(fresh),
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
	writeJSON(w, http.StatusOK, toMeterReadingDTO(fresh))
}
