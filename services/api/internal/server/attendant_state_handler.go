package server

// GET /api/v1/attendant/current-shift — the Mobile Attendant App's workflow
// snapshot (Phase 1). SELF-SCOPED: gated by authentication alone, it only
// ever reads the calling actor's own shift membership, assignments, readings,
// and the 3-team rotation duty for their own employee record — never a
// station-wide surface.
//
// The response carries a computed next_action state machine the mobile home
// screen renders directly:
//
//	off_duty | await_shift_open | check_in | confirm_assignment |
//	verify_opening_readings | open_shift | working |
//	submit_closing_readings | await_reading_verification |
//	submit_collections | await_collection_receipt | complete | blocked
//
// open_shift is reserved: in the current backend a supervisor opens the shift
// (rotation auto-populates its attendants), so an expected-but-unopened day
// reports await_shift_open. blocked carries a machine-readable blocking_code.

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
	"github.com/japharyroman/fuelgrid-os/internal/readings"
)

// Attendance statuses surfaced on the snapshot.
const (
	attendanceNotCheckedIn = "not_checked_in"
)

// next_action values (the attendant workflow state machine).
const (
	actionOffDuty           = "off_duty"
	actionAwaitShiftOpen    = "await_shift_open"
	actionCheckIn           = "check_in"
	actionConfirmAssignment = "confirm_assignment"
	actionVerifyOpenings    = "verify_opening_readings"
	actionWorking           = "working"
	actionSubmitClosings    = "submit_closing_readings"
	actionAwaitVerification = "await_reading_verification"
	actionSubmitCollections = "submit_collections"
	actionAwaitReceipt      = "await_collection_receipt"
	actionComplete          = "complete"
	actionBlocked           = "blocked"
)

// blocking codes for next_action == blocked.
const (
	blockAwaitingNozzleAssignment = "awaiting_nozzle_assignment"
	blockAwaitingShiftClose       = "awaiting_shift_close"
	blockCollectionRejected       = "collection_rejected"
)

type attendantStationDTO struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type attendantExpectedTodayDTO struct {
	Slot     string    `json:"slot"`
	TeamID   uuid.UUID `json:"team_id"`
	TeamName string    `json:"team_name"`
}

type attendantAttendanceDTO struct {
	Status     string  `json:"status"` // not_checked_in | checked_in | checked_out
	CheckInAt  *string `json:"check_in_at,omitempty"`
	CheckOutAt *string `json:"check_out_at,omitempty"`
}

type attendantAssignmentDTO struct {
	AssignmentID uuid.UUID `json:"assignment_id"`
	NozzleID     uuid.UUID `json:"nozzle_id"`
	PumpNumber   int       `json:"pump_number"`
	NozzleNumber int       `json:"nozzle_number"`
	ProductName  string    `json:"product_name"`
	ProductColor string    `json:"product_color"`
	// MeterDecimalPlaces is the nozzle's meter precision (0..4); the mobile
	// capture screens validate input scale against it before submitting,
	// mirroring the server's readings.ValidateScale 422 (Phase 2).
	MeterDecimalPlaces int     `json:"meter_decimal_places"`
	AssignedAt         string  `json:"assigned_at"`
	ConfirmedAt        *string `json:"confirmed_at,omitempty"`
}

// attendantReadingDTO is the attendant's own meter progress on one nozzle.
// Reading figures are exact decimal STRINGS (numeric(14,3) -> text).
// VerificationStatus is set once a closing reading exists: "pending" until a
// reading_verifications row lands, then that row's status
// (approved|corrected|rejected). Once verified, FinalReading carries the
// verification's final_approved_reading (== ClosingReading when approved
// as-is, the supervisor's figure when corrected) and VerificationReason the
// supervisor's reason where one was required — the dual-value model surfaced
// to the attendant review-status screen (Phase 3, PRD §6.9).
type attendantReadingDTO struct {
	NozzleID           uuid.UUID `json:"nozzle_id"`
	OpeningReading     *string   `json:"opening_reading,omitempty"`
	ClosingReading     *string   `json:"closing_reading,omitempty"`
	VerificationStatus *string   `json:"verification_status,omitempty"`
	FinalReading       *string   `json:"final_reading,omitempty"`
	VerificationReason *string   `json:"verification_reason,omitempty"`
}

