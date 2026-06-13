package server

// Mobile Attendant App Phase 7 report datasets, following the structured
// ReportEnvelope pattern (reports_structured_handlers.go):
//
//   - Attendance: station/date-range roster vs check-in/out with the
//     deterministic late / no-show derivation (late = checked in more than
//     operations.LateCheckInGrace after the shift opened).
//   - Corrections & variances: attendant-submitted vs final approved closing
//     readings with reason (the dual-value verification model), and expected
//     vs received collections with the SQL-computed difference + reason.
//
// Both are station-scoped via ?station_id exactly like the sibling reports
// (gated by station.read — the operations-domain read permission — plus the
// in-handler authorizeStation re-check), take an inclusive ?from/?to date
// window (default: the last 30 days), and pass every money/litre figure
// through as the exact decimal string from SQL numeric — no float money.

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/operations"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
)

// reportDateRange parses the optional ?from/?to date params (YYYY-MM-DD,
// inclusive). Defaults to the 30 days ending today (UTC). On a malformed or
// inverted range it writes a 400 and returns ok=false.
func reportDateRange(w http.ResponseWriter, r *http.Request) (from, to time.Time, ok bool) {
	now := time.Now().UTC()
	to = now
	from = now.AddDate(0, 0, -29)
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(dateLayout, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid to date (want YYYY-MM-DD)")
			return from, to, false
		}
		to = t
		from = t.AddDate(0, 0, -29)
	}
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(dateLayout, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid from date (want YYYY-MM-DD)")
			return from, to, false
		}
		from = t
	}
	if from.After(to) {
		writeError(w, http.StatusBadRequest, "from must not be after to")
		return from, to, false
	}
	return from, to, true
}

// handleAttendanceReport returns the Attendance dataset as a ReportEnvelope.
// Station-scoped via ?station_id (station.read); ?from/?to select the window.
func (s *Server) handleAttendanceReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, ok := s.resolveStationScoped(w, r, actor, "station.read")
	if !ok {
		return
	}
	from, to, ok := reportDateRange(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	sid := stationID.String()
	period := from.Format(dateLayout) + ".." + to.Format(dateLayout)
	env := newEnvelope("attendance", "Attendance", period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["from"] = from.Format(dateLayout)
	env.FiltersUsed["to"] = to.Format(dateLayout)

	rows, err := s.operations.AttendanceReportRows(ctx, actor.TenantID, stationID, from, to)
	if err != nil {
		s.logger.Error("attendance report", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	env.Table.Columns = []string{
		"shift", "slot", "shift_status", "attendant", "email",
		"attendance_status", "check_in_at", "check_out_at",
	}
	type chartDay struct {
		Date         string `json:"date"`
		Present      int    `json:"present"`
		Late         int    `json:"late"`
		NoShow       int    `json:"no_show"`
		NotCheckedIn int    `json:"not_checked_in"`
	}
	byDay := map[string]*chartDay{}
	var dayOrder []string
	var present, late, noShow, notCheckedIn int
	for i := range rows {
		row := rows[i]
		slot := ""
		if row.Slot != nil {
			slot = *row.Slot
		}
		checkIn, checkOut := "", ""
		if row.CheckInAt != nil {
			checkIn = row.CheckInAt.Format(time.RFC3339)
		}
		if row.CheckOutAt != nil {
			checkOut = row.CheckOutAt.Format(time.RFC3339)
		}
		env.Table.Rows = append(env.Table.Rows, []string{
			row.ShiftName, slot, row.ShiftStatus, row.AttendantName, row.AttendantEmail,
			row.DerivedStatus, checkIn, checkOut,
		})

		day := row.OpenedAt.UTC().Format(dateLayout)
		cd := byDay[day]
		if cd == nil {
			cd = &chartDay{Date: day}
			byDay[day] = cd
			dayOrder = append(dayOrder, day)
		}
		switch row.DerivedStatus {
		case "present":
			present++
			cd.Present++
		case "late":
			late++
			cd.Late++
		case "no_show":
			noShow++
			cd.NoShow++
		case "not_checked_in":
			notCheckedIn++
			cd.NotCheckedIn++
		}
	}
	// Chronological chart (rows are newest-first).
	chart := make([]chartDay, 0, len(dayOrder))
	for i := len(dayOrder) - 1; i >= 0; i-- {
		chart = append(chart, *byDay[dayOrder[i]])
	}
	env.ChartData = chart

	env.Summary = []summaryMetric{
		{Label: "Rostered", Value: strconv.Itoa(len(rows)), Unit: "count"},
		{Label: "Present", Value: strconv.Itoa(present), Unit: "count"},
		{Label: "Late", Value: strconv.Itoa(late), Unit: "count"},
		{Label: "No-shows", Value: strconv.Itoa(noShow), Unit: "count"},
		{Label: "Not checked in yet", Value: strconv.Itoa(notCheckedIn), Unit: "count"},
	}
	if noShow > 0 || late > 0 {
		env.Insights = append(env.Insights, reporting.Insight{
			Severity: reporting.SeverityWarning,
			Message: fmt.Sprintf("%d late check-in(s) and %d no-show(s) in the window (late = more than %d minutes after the shift opened).",
				late, noShow, int(operations.LateCheckInGrace.Minutes())),
			RecommendedAction: "Review the flagged shifts with the rostered attendants.",
		})
		env.RecommendedActions = append(env.RecommendedActions,
			"Review the flagged shifts with the rostered attendants.")
	}
	if len(rows) == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level: "warning", Message: "No rostered shifts in the selected window — attendance figures are unavailable.",
		})
	}

	env.Drilldown = []drilldownLink{
		{Label: "Operations overview", Href: fmt.Sprintf("/api/v1/stations/%s/operations/overview", sid)},
	}
	writeJSON(w, http.StatusOK, env)
}

