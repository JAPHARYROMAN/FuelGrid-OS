package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/calibration"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/readings"
)

type dipReadingDTO struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	ShiftID     uuid.UUID `json:"shift_id"`
	TankID      uuid.UUID `json:"tank_id"`
	ReadingType string    `json:"reading_type"`
	// DipMM and VolumeLitres are exact decimal STRINGS (both numeric(14,3)).
	DipMM        string     `json:"dip_mm"`
	VolumeLitres string     `json:"volume_litres"`
	WaterMM      *float64   `json:"water_mm,omitempty"`
	TemperatureC *float64   `json:"temperature_c,omitempty"`
	ChartID      uuid.UUID  `json:"chart_id"`
	RecordedBy   uuid.UUID  `json:"recorded_by"`
	RecordedAt   string     `json:"recorded_at"`
	SupersedesID *uuid.UUID `json:"supersedes_id,omitempty"`
	Status       string     `json:"status"`
}

func toDipReadingDTO(d *readings.DipReading) dipReadingDTO {
	return dipReadingDTO{
		ID: d.ID, TenantID: d.TenantID, ShiftID: d.ShiftID, TankID: d.TankID,
		ReadingType: d.ReadingType, DipMM: d.DipMM, VolumeLitres: d.VolumeLitres,
		WaterMM: d.WaterMM, TemperatureC: d.TemperatureC, ChartID: d.ChartID,
		RecordedBy: d.RecordedBy, RecordedAt: d.RecordedAt.Format(time.RFC3339),
		SupersedesID: d.SupersedesID, Status: d.Status,
	}
}