type attendantCurrentShiftDTO struct {
	Status       string  `json:"status"` // off_duty | expected_today | on_shift | complete
	NextAction   string  `json:"next_action"`
	UserMessage  string  `json:"user_message"`
	BlockingCode *string `json:"blocking_code,omitempty"`

	Station       *attendantStationDTO       `json:"station,omitempty"`
	Shift         *shiftDTO                  `json:"shift,omitempty"`
	ExpectedToday *attendantExpectedTodayDTO `json:"expected_today,omitempty"`

	Attendance  attendantAttendanceDTO   `json:"attendance"`
	Assignments []attendantAssignmentDTO `json:"assignments"`
	Readings    []attendantReadingDTO    `json:"readings"`

	// ExpectedOpeningsAvailable reports whether at least one of the actor's
	// nozzles has a derivable expected opening (the handover chain figure).
	ExpectedOpeningsAvailable bool `json:"expected_openings_available"`

	// ExpectedCash (exact decimal string) and the cash/receipt records appear
	// once the shift is closed.
	ExpectedCash      *string               `json:"expected_cash,omitempty"`
	CashSubmission    *cashSubmissionDTO    `json:"cash_submission,omitempty"`
	CollectionReceipt *collectionReceiptDTO `json:"collection_receipt,omitempty"`
}

