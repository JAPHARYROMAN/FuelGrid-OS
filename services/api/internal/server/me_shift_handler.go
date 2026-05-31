package server

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/readings"
)

type myNozzleDTO struct {
	NozzleID           uuid.UUID `json:"nozzle_id"`
	PumpNumber         int       `json:"pump_number"`
	NozzleNumber       int       `json:"nozzle_number"`
	ProductName        string    `json:"product_name"`
	ProductColor       string    `json:"product_color"`
	TankID             uuid.UUID `json:"tank_id"`
	TankCode           string    `json:"tank_code"`
	DefaultPrice       float64   `json:"default_price"`
	MeterDecimalPlaces int       `json:"meter_decimal_places"`
	OpeningReading     *float64  `json:"opening_reading,omitempty"`
	ClosingReading     *float64  `json:"closing_reading,omitempty"`
}

type myTankDTO struct {
	TankID        uuid.UUID `json:"tank_id"`
	TankCode      string    `json:"tank_code"`
	ProductColor  string    `json:"product_color"`
	OpeningDipMM  *float64  `json:"opening_dip_mm,omitempty"`
	OpeningVolume *float64  `json:"opening_volume_litres,omitempty"`
	ClosingDipMM  *float64  `json:"closing_dip_mm,omitempty"`
	ClosingVolume *float64  `json:"closing_volume_litres,omitempty"`
}

type myShiftDTO struct {
	Shift           *shiftDTO          `json:"shift"`
	AssignedNozzles []myNozzleDTO      `json:"assigned_nozzles"`
	AssignedTanks   []myTankDTO        `json:"assigned_tanks"`
	ExpectedCash    *string            `json:"expected_cash,omitempty"`
	CashSubmission  *cashSubmissionDTO `json:"cash_submission,omitempty"`
}

// handleMyActiveShift returns the authenticated actor's current shift and
// the nozzles they're assigned to, denormalized so an attendant needs no
// station-wide read access. Self-scoped: it only ever returns shifts the
// actor is an attendant on, so it's gated by authentication alone.
func (s *Server) handleMyActiveShift(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()

	shift, err := s.operations.ActiveShiftForAttendant(ctx, actor.TenantID, actor.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, myShiftDTO{Shift: nil, AssignedNozzles: []myNozzleDTO{}})
		return
	}
	if err != nil {
		s.logger.Error("my active shift", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	details, err := s.operations.AssignedNozzleDetails(ctx, actor.TenantID, shift.ID, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	meterRows, err := s.readings.ListActiveForShift(ctx, actor.TenantID, shift.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	type ends struct{ opening, closing *float64 }
	byNozzle := map[uuid.UUID]*ends{}
	for i := range meterRows {
		e := byNozzle[meterRows[i].NozzleID]
		if e == nil {
			e = &ends{}
			byNozzle[meterRows[i].NozzleID] = e
		}
		// MD boundary: the attendant /me/shift view exposes readings as JSON
		// numbers; parse the exact-decimal string for that display only.
		v := dispDecimal(meterRows[i].Reading)
		if meterRows[i].ReadingType == "opening" {
			e.opening = &v
		} else {
			e.closing = &v
		}
	}

	nozzles := make([]myNozzleDTO, 0, len(details))
	for i := range details {
		d := details[i]
		n := myNozzleDTO{
			NozzleID: d.NozzleID, PumpNumber: d.PumpNumber, NozzleNumber: d.NozzleNumber,
			ProductName: d.ProductName, ProductColor: d.ProductColor,
			TankID: d.TankID, TankCode: d.TankCode,
			DefaultPrice: d.DefaultPrice, MeterDecimalPlaces: d.MeterDecimalPlaces,
		}
		if e := byNozzle[d.NozzleID]; e != nil {
			n.OpeningReading = e.opening
			n.ClosingReading = e.closing
		}
		nozzles = append(nozzles, n)
	}

	// Assigned tanks (unique, in nozzle order) with any captured dips, so the
	// attendant can record dips without the station-scoped dip-list endpoint.
	dipRows, err := s.readings.ListDipsForShift(ctx, actor.TenantID, shift.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	type dipEnds struct{ opening, closing *readings.DipReading }
	dipByTank := map[uuid.UUID]*dipEnds{}
	for i := range dipRows {
		e := dipByTank[dipRows[i].TankID]
		if e == nil {
			e = &dipEnds{}
			dipByTank[dipRows[i].TankID] = e
		}
		if dipRows[i].ReadingType == "opening" {
			e.opening = &dipRows[i]
		} else {
			e.closing = &dipRows[i]
		}
	}
	tanks := make([]myTankDTO, 0)
	seenTank := map[uuid.UUID]bool{}
	for i := range details {
		d := details[i]
		if seenTank[d.TankID] {
			continue
		}
		seenTank[d.TankID] = true
		td := myTankDTO{TankID: d.TankID, TankCode: d.TankCode, ProductColor: d.ProductColor}
		if e := dipByTank[d.TankID]; e != nil {
			// MD boundary: the attendant /me/shift view exposes dips as JSON
			// numbers; parse the exact-decimal strings for that display only.
			if e.opening != nil {
				od := dispDecimal(e.opening.DipMM)
				ov := dispDecimal(e.opening.VolumeLitres)
				td.OpeningDipMM = &od
				td.OpeningVolume = &ov
			}
			if e.closing != nil {
				cd := dispDecimal(e.closing.DipMM)
				cv := dispDecimal(e.closing.VolumeLitres)
				td.ClosingDipMM = &cd
				td.ClosingVolume = &cv
			}
		}
		tanks = append(tanks, td)
	}

	sd := toShiftDTO(shift)
	out := myShiftDTO{Shift: &sd, AssignedNozzles: nozzles, AssignedTanks: tanks}

	// Once closed, surface expected cash and any submission for the cash form.
	if shift.Status == "closed" {
		// Expected cash is SUM(expected_value) computed in SQL numeric.
		expected, err := s.operations.SumExpectedForShift(ctx, s.operations.Pool(), actor.TenantID, shift.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out.ExpectedCash = &expected
		if cash, err := s.operations.GetCashSubmission(ctx, actor.TenantID, shift.ID); err == nil {
			dto := toCashSubmissionDTO(cash)
			out.CashSubmission = &dto
		} else if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	writeJSON(w, http.StatusOK, out)
}