func (s *Server) handleListDipReadings(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.readings.ListDipsForShift(ctx, actor.TenantID, id)
	if err != nil {
		s.logger.Error("list dip readings", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]dipReadingDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toDipReadingDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

type captureDipRequest struct {
	TankID       uuid.UUID    `json:"tank_id"`
	ReadingType  string       `json:"reading_type"`
	DipMM        decimalInput `json:"dip_mm"`
	WaterMM      *float64     `json:"water_mm,omitempty"`
	TemperatureC *float64     `json:"temperature_c,omitempty"`
}

// resolveDipVolume looks the dip up against the tank's active chart, mapping
// the calibration errors to HTTP responses. Returns ok=false after writing.
//
// MD boundary: the calibration chart (and its linear interpolation) is float
// math owned by the calibration domain in a later wave. dipMM is parsed from
// its exact-decimal string for the lookup, and the resolved volume is formatted
// back to the dip table's numeric(14,3) text form so the stored figure stays an
// exact decimal string.
func (s *Server) resolveDipVolume(w http.ResponseWriter, r *http.Request, actor identity.Actor, tankID uuid.UUID, dipMM string) (volume string, chartID uuid.UUID, ok bool) {
	vol, chartID, err := s.calibration.Lookup(r.Context(), actor.TenantID, tankID, dispDecimal(dipMM))
	switch {
	case errors.Is(err, calibration.ErrNoActiveChart):
		writeError(w, http.StatusConflict, "tank has no active calibration chart")
		return "", uuid.Nil, false
	case errors.Is(err, calibration.ErrOutOfRange):
		writeError(w, http.StatusUnprocessableEntity, "dip is outside the chart's range")
		return "", uuid.Nil, false
	case errors.Is(err, calibration.ErrEmptyChart):
		writeError(w, http.StatusUnprocessableEntity, "the active chart has no entries")
		return "", uuid.Nil, false
	case err != nil:
		s.logger.Error("dip lookup", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return "", uuid.Nil, false
	}
	return strconv.FormatFloat(vol, 'f', 3, 64), chartID, true
}

func (s *Server) handleCaptureDipReading(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req captureDipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.TankID == uuid.Nil || (req.ReadingType != "opening" && req.ReadingType != "closing") {
		writeError(w, http.StatusBadRequest, "tank_id and reading_type (opening|closing) are required")
		return
	}
	if !req.DipMM.Valid() {
		writeError(w, http.StatusBadRequest, "dip_mm must be a non-negative decimal")
		return
	}

	// Self-scoped like meter capture: attendants (reading.edit) may only dip
	// tanks behind their own assigned nozzles; supervisors (reading.override)
	// may dip any tank with an assigned nozzle on the shift.
	shift, override, ok := s.shiftForScopedWrite(w, r, actor, "reading.edit", "reading.override", true)
	if !ok {
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
	if tank.StationID != shift.StationID {
		writeError(w, http.StatusBadRequest, "tank is at a different station than the shift")
		return
	}
	if !s.requireTankAssigned(w, ctx, actor, shift.ID, req.TankID, override) {
		return
	}

	volume, chartID, ok := s.resolveDipVolume(w, r, actor, req.TankID, req.DipMM.String())
	if !ok {
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	dip, err := s.readings.CaptureDip(ctx, tx, actor.TenantID, readings.CaptureDipInput{
		ShiftID: shift.ID, TankID: req.TankID, ReadingType: req.ReadingType,
		DipMM: req.DipMM.String(), VolumeLitres: volume, WaterMM: req.WaterMM,
		TemperatureC: req.TemperatureC, ChartID: chartID, RecordedBy: actor.UserID,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "a "+req.ReadingType+" dip already exists for this tank; correct it instead")
		return
	}
	if err != nil {
		s.logger.Error("capture dip reading", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "dip_reading.captured", EventType: "DipReadingCaptured",
		EntityType: "tank_dip_reading", EntityID: dip.ID.String(),
		NewValue: toDipReadingDTO(dip),
		IP:       clientIP(r), UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("capture dip reading: audit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toDipReadingDTO(dip))
}

type correctDipRequest struct {
	DipMM        decimalInput `json:"dip_mm"`
	WaterMM      *float64     `json:"water_mm,omitempty"`
	TemperatureC *float64     `json:"temperature_c,omitempty"`
}

func (s *Server) handleCorrectDipReading(w http.ResponseWriter, r *http.Request) {
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
	var req correctDipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !req.DipMM.Valid() {
		writeError(w, http.StatusBadRequest, "dip_mm must be a non-negative decimal")
		return
	}

	// Corrections only while open (close freezes the snapshot — audit P1).
	shift, override, ok := s.shiftForScopedWrite(w, r, actor, "reading.edit", "reading.override", true)
	if !ok {
		return
	}

	ctx := r.Context()
	old, err := s.readings.GetDip(ctx, actor.TenantID, readingID)
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
	if !s.requireTankAssigned(w, ctx, actor, shift.ID, old.TankID, override) {
		return
	}

	volume, chartID, ok := s.resolveDipVolume(w, r, actor, old.TankID, req.DipMM.String())
	if !ok {
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.readings.SupersedeDip(ctx, tx, actor.TenantID, old.ID); err != nil {
		if errors.Is(err, readings.ErrDipNotFound) {
			writeError(w, http.StatusConflict, "reading is already superseded")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	fresh, err := s.readings.CaptureDip(ctx, tx, actor.TenantID, readings.CaptureDipInput{
		ShiftID: shift.ID, TankID: old.TankID, ReadingType: old.ReadingType,
		DipMM: req.DipMM.String(), VolumeLitres: volume, WaterMM: req.WaterMM,
		TemperatureC: req.TemperatureC, ChartID: chartID, RecordedBy: actor.UserID,
		SupersedesID: &old.ID,
	})
	if err != nil {
		s.logger.Error("correct dip reading", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "dip_reading.corrected", EventType: "DipReadingCorrected",
		EntityType: "tank_dip_reading", EntityID: fresh.ID.String(),
		PreviousValue: toDipReadingDTO(old), NewValue: toDipReadingDTO(fresh),
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
	writeJSON(w, http.StatusOK, toDipReadingDTO(fresh))
}
