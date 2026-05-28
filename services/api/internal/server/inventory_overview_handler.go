package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/reconciliation"
)

// recentVarianceDTO is one reconciliation in a tank's variance history.
type recentVarianceDTO struct {
	OperatingDayID   uuid.UUID `json:"operating_day_id"`
	BusinessDate     string    `json:"business_date"`
	VarianceLitres   float64   `json:"variance_litres"`
	VariancePercent  float64   `json:"variance_percent"`
	TolerancePercent float64   `json:"tolerance_percent"`
	OverTolerance    bool      `json:"over_tolerance"`
	Status           string    `json:"status"`
	SealedAt         *string   `json:"sealed_at,omitempty"`
}

func toRecentVarianceDTO(rr *reconciliation.RecentReconciliation) recentVarianceDTO {
	return recentVarianceDTO{
		OperatingDayID: rr.OperatingDayID, BusinessDate: rr.BusinessDate.Format(dateLayout),
		VarianceLitres: rr.VarianceLitres, VariancePercent: rr.VariancePercent,
		TolerancePercent: rr.TolerancePercent,
		OverTolerance:    overTolerance(rr.VarianceLitres, rr.ClosingBook, rr.TolerancePercent),
		Status:           rr.Status, SealedAt: fmtTime(rr.SealedAt),
	}
}

// inventoryTankDTO is one tank's at-a-glance inventory health.
type inventoryTankDTO struct {
	Tank               tankDTO             `json:"tank"`
	BookBalance        float64             `json:"book_balance"`
	LatestPhysical     *float64            `json:"latest_physical,omitempty"`
	LatestPhysicalAt   *string             `json:"latest_physical_at,omitempty"`
	FillPercent        float64             `json:"fill_percent"`
	DaysOfStock        *float64            `json:"days_of_stock,omitempty"`
	LastReconciliation *recentVarianceDTO  `json:"last_reconciliation,omitempty"`
	RecentVariances    []recentVarianceDTO `json:"recent_variances"`
}

type inventoryOverviewDTO struct {
	Station stationDTO         `json:"station"`
	Tanks   []inventoryTankDTO `json:"tanks"`
}

// handleInventoryOverview returns each of a station's tanks with its current
// book balance, latest physical (dip) reading, fill %, a days-of-stock
// estimate, and its reconciliation history. Gated by inventory.read for the
// URL station via the route.
func (s *Server) handleInventoryOverview(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tankRows, err := s.tanks.List(ctx, actor.TenantID, []uuid.UUID{stationID})
	if err != nil {
		s.logger.Error("inventory overview: tanks", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	latest, err := s.readings.LatestDipsForStation(ctx, actor.TenantID, stationID)
	if err != nil {
		s.logger.Error("inventory overview: latest dips", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := inventoryOverviewDTO{Station: toStationDTO(station), Tanks: []inventoryTankDTO{}}
	for i := range tankRows {
		tank := tankRows[i]
		book, err := s.inventory.CurrentBalance(ctx, actor.TenantID, tank.ID)
		if err != nil {
			s.logger.Error("inventory overview: balance", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		recent, err := s.reconciliation.RecentForTank(ctx, actor.TenantID, tank.ID, 7)
		if err != nil {
			s.logger.Error("inventory overview: recent reconciliations", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		dailySales, err := s.inventory.AverageDailySales(ctx, actor.TenantID, tank.ID, 7)
		if err != nil {
			s.logger.Error("inventory overview: sales rate", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		dto := inventoryTankDTO{Tank: toTankDTO(&tank), BookBalance: book, RecentVariances: []recentVarianceDTO{}}
		if tank.CapacityLitres > 0 {
			dto.FillPercent = book / tank.CapacityLitres * 100
		}
		if dailySales > 0 {
			d := book / dailySales
			dto.DaysOfStock = &d
		}
		if ld, ok := latest[tank.ID]; ok {
			v := ld.VolumeLitres
			at := ld.RecordedAt.Format(time.RFC3339)
			dto.LatestPhysical = &v
			dto.LatestPhysicalAt = &at
		}
		for j := range recent {
			rv := toRecentVarianceDTO(&recent[j])
			dto.RecentVariances = append(dto.RecentVariances, rv)
			if j == 0 {
				first := rv
				dto.LastReconciliation = &first
			}
		}
		out.Tanks = append(out.Tanks, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

// reconciliationTankDTO is one tank on the reconciliation console: its current
// book/physical and the day's persisted reconciliation (nil if not yet run).
type reconciliationTankDTO struct {
	Tank           tankDTO            `json:"tank"`
	BookBalance    float64            `json:"book_balance"`
	LatestPhysical *float64           `json:"latest_physical,omitempty"`
	Reconciliation *reconciliationDTO `json:"reconciliation,omitempty"`
}

type reconciliationOverviewDTO struct {
	Station           stationDTO              `json:"station"`
	Day               *operatingDayDTO        `json:"day"`
	AllShiftsApproved bool                    `json:"all_shifts_approved"`
	Tanks             []reconciliationTankDTO `json:"tanks"`
}

// handleReconciliationOverview returns a station's tanks for the active (or
// ?operating_day_id) day, each with current book/physical and the persisted
// reconciliation if one exists, plus whether all the day's shifts are approved
// (so the console can gate the run action). Gated by reconciliation.read.
func (s *Server) handleReconciliationOverview(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := reconciliationOverviewDTO{Station: toStationDTO(station), Tanks: []reconciliationTankDTO{}}

	// Resolve the day: an explicit ?operating_day_id, else the latest active day.
	var dayID uuid.UUID
	if raw := r.URL.Query().Get("operating_day_id"); raw != "" {
		dayID, err = uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid operating_day_id")
			return
		}
		day, err := s.operations.GetDay(ctx, actor.TenantID, dayID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "operating day not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		dd := toOperatingDayDTO(day)
		out.Day = &dd
	} else {
		day, err := s.operations.LatestActiveDayForStation(ctx, actor.TenantID, stationID)
		if err == nil {
			dayID = day.ID
			dd := toOperatingDayDTO(day)
			out.Day = &dd
		} else if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	// Persisted reconciliations for the day, keyed by tank.
	recByTank := map[uuid.UUID]reconciliationDTO{}
	if out.Day != nil {
		unapproved, err := s.operations.UnapprovedShiftCountForDay(ctx, actor.TenantID, dayID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out.AllShiftsApproved = unapproved == 0

		recs, err := s.reconciliation.ListForStationDay(ctx, actor.TenantID, stationID, dayID)
		if err != nil {
			s.logger.Error("reconciliation overview: list", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for i := range recs {
			recByTank[recs[i].TankID] = toReconciliationDTO(&recs[i])
		}
	}

	tankRows, err := s.tanks.List(ctx, actor.TenantID, []uuid.UUID{stationID})
	if err != nil {
		s.logger.Error("reconciliation overview: tanks", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	latest, err := s.readings.LatestDipsForStation(ctx, actor.TenantID, stationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	for i := range tankRows {
		tank := tankRows[i]
		book, err := s.inventory.CurrentBalance(ctx, actor.TenantID, tank.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		dto := reconciliationTankDTO{Tank: toTankDTO(&tank), BookBalance: book}
		if ld, ok := latest[tank.ID]; ok {
			v := ld.VolumeLitres
			dto.LatestPhysical = &v
		}
		if rec, ok := recByTank[tank.ID]; ok {
			rc := rec
			dto.Reconciliation = &rc
		}
		out.Tanks = append(out.Tanks, dto)
	}
	writeJSON(w, http.StatusOK, out)
}
