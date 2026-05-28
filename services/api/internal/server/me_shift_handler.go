package server

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

type myNozzleDTO struct {
	NozzleID           uuid.UUID `json:"nozzle_id"`
	PumpNumber         int       `json:"pump_number"`
	NozzleNumber       int       `json:"nozzle_number"`
	ProductName        string    `json:"product_name"`
	ProductColor       string    `json:"product_color"`
	TankCode           string    `json:"tank_code"`
	DefaultPrice       float64   `json:"default_price"`
	MeterDecimalPlaces int       `json:"meter_decimal_places"`
	OpeningReading     *float64  `json:"opening_reading,omitempty"`
	ClosingReading     *float64  `json:"closing_reading,omitempty"`
}

type myShiftDTO struct {
	Shift           *shiftDTO          `json:"shift"`
	AssignedNozzles []myNozzleDTO      `json:"assigned_nozzles"`
	ExpectedCash    *float64           `json:"expected_cash,omitempty"`
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
		v := meterRows[i].Reading
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
			ProductName: d.ProductName, ProductColor: d.ProductColor, TankCode: d.TankCode,
			DefaultPrice: d.DefaultPrice, MeterDecimalPlaces: d.MeterDecimalPlaces,
		}
		if e := byNozzle[d.NozzleID]; e != nil {
			n.OpeningReading = e.opening
			n.ClosingReading = e.closing
		}
		nozzles = append(nozzles, n)
	}

	sd := toShiftDTO(shift)
	out := myShiftDTO{Shift: &sd, AssignedNozzles: nozzles}

	// Once closed, surface expected cash and any submission for the cash form.
	if shift.Status == "closed" {
		lines, err := s.operations.ListCloseLines(ctx, actor.TenantID, shift.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		expected := sumExpected(lines)
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
