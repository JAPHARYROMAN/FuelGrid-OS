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
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	ShiftID     uuid.UUID `json:"shift_id"`
	NozzleID    uuid.UUID `json:"nozzle_id"`
	ReadingType string    `json:"reading_type"`
	// Reading is an exact decimal STRING (numeric(14,3)).
	Reading      string     `json:"reading"`
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
// active opening and closing reading. Opening/closing/litres are exact decimal
// strings; litres_dispensed is computed in SQL numeric (OPS-001).
type dispensedDTO struct {
	NozzleID        uuid.UUID `json:"nozzle_id"`
	Opening         string    `json:"opening"`
	Closing         string    `json:"closing"`
	LitresDispensed string    `json:"litres_dispensed"`
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

	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	rows, err := s.readings.ListActiveForShiftPage(ctx, actor.TenantID, id, limit+1, offset)
	if err != nil {
		s.logger.Error("list meter readings", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]meterReadingDTO, 0, len(rows))
	for i := range rows {
		items = append(items, toMeterReadingDTO(&rows[i]))
	}

	// Litres dispensed (closing - opening) is computed in SQL numeric per
	// nozzle, never in Go float (OPS-001). Rolled-back meters are excluded by
	// the query, so a bad pair simply doesn't appear.
	disp, err := s.readings.DispensedForShift(ctx, actor.TenantID, id)
	if err != nil {
		s.logger.Error("dispensed for shift", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	dispensed := make([]dispensedDTO, 0, len(disp))
	for i := range disp {
		dispensed = append(dispensed, dispensedDTO{
			NozzleID: disp[i].NozzleID, Opening: disp[i].Opening,
			Closing: disp[i].Closing, LitresDispensed: disp[i].LitresDispensed,
		})
	}

	// Standard paged envelope plus the dispensed companion this endpoint has
	// always returned alongside the readings page.
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "count": len(items),
		"limit": limit, "offset": offset, "has_more": hasMore,
		"dispensed": dispensed,
	})
}

type captureMeterReadingRequest struct {
	NozzleID    uuid.UUID    `json:"nozzle_id"`
	ReadingType string       `json:"reading_type"`
	Reading     decimalInput `json:"reading"`
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
	if !req.Reading.Valid() {
		writeError(w, http.StatusBadRequest, "reading must be a non-negative decimal")
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
	// MD: the reading is stored as an exact decimal string. ValidateScale is a
	// precision check against the nozzle's meter decimal places; it parses the
	// decimal for that comparison only (the stored value stays the string).
	if err := readings.ValidateScale(dispDecimal(req.Reading.String()), nozzle.MeterDecimalPlaces); err != nil {
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
		Reading: req.Reading.String(), RecordedBy: actor.UserID,
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
	Reading decimalInput `json:"reading"`
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
	if !req.Reading.Valid() {
		writeError(w, http.StatusBadRequest, "reading must be a non-negative decimal")
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
	if err := readings.ValidateScale(dispDecimal(req.Reading.String()), nozzle.MeterDecimalPlaces); err != nil {
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
		Reading: req.Reading.String(), RecordedBy: actor.UserID, SupersedesID: &old.ID,
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
