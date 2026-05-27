package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// stationDTO is the response shape for GET /api/v1/stations/{id}. Lean on
// purpose — full station detail will arrive when Stage 6 fleshes out the
// stations module.
type stationDTO struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Code    string    `json:"code"`
	Status  string    `json:"status"`
	City    *string   `json:"city,omitempty"`
	Country *string   `json:"country,omitempty"`
}

func (s *Server) handleGetStation(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}

	var dto stationDTO
	err = s.deps.DB.QueryRow(r.Context(), `
		SELECT id, name, code, status, city, country
		FROM stations
		WHERE id = $1 AND tenant_id = $2 AND status <> 'deleted'
	`, id, actor.TenantID).Scan(
		&dto.ID, &dto.Name, &dto.Code, &dto.Status, &dto.City, &dto.Country,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		s.logger.Error("get station", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, dto)
}
