package server

import (
	"errors"
	"net/http"
	"time"

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

// shiftSummaryDTO is the dashboard view of an open shift: the shift plus its
// attendants and nozzle assignments, so the Shifts strip can render who is
// on and what they're running without extra calls.
type shiftSummaryDTO struct {
	shiftDTO
	Attendants        []attendantDTO        `json:"attendants"`
	NozzleAssignments []nozzleAssignmentDTO `json:"nozzle_assignments"`
}

type stationOverviewDTO struct {
	Station       stationDTO           `json:"station"`
	Tanks         []tankDTO            `json:"tanks"`
	Pumps         []pumpWithNozzlesDTO `json:"pumps"`
	OpenShifts    []shiftSummaryDTO    `json:"open_shifts"`
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

	stationFilter := []uuid.UUID{id}
	tankRows, err := s.tanks.List(ctx, actor.TenantID, stationFilter)
	if err != nil {
		s.logger.Error("station overview: tanks", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	pumpRows, err := s.pumps.List(ctx, actor.TenantID, stationFilter)
	if err != nil {
		s.logger.Error("station overview: pumps", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	nozzleRows, err := s.nozzles.List(ctx, actor.TenantID, stationFilter, nil)
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
	openShiftRows, err := s.operations.ListOpenShiftsForStation(ctx, actor.TenantID, id)
	if err != nil {
		s.logger.Error("station overview: shifts", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	nozzlesByPump := make(map[uuid.UUID][]nozzleDTO, len(pumpRows))
	for i := range nozzleRows {
		dto := toNozzleDTO(&nozzleRows[i])
		nozzlesByPump[dto.PumpID] = append(nozzlesByPump[dto.PumpID], dto)
	}

	// Latest dip-resolved volume + metadata per tank, for the visual fill
	// level (and so the dashboard can flag a stale, prior-day reading).
	currentByTank, err := s.readings.LatestDipsForStation(ctx, actor.TenantID, id)
	if err != nil {
		s.logger.Error("station overview: dip volumes", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tanks := make([]tankDTO, 0, len(tankRows))
	for i := range tankRows {
		dto := toTankDTO(&tankRows[i])
		if d, ok := currentByTank[tankRows[i].ID]; ok {
			vol := d.VolumeLitres
			dto.CurrentLitres = &vol
			rt := d.ReadingType
			dto.CurrentDipReadingType = &rt
			at := d.RecordedAt.Format(time.RFC3339)
			dto.CurrentDipRecordedAt = &at
			bd := d.BusinessDate.Format(dateLayout)
			dto.CurrentDipBusinessDate = &bd
		}
		tanks = append(tanks, dto)
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

	openShifts := make([]shiftSummaryDTO, 0, len(openShiftRows))
	for i := range openShiftRows {
		sh := openShiftRows[i]
		summary := shiftSummaryDTO{
			shiftDTO:          toShiftDTO(&sh),
			Attendants:        []attendantDTO{},
			NozzleAssignments: []nozzleAssignmentDTO{},
		}
		atts, err := s.operations.ListAttendants(ctx, actor.TenantID, sh.ID)
		if err != nil {
			s.logger.Error("station overview: shift attendants", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for j := range atts {
			summary.Attendants = append(summary.Attendants, toAttendantDTO(&atts[j]))
		}
		nas, err := s.operations.ListNozzleAssignments(ctx, actor.TenantID, sh.ID)
		if err != nil {
			s.logger.Error("station overview: shift nozzle assignments", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for j := range nas {
			summary.NozzleAssignments = append(summary.NozzleAssignments, toNozzleAssignmentDTO(&nas[j]))
		}
		openShifts = append(openShifts, summary)
	}

	writeJSON(w, http.StatusOK, stationOverviewDTO{
		Station:       toStationDTO(station),
		Tanks:         tanks,
		Pumps:         pumps,
		OpenShifts:    openShifts,
		OpenIncidents: incidents,
	})
}
