package server

import (
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
	"github.com/japharyroman/fuelgrid-os/internal/tanks"
)

// chartCapacityTolerance allows a chart's top volume to sit slightly above
// the tank's nominal capacity (rounding, ullage models) before it's rejected.
const chartCapacityTolerance = 1.01

// maxCalibrationUpload caps the in-memory multipart parse. A strapping chart
// is a few KB; 4 MB is generous headroom.
const maxCalibrationUpload = 4 << 20

type calibrationChartDTO struct {
	ID             uuid.UUID `json:"id"`
	TenantID       uuid.UUID `json:"tenant_id"`
	TankID         uuid.UUID `json:"tank_id"`
	Name           string    `json:"name"`
	EffectiveFrom  string    `json:"effective_from"`
	EffectiveUntil *string   `json:"effective_until,omitempty"`
	Status         string    `json:"status"`
	Source         string    `json:"source"`
	EntryCount     int       `json:"entry_count"`
}

func fmtTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

func toCalibrationChartDTO(c *calibration.Chart) calibrationChartDTO {
	return calibrationChartDTO{
		ID: c.ID, TenantID: c.TenantID, TankID: c.TankID, Name: c.Name,
		EffectiveFrom:  c.EffectiveFrom.Format(time.RFC3339),
		EffectiveUntil: fmtTime(c.EffectiveUntil),
		Status:         c.Status, Source: c.Source, EntryCount: c.EntryCount,
	}
}

// tankForCalibration loads the tank named by the {id} URL param, scoped to
// the actor's tenant. It writes the error response and returns false on any
// failure so callers can simply return.
func (s *Server) tankForCalibration(w http.ResponseWriter, r *http.Request, actor identity.Actor) (*tanks.Tank, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid tank id")
		return nil, false
	}
	tank, err := s.tanks.Get(r.Context(), actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "tank not found")
		return nil, false
	}
	if err != nil {
		s.logger.Error("calibration: load tank", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	return tank, true
}

func (s *Server) handleListCalibrationCharts(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tank, ok := s.tankForCalibration(w, r, actor)
	if !ok {
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", tank.StationID) {
		return
	}
	tankID := tank.ID
	rows, err := s.calibration.ListCharts(r.Context(), actor.TenantID, tankID)
	if err != nil {
		s.logger.Error("list calibration charts", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]calibrationChartDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toCalibrationChartDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleGetActiveCalibrationChart(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tank, ok := s.tankForCalibration(w, r, actor)
	if !ok {
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", tank.StationID) {
		return
	}
	tankID := tank.ID
	chart, err := s.calibration.ActiveChart(r.Context(), actor.TenantID, tankID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no active calibration chart")
		return
	}
	if err != nil {
		s.logger.Error("active calibration chart", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toCalibrationChartDTO(chart))
}

func (s *Server) handleCalibratedVolume(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	raw := r.URL.Query().Get("dip_mm")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "dip_mm query parameter is required")
		return
	}
	dip, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "dip_mm must be a number")
		return
	}
	tank, ok := s.tankForCalibration(w, r, actor)
	if !ok {
		return
	}
	if !s.authorizeStation(w, r, actor, "station.read", tank.StationID) {
		return
	}
	tankID := tank.ID

	volume, chartID, err := s.calibration.Lookup(r.Context(), actor.TenantID, tankID, dip)
	switch {
	case errors.Is(err, calibration.ErrNoActiveChart):
		writeError(w, http.StatusNotFound, "no active calibration chart for this tank")
		return
	case errors.Is(err, calibration.ErrOutOfRange):
		writeError(w, http.StatusUnprocessableEntity, "dip is outside the chart's range")
		return
	case errors.Is(err, calibration.ErrEmptyChart):
		writeError(w, http.StatusUnprocessableEntity, "the active chart has no entries")
		return
	case err != nil:
		s.logger.Error("calibrated volume", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id":       tankID,
		"chart_id":      chartID,
		"dip_mm":        dip,
		"volume_litres": volume,
	})
}

func (s *Server) handleUploadCalibrationChart(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tank, ok := s.tankForCalibration(w, r, actor)
	if !ok {
		return
	}
	tankID := tank.ID

	// Station-scoped authorization (the tank's station).
	if !s.authorizeStation(w, r, actor, "tanks.calibrate", tank.StationID) {
		return
	}

	// Bound the whole request body, not just the in-memory portion, so a
	// huge upload can't exhaust memory.
	r.Body = http.MaxBytesReader(w, r.Body, maxCalibrationUpload)
	if err := r.ParseMultipartForm(maxCalibrationUpload); err != nil { //nolint:gosec // G120: body bounded by MaxBytesReader above
		writeError(w, http.StatusBadRequest, "invalid or oversized multipart form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "a CSV file field is required")
		return
	}
	defer func() { _ = file.Close() }()

	name := r.FormValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	source := r.FormValue("source")
	if source == "" {
		source = "csv_upload"
	}
	effectiveFrom := time.Now()
	if v := r.FormValue("effective_from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "effective_from must be RFC3339")
			return
		}
		effectiveFrom = t
	}

	entries, err := calibration.ParseCSV(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// A chart must not map the tank above its physical capacity (a small
	// tolerance absorbs rounding / ullage). entries is sorted ascending, so
	// the last row carries the max volume.
	maxVolume := entries[len(entries)-1].VolumeLitres
	if maxVolume > tank.CapacityLitres*chartCapacityTolerance {
		writeError(w, http.StatusUnprocessableEntity, "chart maximum volume exceeds the tank's capacity")
		return
	}

	// Dry run: validate and summarise without persisting.
	if r.URL.Query().Get("dry_run") == "true" || r.FormValue("dry_run") == "true" {
		writeJSON(w, http.StatusOK, map[string]any{
			"preview":     true,
			"entry_count": len(entries),
			"min_dip_mm":  entries[0].DipMM,
			"max_dip_mm":  entries[len(entries)-1].DipMM,
			"min_volume":  entries[0].VolumeLitres,
			"max_volume":  entries[len(entries)-1].VolumeLitres,
		})
		return
	}

	ctx := r.Context()
	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Supersede the current active chart (if any) before inserting the new
	// one — the partial unique index allows only one active per tank.
	supersededID, err := s.calibration.SupersedeActive(ctx, tx, actor.TenantID, tankID)
	if err != nil {
		s.logger.Error("supersede chart", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	chart, err := s.calibration.CreateChart(ctx, tx, actor.TenantID, tankID, name, source, effectiveFrom, entries)
	if err != nil {
		s.logger.Error("create calibration chart", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	reqID := chimiddleware.GetReqID(ctx)
	if supersededID != nil {
		if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "tank_calibration.chart_superseded", EventType: "TankCalibrationChartSuperseded",
			EntityType: "tank_calibration_chart", EntityID: supersededID.String(),
			NewValue: map[string]any{"tank_id": tankID, "superseded_by": chart.ID},
			IP:       clientIP(r), UserAgent: r.UserAgent(), RequestID: reqID,
		}); err != nil {
			s.logger.Error("audit chart superseded", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "tank_calibration.chart_uploaded", EventType: "TankCalibrationChartUploaded",
		EntityType: "tank_calibration_chart", EntityID: chart.ID.String(),
		NewValue: toCalibrationChartDTO(chart),
		IP:       clientIP(r), UserAgent: r.UserAgent(), RequestID: reqID,
	}); err != nil {
		s.logger.Error("audit chart uploaded", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toCalibrationChartDTO(chart))
}
