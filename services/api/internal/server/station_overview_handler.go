package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// pumpWithNozzlesDTO embeds a pump and inlines its nozzles, so the dashboard
// gets the full dispensing tree without an extra round-trip per pump.
type pumpWithNozzlesDTO struct {
	pumpDTO
	Nozzles []nozzleDTO `json:"nozzles"`
}

type stationOverviewDTO struct {
	Station       stationDTO           `json:"station"`
	Tanks         []tankDTO            `json:"tanks"`
	Pumps         []pumpWithNozzlesDTO `json:"pumps"`
	OpenIncidents []incidentDTO        `json:"open_incidents"`
}

// handleStationOverview returns a station plus its tanks, pumps (each with
// nested nozzles), and active incidents in one response — the single call
// the station dashboard makes, avoiding N+1 from the frontend. Gated by
// station.read for the specific station, so a forbidden station 403s via
// the policy middleware before this handler runs.
func (s *Server) handleStationOverview(w http.ResponseWriter, r *http.Request) {
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

	ctx := r.Context()
	station, err := s.stations.Get(ctx, actor.TenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		s.logger.Error("station overview: station", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tankRows, err := s.tanks.List(ctx, actor.TenantID, &id)
	if err != nil {
		s.logger.Error("station overview: tanks", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	pumpRows, err := s.pumps.List(ctx, actor.TenantID, &id)
	if err != nil {
		s.logger.Error("station overview: pumps", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	nozzleRows, err := s.nozzles.List(ctx, actor.TenantID, &id, nil)
	if err != nil {
		s.logger.Error("station overview: nozzles", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	incidentRows, err := s.incidents.ListActiveForStation(ctx, actor.TenantID, id)
	if err != nil {
		s.logger.Error("station overview: incidents", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	nozzlesByPump := make(map[uuid.UUID][]nozzleDTO, len(pumpRows))
	for i := range nozzleRows {
		dto := toNozzleDTO(&nozzleRows[i])
		nozzlesByPump[dto.PumpID] = append(nozzlesByPump[dto.PumpID], dto)
	}

	tanks := make([]tankDTO, 0, len(tankRows))
	for i := range tankRows {
		tanks = append(tanks, toTankDTO(&tankRows[i]))
	}

	pumps := make([]pumpWithNozzlesDTO, 0, len(pumpRows))
	for i := range pumpRows {
		p := toPumpDTO(&pumpRows[i])
		nozzles := nozzlesByPump[p.ID]
		if nozzles == nil {
			nozzles = []nozzleDTO{}
		}
		pumps = append(pumps, pumpWithNozzlesDTO{pumpDTO: p, Nozzles: nozzles})
	}

	incidents := make([]incidentDTO, 0, len(incidentRows))
	for i := range incidentRows {
		incidents = append(incidents, toIncidentDTO(&incidentRows[i]))
	}

	writeJSON(w, http.StatusOK, stationOverviewDTO{
		Station:       toStationDTO(station),
		Tanks:         tanks,
		Pumps:         pumps,
		OpenIncidents: incidents,
	})
}