// handleCorrectionsVariancesReport returns the Corrections & Variances dataset
// as a ReportEnvelope: supervisor-corrected (or rejected) closing readings with
// both values + reason, and collection receipts with expected vs received +
// difference + reason. Station-scoped via ?station_id (station.read).
func (s *Server) handleCorrectionsVariancesReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, ok := s.resolveStationScoped(w, r, actor, "station.read")
	if !ok {
		return
	}
	from, to, ok := reportDateRange(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	sid := stationID.String()
	period := from.Format(dateLayout) + ".." + to.Format(dateLayout)
	env := newEnvelope("corrections-variances", "Corrections & Variances", period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["from"] = from.Format(dateLayout)
	env.FiltersUsed["to"] = to.Format(dateLayout)

	corrections, err := s.readings.CorrectionReportRows(ctx, actor.TenantID, stationID, from, to)
	if err != nil {
		s.logger.Error("corrections report: readings", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	receipts, totals, err := s.operations.CollectionVarianceReport(ctx, actor.TenantID, stationID, from, to)
	if err != nil {
		s.logger.Error("corrections report: collections", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// One flattened grid over both datasets (kind column disambiguates); the
	// chart payload carries each dataset fully structured for richer rendering.
	env.Table.Columns = []string{
		"kind", "shift", "attendant", "reference",
		"submitted", "final_or_received", "expected", "variance",
		"reason", "decided_by", "decided_at",
	}
	type correctionItem struct {
		ShiftID          string  `json:"shift_id"`
		Shift            string  `json:"shift"`
		Pump             int     `json:"pump"`
		Nozzle           int     `json:"nozzle"`
		AttendantID      string  `json:"attendant_id"`
		Attendant        string  `json:"attendant"`
		SubmittedReading string  `json:"submitted_reading"`
		FinalReading     string  `json:"final_reading"`
		DeltaLitres      string  `json:"delta_litres"`
		Status           string  `json:"status"`
		Reason           *string `json:"reason,omitempty"`
		VerifiedBy       string  `json:"verified_by"`
		VerifiedAt       string  `json:"verified_at"`
	}
	type collectionItem struct {
		ShiftID        string  `json:"shift_id"`
		Shift          string  `json:"shift"`
		SubmittedByID  string  `json:"submitted_by_id"`
		SubmittedBy    string  `json:"submitted_by"`
		ExpectedAmount string  `json:"expected_amount"`
		SubmittedTotal string  `json:"submitted_total"`
		ReceivedTotal  string  `json:"received_total"`
		Difference     string  `json:"difference"`
		Status         string  `json:"status"`
		Reason         *string `json:"reason,omitempty"`
		ReceivedAt     string  `json:"received_at"`
	}
	chart := struct {
		Corrections []correctionItem `json:"corrections"`
		Collections []collectionItem `json:"collections"`
	}{Corrections: []correctionItem{}, Collections: []collectionItem{}}

	for i := range corrections {
		c := corrections[i]
		reason := ""
		if c.Reason != nil {
			reason = *c.Reason
		}
		ref := fmt.Sprintf("pump %d / nozzle %d", c.PumpNumber, c.NozzleNumber)
		env.Table.Rows = append(env.Table.Rows, []string{
			"reading_" + c.Status, c.ShiftName, c.AttendantName, ref,
			c.SubmittedReading, c.FinalReading, "", c.DeltaLitres,
			reason, c.VerifiedByName, c.VerifiedAt.Format(time.RFC3339),
		})
		chart.Corrections = append(chart.Corrections, correctionItem{
			ShiftID: c.ShiftID.String(), Shift: c.ShiftName,
			Pump: c.PumpNumber, Nozzle: c.NozzleNumber,
			AttendantID: c.AttendantID.String(), Attendant: c.AttendantName,
			SubmittedReading: c.SubmittedReading, FinalReading: c.FinalReading,
			DeltaLitres: c.DeltaLitres, Status: c.Status, Reason: c.Reason,
			VerifiedBy: c.VerifiedByName, VerifiedAt: c.VerifiedAt.Format(time.RFC3339),
		})
	}
	for i := range receipts {
		c := receipts[i]
		reason := ""
		if c.Reason != nil {
			reason = *c.Reason
		}
		env.Table.Rows = append(env.Table.Rows, []string{
			"collection_" + c.Status, c.ShiftName, c.SubmittedByName, "cash handover",
			c.SubmittedTotal, c.ReceivedTotal, c.ExpectedAmount, c.Difference,
			reason, "", c.ReceivedAt.Format(time.RFC3339),
		})
		chart.Collections = append(chart.Collections, collectionItem{
			ShiftID: c.ShiftID.String(), Shift: c.ShiftName,
			SubmittedByID: c.SubmittedBy.String(), SubmittedBy: c.SubmittedByName,
			ExpectedAmount: c.ExpectedAmount, SubmittedTotal: c.SubmittedTotal,
			ReceivedTotal: c.ReceivedTotal, Difference: c.Difference,
			Status: c.Status, Reason: c.Reason,
			ReceivedAt: c.ReceivedAt.Format(time.RFC3339),
		})
	}
	env.ChartData = chart

	env.Summary = []summaryMetric{
		{Label: "Reading corrections", Value: strconv.Itoa(len(corrections)), Unit: "count"},
		{Label: "Collection receipts", Value: strconv.Itoa(totals.ReceiptCount), Unit: "count"},
		{Label: "Total shortage", Value: totals.ShortageTotal, Unit: "TZS"},
		{Label: "Total excess", Value: totals.ExcessTotal, Unit: "TZS"},
	}
	if len(corrections) > 0 {
		env.Insights = append(env.Insights, reporting.Insight{
			Severity: reporting.SeverityWarning,
			Message: fmt.Sprintf("%d closing reading(s) were corrected or rejected by a supervisor in the window — the dual-value snapshots preserve both figures.",
				len(corrections)),
			RecommendedAction: "Review repeat-corrected attendants and nozzles for meter or training issues.",
		})
		env.RecommendedActions = append(env.RecommendedActions,
			"Review repeat-corrected attendants and nozzles for meter or training issues.")
	}
	if len(corrections) == 0 && totals.ReceiptCount == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level: "warning", Message: "No verifications or collection receipts in the selected window.",
		})
	}

	env.Drilldown = []drilldownLink{
		{Label: "Operations overview", Href: fmt.Sprintf("/api/v1/stations/%s/operations/overview", sid)},
		{Label: "Cash reconciliation report", Href: fmt.Sprintf("/api/v1/reports/cash-reconciliation?station_id=%s", sid)},
	}
	writeJSON(w, http.StatusOK, env)
}
