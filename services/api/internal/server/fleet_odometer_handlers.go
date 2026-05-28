package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// ---- Odometer (Stage 10) ----

func (s *Server) handleRecordOdometer(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	vehicleID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid vehicle id")
		return
	}
	var req struct {
		Reading         string     `json:"reading"`
		AuthorizationID *uuid.UUID `json:"authorization_id,omitempty"`
		StationID       *uuid.UUID `json:"station_id,omitempty"`
		Note            *string    `json:"note,omitempty"`
		Override        bool       `json:"override,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if v, ok := parseDecimal(req.Reading); !ok || v < 0 {
		writeError(w, http.StatusBadRequest, "reading must be a non-negative decimal")
		return
	}
	reading, err := s.fleet.RecordOdometer(r.Context(), actor.TenantID, vehicleID, req.AuthorizationID, req.StationID, req.Reading, req.Note, req.Override, actor.UserID)
	if err != nil {
		if isForeignKeyViolation(err) {
			writeError(w, http.StatusBadRequest, "unknown vehicle")
			return
		}
		s.logger.Error("record odometer", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Audit (best-effort, outside the read path's tx).
	if tx, txErr := s.deps.DB.Begin(r.Context()); txErr == nil {
		_ = audit.WriteWithOutbox(r.Context(), tx, audit.TxRecord{
			TenantID: actor.TenantID, ActorID: actor.UserID,
			Action: "vehicle_odometer.recorded", EventType: "VehicleOdometerRecorded", EntityType: "vehicle_odometer_reading",
			EntityID: reading.ID.String(), NewValue: map[string]any{"reading": reading.Reading, "validation_status": reading.ValidationStatus},
			IP: clientIP(r), UserAgent: r.UserAgent(),
		})
		_ = tx.Commit(r.Context())
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": reading.ID, "vehicle_id": reading.VehicleID, "reading": reading.Reading,
		"distance_since": reading.DistanceSince, "validation_status": reading.ValidationStatus,
		"captured_at": reading.CapturedAt.Format(time.RFC3339),
	})
}

func (s *Server) handleListOdometer(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	vehicleID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid vehicle id")
		return
	}
	rows, err := s.fleet.ListOdometerReadings(r.Context(), actor.TenantID, vehicleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		o := rows[i]
		out = append(out, map[string]any{
			"id": o.ID, "reading": o.Reading, "distance_since": o.DistanceSince,
			"validation_status": o.ValidationStatus, "note": o.Note, "captured_at": o.CapturedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// ---- Fleet consumption (Stage 11) ----

func (s *Server) handleFleetConsumption(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	customerID := queryUUID(r, "customer_id")
	if customerID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "customer_id is required")
		return
	}
	from := parseDateParam(r, "from", time.Now().AddDate(0, -1, 0))
	to := parseDateParam(r, "to", time.Now())
	rows, err := s.fleet.FleetConsumption(r.Context(), actor.TenantID, customerID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		c := rows[i]
		out = append(out, map[string]any{
			"vehicle_id": c.VehicleID, "registration": c.Registration, "fuelings": c.Fuelings,
			"amount_total": c.AmountTotal, "odometer_start": c.OdometerStart, "odometer_end": c.OdometerEnd, "distance": c.Distance,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from.Format(dateLayout), "to": to.Format(dateLayout), "items": out, "count": len(out),
	})
}