// handleAttendantCurrentShift assembles the actor's workflow snapshot.
func (s *Server) handleAttendantCurrentShift(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()

	// 1) The actor's current (non-approved) shift, when they have one.
	shift, err := s.operations.ActiveShiftForAttendant(ctx, actor.TenantID, actor.UserID)
	if err == nil {
		out, herr := s.attendantShiftSnapshot(r, actor, shift)
		if herr != nil {
			s.logger.Error("attendant current shift", "error", herr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		s.logger.Error("attendant current shift", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// 2) A shift already approved today stays visible as "complete" so the
	// home screen doesn't snap back to off-duty mid-afternoon.
	if approved, aerr := s.operations.LatestApprovedShiftTodayForAttendant(ctx, actor.TenantID, actor.UserID); aerr == nil {
		out, herr := s.attendantShiftSnapshot(r, actor, approved)
		if herr != nil {
			s.logger.Error("attendant current shift", "error", herr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	} else if !errors.Is(aerr, pgx.ErrNoRows) {
		s.logger.Error("attendant current shift", "error", aerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// 3) No shift at all: consult the 3-team rotation for the actor's own
	// employee record — expected today, or off duty.
	duty, err := s.workforce.DutyForUser(ctx, actor.TenantID, actor.UserID, time.Now().UTC())
	if err != nil {
		s.logger.Error("attendant current shift: duty", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := attendantCurrentShiftDTO{
		Status:      "off_duty",
		NextAction:  actionOffDuty,
		UserMessage: "You are not on a shift today.",
		Attendance:  attendantAttendanceDTO{Status: attendanceNotCheckedIn},
		Assignments: []attendantAssignmentDTO{},
		Readings:    []attendantReadingDTO{},
	}
	if duty != nil && duty.OnDuty {
		stationName, nerr := s.operations.StationName(ctx, actor.TenantID, duty.StationID)
		if nerr != nil {
			s.logger.Error("attendant current shift: station", "error", nerr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out.Status = "expected_today"
		out.NextAction = actionAwaitShiftOpen
		out.UserMessage = "Your team " + duty.TeamName + " covers the " + string(duty.Slot) +
			" shift today at " + stationName + ". Wait for your supervisor to open the shift."
		out.Station = &attendantStationDTO{ID: duty.StationID, Name: stationName}
		out.ExpectedToday = &attendantExpectedTodayDTO{
			Slot: string(duty.Slot), TeamID: duty.TeamID, TeamName: duty.TeamName,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// attendantShiftSnapshot builds the snapshot for an actor that HAS a shift
// (open, closed, or approved-today) and computes next_action.
func (s *Server) attendantShiftSnapshot(r *http.Request, actor identity.Actor, shift *operations.Shift) (*attendantCurrentShiftDTO, error) {
	ctx := r.Context()

	stationName, err := s.operations.StationName(ctx, actor.TenantID, shift.StationID)
	if err != nil {
		return nil, err
	}

	attendance := attendantAttendanceDTO{Status: attendanceNotCheckedIn}
	if rec, aerr := s.operations.GetAttendance(ctx, actor.TenantID, shift.ID, actor.UserID); aerr == nil {
		checkIn := rec.CheckInAt.Format(time.RFC3339)
		attendance = attendantAttendanceDTO{
			Status: rec.Status, CheckInAt: &checkIn, CheckOutAt: fmtTime(rec.CheckOutAt),
		}
	} else if !errors.Is(aerr, operations.ErrAttendanceNotFound) {
		return nil, aerr
	}

	assignments, err := s.operations.AttendantAssignments(ctx, actor.TenantID, shift.ID, actor.UserID)
	if err != nil {
		return nil, err
	}
	assignmentDTOs := make([]attendantAssignmentDTO, 0, len(assignments))
	unconfirmed := 0
	myNozzles := map[uuid.UUID]bool{}
	for i := range assignments {
		a := assignments[i]
		myNozzles[a.NozzleID] = true
		if a.ConfirmedAt == nil {
			unconfirmed++
		}
		assignmentDTOs = append(assignmentDTOs, attendantAssignmentDTO{
			AssignmentID: a.ID, NozzleID: a.NozzleID,
			PumpNumber: a.PumpNumber, NozzleNumber: a.NozzleNumber,
			ProductName: a.ProductName, ProductColor: a.ProductColor,
			MeterDecimalPlaces: a.MeterDecimalPlaces,
			AssignedAt:         a.AssignedAt.Format(time.RFC3339), ConfirmedAt: fmtTime(a.ConfirmedAt),
		})
	}

	// The actor's own meter progress: active readings on their nozzles, with
	// each closing's verification status (dual-value model).
	meterRows, err := s.readings.ListActiveForShift(ctx, actor.TenantID, shift.ID)
	if err != nil {
		return nil, err
	}
	verifications, err := s.readings.ListVerificationsForShift(ctx, actor.TenantID, shift.ID)
	if err != nil {
		return nil, err
	}
	verifByReading := map[uuid.UUID]*readings.Verification{}
	for i := range verifications {
		verifByReading[verifications[i].ReadingID] = &verifications[i]
	}
	readingByNozzle := map[uuid.UUID]*attendantReadingDTO{}
	for i := range meterRows {
		m := meterRows[i]
		if !myNozzles[m.NozzleID] {
			continue
		}
		d := readingByNozzle[m.NozzleID]
		if d == nil {
			d = &attendantReadingDTO{NozzleID: m.NozzleID}
			readingByNozzle[m.NozzleID] = d
		}
		v := m.Reading
		if m.ReadingType == "opening" {
			d.OpeningReading = &v
		} else {
			d.ClosingReading = &v
			status := "pending"
			if ver, ok := verifByReading[m.ID]; ok {
				status = ver.Status
				final := ver.FinalApprovedReading
				d.FinalReading = &final
				d.VerificationReason = ver.Reason
			}
			d.VerificationStatus = &status
		}
	}
	openingsMissing, closingsCaptured, closingsUnverified := 0, 0, 0
	readingDTOs := make([]attendantReadingDTO, 0, len(assignments))
	for i := range assignments {
		d := readingByNozzle[assignments[i].NozzleID]
		if d == nil {
			d = &attendantReadingDTO{NozzleID: assignments[i].NozzleID}
		}
		if d.OpeningReading == nil {
			openingsMissing++
		}
		if d.ClosingReading != nil {
			closingsCaptured++
			if d.VerificationStatus != nil && *d.VerificationStatus == "pending" {
				closingsUnverified++
			}
		}
		readingDTOs = append(readingDTOs, *d)
	}

	// Expected-openings availability for the actor's nozzles (handover chain).
	expectedAvailable := false
	if len(assignments) > 0 {
		expected, eerr := s.operations.ExpectedOpeningsForShift(ctx, actor.TenantID, shift)
		if eerr != nil {
			return nil, eerr
		}
		for i := range expected {
			if expected[i].AttendantID == actor.UserID && expected[i].ExpectedReading != nil {
				expectedAvailable = true
				break
			}
		}
	}

	sd := toShiftDTO(shift)
	out := &attendantCurrentShiftDTO{
		Status:                    "on_shift",
		Station:                   &attendantStationDTO{ID: shift.StationID, Name: stationName},
		Shift:                     &sd,
		Attendance:                attendance,
		Assignments:               assignmentDTOs,
		Readings:                  readingDTOs,
		ExpectedOpeningsAvailable: expectedAvailable,
	}

	// Cash + receipt appear once the shift is closed (or approved).
	var (
		cash    *operations.CashSubmission
		receipt *operations.CollectionReceipt
	)
	if shift.Status != "open" {
		expectedCash, cerr := s.operations.SumExpectedForShift(ctx, s.operations.Pool(), actor.TenantID, shift.ID)
		if cerr != nil {
			return nil, cerr
		}
		out.ExpectedCash = &expectedCash
		cash, cerr = s.operations.GetCashSubmission(ctx, actor.TenantID, shift.ID)
		if cerr != nil && !errors.Is(cerr, pgx.ErrNoRows) {
			return nil, cerr
		}
		if cash != nil {
			dto := toCashSubmissionDTO(cash)
			out.CashSubmission = &dto
		}
		receipt, cerr = s.operations.GetCollectionReceiptForShift(ctx, actor.TenantID, shift.ID)
		if cerr != nil && !errors.Is(cerr, pgx.ErrNoRows) {
			return nil, cerr
		}
		if receipt != nil {
			dto := toCollectionReceiptDTO(receipt)
			out.CollectionReceipt = &dto
		}
	}

	computeAttendantNextAction(out, shift, attendance.Status, len(assignments),
		unconfirmed, openingsMissing, closingsCaptured, closingsUnverified, cash, receipt)
	return out, nil
}

// computeAttendantNextAction runs the workflow state machine over the
// assembled snapshot and stamps status / next_action / user_message (and the
// blocking code when blocked).
func computeAttendantNextAction(
	out *attendantCurrentShiftDTO, shift *operations.Shift, attendanceStatus string,
	assignmentCount, unconfirmed, openingsMissing, closingsCaptured, closingsUnverified int,
	cash *operations.CashSubmission, receipt *operations.CollectionReceipt,
) {
	block := func(code, message string) {
		out.NextAction = actionBlocked
		out.BlockingCode = &code
		out.UserMessage = message
	}

	switch shift.Status {
	case "open":
		switch {
		case attendanceStatus == attendanceNotCheckedIn:
			out.NextAction = actionCheckIn
			out.UserMessage = "Your shift is open. Check in to start working."
		case assignmentCount == 0:
			block(blockAwaitingNozzleAssignment, "You are checked in. Wait for your nozzle assignment.")
		case unconfirmed > 0:
			out.NextAction = actionConfirmAssignment
			out.UserMessage = "Confirm your nozzle assignment to continue."
		case openingsMissing > 0:
			out.NextAction = actionVerifyOpenings
			out.UserMessage = "Verify the opening reading on each of your nozzles."
		case closingsCaptured == 0:
			out.NextAction = actionWorking
			out.UserMessage = "You are set for this shift. Enter closing readings when your shift ends."
		case closingsCaptured < assignmentCount:
			out.NextAction = actionSubmitClosings
			out.UserMessage = "Enter the remaining closing readings for your nozzles."
		case closingsUnverified > 0:
			out.NextAction = actionAwaitVerification
			out.UserMessage = "Closing readings submitted. Wait for your supervisor to verify them."
		default:
			block(blockAwaitingShiftClose, "Readings verified. Wait for your supervisor to close the shift.")
		}
	case "closed":
		switch {
		case closingsUnverified > 0:
			out.NextAction = actionAwaitVerification
			out.UserMessage = "Closing readings submitted. Wait for your supervisor to verify them."
		case cash == nil:
			out.NextAction = actionSubmitCollections
			out.UserMessage = "Readings verified. Submit your shift collections."
		case receipt == nil:
			out.NextAction = actionAwaitReceipt
			out.UserMessage = "Collections submitted. Wait for your supervisor to confirm receipt."
		case receipt.Status == "rejected":
			block(blockCollectionRejected, "Your collection was rejected. See your supervisor.")
		default:
			out.Status = "complete"
			out.NextAction = actionComplete
			out.UserMessage = "Shift complete. Wait for final approval."
		}
	default: // approved
		out.Status = "complete"
		out.NextAction = actionComplete
		out.UserMessage = "Shift complete. Well done."
	}
}
