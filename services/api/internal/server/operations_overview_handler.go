package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// operationsAttendantDTO names a shift attendant for the supervisor view.
type operationsAttendantDTO struct {
	UserID   uuid.UUID `json:"user_id"`
	FullName string    `json:"full_name"`
	Email    string    `json:"email"`
}

// operationsShiftDTO is one shift on the supervisor operations dashboard: the
// shift, who's on it, what they run, the close figures, the cash status, and
// any exceptions — everything needed to review and approve in one card.
type operationsShiftDTO struct {
	shiftDTO
	Attendants         []operationsAttendantDTO `json:"attendants"`
	NozzleAssignments  []nozzleAssignmentDTO    `json:"nozzle_assignments"`
	ExpectedCash       float64                  `json:"expected_cash"`
	LitresSold         float64                  `json:"litres_sold"`
	CashSubmission     *cashSubmissionDTO       `json:"cash_submission,omitempty"`
	Exceptions         []shiftExceptionDTO      `json:"exceptions"`
	OpenExceptionCount int                      `json:"open_exception_count"`
}

// operationsOverviewDTO is the single payload the /operations dashboard reads:
// a station's active operating day and every shift in it, denormalized so the
// frontend makes one call.
type operationsOverviewDTO struct {
	Station stationDTO           `json:"station"`
	Day     *operatingDayDTO     `json:"day"`
	Shifts  []operationsShiftDTO `json:"shifts"`
}

// handleOperationsOverview returns the station's active operating day plus all
// of its shifts, each with attendants, nozzle assignments, close totals, cash
// status, and exceptions. Gated by station.read for the URL station via the
// route. When no day is open or closed, day is null and shifts is empty.
func (s *Server) handleOperationsOverview(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, err := uuid.Parse(chi.URLParam(r, "stationID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station id")
		return
	}

	ctx := r.Context()
	station, err := s.stations.Get(ctx, actor.TenantID, stationID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "station not found")
		return
	}
	if err != nil {
		s.logger.Error("operations overview: station", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := operationsOverviewDTO{
		Station: toStationDTO(station),
		Shifts:  []operationsShiftDTO{},
	}

	day, err := s.operations.LatestActiveDayForStation(ctx, actor.TenantID, stationID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, out)
		return
	}
	if err != nil {
		s.logger.Error("operations overview: day", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	dayDTO := toOperatingDayDTO(day)
	out.Day = &dayDTO

	shifts, err := s.operations.ListShifts(ctx, actor.TenantID, stationID, &day.ID)
	if err != nil {
		s.logger.Error("operations overview: shifts", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	for i := range shifts {
		sh := shifts[i]
		dto := operationsShiftDTO{
			shiftDTO:          toShiftDTO(&sh),
			Attendants:        []operationsAttendantDTO{},
			NozzleAssignments: []nozzleAssignmentDTO{},
			Exceptions:        []shiftExceptionDTO{},
		}

		atts, err := s.operations.AttendantSummariesForShift(ctx, actor.TenantID, sh.ID)
		if err != nil {
			s.logger.Error("operations overview: attendants", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for j := range atts {
			dto.Attendants = append(dto.Attendants, operationsAttendantDTO{
				UserID: atts[j].UserID, FullName: atts[j].FullName, Email: atts[j].Email,
			})
		}

		nas, err := s.operations.ListNozzleAssignments(ctx, actor.TenantID, sh.ID)
		if err != nil {
			s.logger.Error("operations overview: nozzle assignments", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for j := range nas {
			dto.NozzleAssignments = append(dto.NozzleAssignments, toNozzleAssignmentDTO(&nas[j]))
		}

		lines, err := s.operations.ListCloseLines(ctx, actor.TenantID, sh.ID)
		if err != nil {
			s.logger.Error("operations overview: close lines", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for j := range lines {
			dto.ExpectedCash += lines[j].ExpectedValue
			dto.LitresSold += lines[j].LitresSold
		}

		if cash, err := s.operations.GetCashSubmission(ctx, actor.TenantID, sh.ID); err == nil {
			cd := toCashSubmissionDTO(cash)
			dto.CashSubmission = &cd
		} else if !errors.Is(err, pgx.ErrNoRows) {
			s.logger.Error("operations overview: cash", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		excs, err := s.operations.ListExceptionsForShift(ctx, actor.TenantID, sh.ID)
		if err != nil {
			s.logger.Error("operations overview: exceptions", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for j := range excs {
			dto.Exceptions = append(dto.Exceptions, toShiftExceptionDTO(&excs[j]))
			if excs[j].Status == "open" {
				dto.OpenExceptionCount++
			}
		}

		out.Shifts = append(out.Shifts, dto)
	}

	writeJSON(w, http.StatusOK, out)
}
