package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
	"github.com/japharyroman/fuelgrid-os/internal/risk"
)

// Risk & Loss intelligence report (Reports §5.11 / §20.4). This enriches the
// original fuel-loss endpoint into the signature Risk & Loss report: a KPI hero
// (loss litres + value, variance %, open alerts/investigations, repeated
// incidents, highest-risk station), the DETERMINISTIC §5.11 pattern intelligence
// (variance events counted by station/product/pump/shift/attendant over a window,
// turned into "<x> appeared in <pct>% of related events" findings by the
// internal/reporting composer), and the reusable visuals — a risk HEATMAP
// (station × risk-type), a loss TREND, a station risk RANKING, a root-cause
// DONUT, an alert-severity BOARD, and an investigation TIMELINE — all carried in
// one envelope. Loss VALUE is sensitive: it is margin.view-gated and OMITTED
// (not zeroed) for non-holders. Station-scoped, gated by reconciliation.read at
// the route (the variance data's read permission, consistent with fuel-loss);
// the risk rules driving the alerts are surfaced read-only as a tuning context.

// riskLossWindowDays is the variance-event look-back window the pattern
// intelligence is computed over (a transparent, fixed 30-day window — the same
// horizon RecentForTank uses). Kept explicit so every "% of related events"
// figure has a stated denominator window.
const riskLossWindowDays = 30

// lossEventRow is one over-tolerance variance event with the dimension labels the
// §5.11 pattern joins need. variance_litres / variance_value are exact decimal
// strings (numeric::text); loss_litres is the |variance| when the event is a
// shortage (negative variance), else "0". All money/litre arithmetic stays in
// SQL — Go never recomputes a figure here.
type lossEventRow struct {
	ReconID      uuid.UUID
	StationID    uuid.UUID
	StationName  string
	BusinessDate time.Time
	TankCode     string
	ProductName  string
	ProductColor string
	OverTol      bool
	HasDip       bool
	Sealed       bool
	LossLitres   string // |variance| when a shortage, else "0"
	LossValue    string // |variance|×price when a shortage AND priced, else ""
	VariancePct  string
	ShiftLabels  []string // shift name(s) that ran a nozzle on the event's tank that day
	Attendants   []string // attendant name(s) assigned to a nozzle on the event's tank that day
}

// loadRiskLossEvents reads the station's variance events over the window with the
// dimension labels (station / product / pump / shift / attendant) the §5.11
// pattern intelligence is computed from. Every figure is computed in SQL numeric
// and returned as an exact decimal string; the per-event shift/attendant labels
// are CAUSALLY linked to the event — only shifts/attendants that operated a
// nozzle pulling from the event's tank on that operating day, via
// shift_nozzle_assignments → nozzles → tank, so the "% of related events" tally
// reflects who ran the affected tank rather than everyone rostered that day.
// Returns events newest-first.
func (s *Server) loadRiskLossEvents(ctx context.Context, tenantID, stationID uuid.UUID) ([]lossEventRow, error) {
	rows, err := s.deps.DB.Query(ctx, `
		WITH ev AS (
			SELECT tr.id AS recon_id, tr.tank_id, tr.operating_day_id,
			       od.station_id, od.business_date,
			       tr.variance_litres, tr.variance_percent, tr.closing_book,
			       tr.tolerance_percent, tr.status,
			       t.code AS tank_code,
			       p.name AS product_name, COALESCE(p.color, '') AS product_color,
			       CASE WHEN p.default_price > 0 THEN p.default_price::text ELSE NULL END AS price
			FROM tank_reconciliations tr
			JOIN tanks t           ON t.id = tr.tank_id  AND t.tenant_id = tr.tenant_id
			JOIN operating_days od  ON od.id = tr.operating_day_id AND od.tenant_id = tr.tenant_id
			JOIN products p         ON p.id = t.product_id AND p.tenant_id = t.tenant_id
			WHERE tr.tenant_id = $1
			  AND od.station_id = $2
			  AND od.business_date >= (now()::date - make_interval(days => $3))
		),
		dips AS (
			SELECT DISTINCT d.tank_id
			FROM tank_dip_readings d
			JOIN tanks t ON t.id = d.tank_id AND t.tenant_id = d.tenant_id
			WHERE d.tenant_id = $1 AND d.status = 'active' AND t.station_id = $2
		),
		-- Shifts/attendants are attributed to a variance event ONLY through the
		-- nozzle(s) that actually pull from the event's tank — a shift that ran a
		-- nozzle drawing the affected tank's product on that operating day, and the
		-- attendant assigned to that nozzle. This ties the §5.11 "% of related
		-- events" tally to who OPERATED the affected tank, not to everyone rostered
		-- that day (which would credit unrelated staff with another tank's loss).
		shifts_agg AS (
			SELECT sh.operating_day_id, n.tank_id,
			       array_agg(DISTINCT sh.name) AS shift_names
			FROM shifts sh
			JOIN shift_nozzle_assignments sna ON sna.shift_id = sh.id AND sna.tenant_id = sh.tenant_id
			JOIN nozzles n                    ON n.id = sna.nozzle_id AND n.tenant_id = sh.tenant_id
			WHERE sh.tenant_id = $1
			GROUP BY sh.operating_day_id, n.tank_id
		),
		att_agg AS (
			SELECT sh.operating_day_id, n.tank_id,
			       array_agg(DISTINCT COALESCE(u.full_name, u.email, sna.attendant_id::text)) AS attendants
			FROM shifts sh
			JOIN shift_nozzle_assignments sna ON sna.shift_id = sh.id AND sna.tenant_id = sh.tenant_id
			JOIN nozzles n                    ON n.id = sna.nozzle_id AND n.tenant_id = sh.tenant_id
			JOIN users u                      ON u.id = sna.attendant_id AND u.tenant_id = sh.tenant_id
			WHERE sh.tenant_id = $1
			GROUP BY sh.operating_day_id, n.tank_id
		)
		SELECT ev.recon_id, ev.station_id, st.name AS station_name, ev.business_date,
		       ev.tank_code, ev.product_name, ev.product_color,
		       (ev.status = 'exception'
		         OR abs(ev.variance_litres) > abs(ev.closing_book) * ev.tolerance_percent / 100.0) AS over_tol,
		       (d.tank_id IS NOT NULL) AS has_dip,
		       (ev.status = 'sealed') AS sealed,
		       CASE WHEN ev.variance_litres < 0 THEN abs(ev.variance_litres)::text ELSE '0' END AS loss_litres,
		       CASE WHEN ev.variance_litres < 0 AND ev.price IS NOT NULL
		            THEN (abs(ev.variance_litres) * ev.price::numeric)::numeric(14,2)::text
		            ELSE '' END AS loss_value,
		       ev.variance_percent::text AS variance_pct,
		       COALESCE(sg.shift_names, ARRAY[]::text[]) AS shift_names,
		       COALESCE(aa.attendants, ARRAY[]::text[]) AS attendants
		FROM ev
		JOIN stations st         ON st.id = ev.station_id AND st.tenant_id = $1
		LEFT JOIN dips d         ON d.tank_id = ev.tank_id
		LEFT JOIN shifts_agg sg  ON sg.operating_day_id = ev.operating_day_id AND sg.tank_id = ev.tank_id
		LEFT JOIN att_agg aa     ON aa.operating_day_id = ev.operating_day_id AND aa.tank_id = ev.tank_id
		ORDER BY ev.business_date DESC, ev.tank_code
	`, tenantID, stationID, riskLossWindowDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []lossEventRow{}
	for rows.Next() {
		var e lossEventRow
		if err := rows.Scan(&e.ReconID, &e.StationID, &e.StationName, &e.BusinessDate,
			&e.TankCode, &e.ProductName, &e.ProductColor, &e.OverTol, &e.HasDip, &e.Sealed,
			&e.LossLitres, &e.LossValue, &e.VariancePct, &e.ShiftLabels, &e.Attendants); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// riskHeatRow is one row of the station × risk-type heatmap chart payload: a
// station with a count cell per risk type. Counts are integers (event/alert
// tallies), so they are plain ints — no money on this path.
type riskHeatRow struct {
	Station string         `json:"station"`
	Cells   map[string]int `json:"cells"`
}

// riskRuleSummary is one read-only row of the rules-tuning context: enough of the
// configured rule to show what is driving the alerts, without rebuilding the CRUD.
type riskRuleSummary struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Condition string `json:"condition"`
	Severity  string `json:"severity"`
	Threshold string `json:"threshold"`
	Enabled   bool   `json:"enabled"`
	Status    string `json:"status"`
}

// riskLossChartData is the Risk & Loss report's report-specific chart payload —
// every visual the §5.11 page renders, carried in one envelope. Decimal strings
// for money/litres; ints for counts; floats never touch a money path.
type riskLossChartData struct {
	Heatmap        []riskHeatRow              `json:"heatmap"`        // station × risk-type counts
	HeatTypes      []string                   `json:"heat_types"`     // the risk-type columns
	Trend          []riskLossTrendPoint       `json:"trend"`          // loss litres by date
	Ranking        []riskStationRank          `json:"ranking"`        // station risk ranking
	Distribution   []reporting.DonutDatum     `json:"distribution"`   // root-cause / pattern donut
	AlertBoard     []riskAlertChip            `json:"alert_board"`    // alert-severity board
	Investigations []riskInvestigationStep    `json:"investigations"` // investigation timeline
	Patterns       []reporting.PatternFinding `json:"patterns"`       // §5.11 traceable findings
	Rules          []riskRuleSummary          `json:"rules"`          // read-only rules-tuning context
	ValueShown     bool                       `json:"value_shown"`    // loss VALUE permitted
}

// riskLossTrendPoint is one day's loss on the trend line (decimal-string litres,
// and value only when permitted).
type riskLossTrendPoint struct {
	Date       string `json:"date"`
	LossLitres string `json:"loss_litres"`
	LossValue  string `json:"loss_value,omitempty"`
	Events     int    `json:"events"`
}

// riskStationRank is one station's risk-ranking row (score + open alerts + band).
type riskStationRank struct {
	Station    string `json:"station"`
	Score      int    `json:"score"`
	Band       string `json:"band"`
	OpenAlerts int    `json:"open_alerts"`
}

// riskAlertChip is one chip on the alert-severity board: a severity bucket with
// its open count, mapped to a StatusBoard tone.
type riskAlertChip struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Tone   string `json:"tone"`
	Count  int    `json:"count"`
	Detail string `json:"detail"`
}

// riskInvestigationStep is one node of the investigation lifecycle timeline
// (open → investigating → resolved/dismissed), mapped onto the ShiftTimeline.
type riskInvestigationStep struct {
	Title  string `json:"title"`
	Status string `json:"status"` // done | current | pending | failed
	When   string `json:"when"`
	Detail string `json:"detail"`
}

// handleRiskLossReport returns the §5.11 / §20.4 Risk & Loss intelligence report
// as a ReportEnvelope. Station-scoped, gated by reconciliation.read. The loss
// VALUE is sensitive (margin.view) and OMITTED for non-holders.
func (s *Server) handleRiskLossReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, ok := s.resolveStationScoped(w, r, actor, "reconciliation.read")
	if !ok {
		return
	}
	ctx := r.Context()
	period := reportPeriodParam(r)
	sid := stationID.String()
	env := newEnvelope("risk-loss", "Risk & Loss Intelligence", period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["period"] = period

	valueShown := s.canViewMarginAtStation(ctx, actor, stationID)
	// The cross-station risk ranking, the highest-risk-station KPI and the
	// investigation timeline (with case titles) are sourced from the risk.* repos,
	// which are themselves gated by risk.read / investigation.read — permissions a
	// reconciliation.read-only actor (station_manager / supervisor) does NOT hold.
	// Surfacing them through this report would leak risk.read-gated, tenant-wide
	// data (other stations' scores, bands, open-alert counts and case titles) to an
	// actor who is 403'd at /risk/scores and /risk/cases. Gate them here: omit (not
	// zero) for non-holders, with a data-quality note, mirroring the margin gate.
	riskShown := s.canViewRiskIntel(ctx, actor)
	investShown := s.canViewInvestigations(ctx, actor)

	events, eerr := s.loadRiskLossEvents(ctx, actor.TenantID, stationID)
	if eerr != nil {
		s.logger.Error("risk-loss report: load events", "error", eerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// ---- Aggregate the variance events into the §5.11 dimensions + totals ----
	// All money/litre sums stay as decimal-string arithmetic done by accumulating
	// the SQL-computed per-event decimal strings; the only float coercion is the
	// loss-litre/value running totals for the DISPLAY headline (never a persisted
	// figure), exactly as the other structured reports do.
	in := reporting.RiskLossInput{
		StationLabel:   "",
		WindowDays:     riskLossWindowDays,
		LossValueShown: valueShown,
	}
	byPump := map[string]*dimAgg{}
	byShift := map[string]*dimAgg{}
	byAtt := map[string]*dimAgg{}
	byProduct := map[string]*dimAgg{}
	byDate := map[string]*trendAgg{}
	var lossLitresTotal, lossValueTotal float64
	var totalEvents, eventsMissingDip, incompleteRecons int
	tankBreaches := map[string]int{} // tank code -> over-tolerance days (recurring)
	stationName := ""

	for i := range events {
		e := events[i]
		if stationName == "" {
			stationName = e.StationName
		}
		// Loss litres/value accumulate over ALL shortage events (negative variance),
		// the report's "total loss". The variance-event dimensions and the pattern
		// math count OVER-TOLERANCE events only (the §5.11 "related events").
		if lv, ok := parseFloatSafe(e.LossLitres); ok {
			lossLitresTotal += lv
		}
		if valueShown {
			if vv, ok := parseFloatSafe(e.LossValue); ok {
				lossValueTotal += vv
			}
		}
		td := e.BusinessDate.Format(dateLayout)
		ta := byDate[td]
		if ta == nil {
			ta = &trendAgg{}
			byDate[td] = ta
		}
		ta.addLoss(e.LossLitres, e.LossValue, valueShown)

		if !e.Sealed {
			incompleteRecons++
		}
		if !e.OverTol {
			continue
		}
		totalEvents++
		ta.events++
		tankBreaches[e.TankCode]++
		if !e.HasDip {
			eventsMissingDip++
		}
		accumDim(byPump, e.TankCode, e.TankCode)
		accumDim(byProduct, e.ProductName, e.ProductName)
		for _, sh := range e.ShiftLabels {
			accumDim(byShift, sh, sh)
		}
		for _, at := range e.Attendants {
			accumDim(byAtt, at, at)
		}
	}
	in.StationLabel = stationName
	in.TotalEvents = totalEvents
	in.EventsMissingDip = eventsMissingDip
	in.IncompleteRecons = incompleteRecons
	in.LossLitres = strconv.FormatFloat(lossLitresTotal, 'f', 3, 64)
	if valueShown {
		in.LossValue = strconv.FormatFloat(lossValueTotal, 'f', 2, 64)
	}
	for code, days := range tankBreaches {
		_ = code
		if days >= 2 {
			in.RepeatedTanks++
		}
	}
	in.ByPump = dimTallies(byPump)
	in.ByShift = dimTallies(byShift)
	in.ByAttendant = dimTallies(byAtt)
	in.ByProduct = dimTallies(byProduct)

	// ---- Open risk alerts (filtered to this station) + severity board ----
	alertsBySeverity := map[string]int{}
	openAlerts := 0
	if alerts, aerr := s.risk.ListAlerts(ctx, actor.TenantID, "open", ""); aerr == nil {
		for i := range alerts {
			if alerts[i].StationID != nil && *alerts[i].StationID == stationID {
				openAlerts++
				alertsBySeverity[alerts[i].Severity]++
			}
		}
	}
	in.OpenAlerts = openAlerts

	// ---- Open investigations (tenant-scoped; the lifecycle timeline) ----
	// Investigation cases carry no station id, so the list is inherently
	// tenant-wide; surface it ONLY to holders of investigation.read (the same gate
	// /investigations enforces). A non-holder sees neither the count nor the
	// case-title timeline — omit, not zero.
	openInvest := 0
	investSteps := []riskInvestigationStep{}
	if investShown {
		cases, _ := s.risk.ListCases(ctx, actor.TenantID, "")
		investSteps = investigationTimeline(cases)
		for i := range cases {
			if !caseTerminal(cases[i].Status) {
				openInvest++
			}
		}
	}
	in.OpenInvestations = openInvest

	// ---- Disabled-rule data-quality signal + read-only rules-tuning context ----
	rules, _ := s.risk.ListRules(ctx, actor.TenantID)
	ruleSummaries, lossRuleDisabled := summarizeRiskRules(rules)
	in.DisabledLossRule = lossRuleDisabled

	// ---- Compose the deterministic insights + data-quality ----
	rep := reporting.RiskLoss(in)
	env.applyReport(rep)
	patterns := reporting.RiskLossPatterns(in)

	// Omit-not-zero data-quality notes for the risk.read / investigation.read
	// gated sections, so a non-holder understands what is hidden (rather than
	// reading a missing ranking/timeline as "no data").
	if !riskShown {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "info",
			Message: "The cross-station risk ranking and highest-risk station are hidden — they require the risk.read permission. This station's own loss and pattern figures are shown in full.",
		})
	}
	if !investShown {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "info",
			Message: "Open investigations are hidden — they require the investigation.read permission.",
		})
	}

	// ---- KPI hero (§5.11): loss litres + value (gated), variance %, open
	// alerts, open investigations, repeated incidents, highest-risk station ----
	// The station risk ranking + highest-risk KPI are risk.read-gated, tenant-wide
	// leadership data; resolve them only for holders of risk.read (omit, not zero).
	var ranking []riskStationRank
	highestRisk := ""
	if riskShown {
		scores, _ := s.risk.ListScores(ctx, actor.TenantID, "station")
		ranking, highestRisk = stationRanking(ctx, s, actor.TenantID, scores, stationID)
	}

	env.Summary = []summaryMetric{
		{Label: "Total loss litres", Value: in.LossLitres, Unit: "L"},
	}
	if valueShown {
		env.Summary = append(env.Summary, summaryMetric{
			Label: "Loss value", Value: in.LossValue, Unit: "TZS",
		})
	}
	env.Summary = append(env.Summary,
		summaryMetric{Label: "Over-tolerance events", Value: strconv.Itoa(totalEvents), Unit: "count"},
		summaryMetric{Label: "Repeated-incident tanks", Value: strconv.Itoa(in.RepeatedTanks), Unit: "count"},
		summaryMetric{Label: "Open risk alerts", Value: strconv.Itoa(openAlerts), Unit: "count"},
	)
	if investShown {
		env.Summary = append(env.Summary,
			summaryMetric{Label: "Open investigations", Value: strconv.Itoa(openInvest), Unit: "count"})
	}
	if highestRisk != "" {
		env.Summary = append(env.Summary, summaryMetric{Label: "Highest-risk station", Value: highestRisk})
	}

	// ---- Build the chart payload (REUSE merged primitives) ----
	heatRows, heatTypes := riskHeatmap(stationName, totalEvents, in.RepeatedTanks, openAlerts, openInvest)
	trend := trendSeries(byDate, valueShown)
	distribution := patternDistribution(in)

	chart := riskLossChartData{
		Heatmap:        heatRows,
		HeatTypes:      heatTypes,
		Trend:          trend,
		Ranking:        ranking,
		Distribution:   distribution,
		AlertBoard:     alertBoard(alertsBySeverity),
		Investigations: investSteps,
		Patterns:       patterns,
		Rules:          ruleSummaries,
		ValueShown:     valueShown,
	}
	env.ChartData = chart

	// ---- Drillable table: variance event history (loss -> station -> product ->
	// tank -> reconciliation -> shift -> attendant, as far as data supports) ----
	env.Table.Columns = []string{
		"business_date", "product", "pump", "variance_pct", "loss_litres", "over_tolerance", "shift", "attendant",
	}
	for i := range events {
		e := events[i]
		env.Table.Rows = append(env.Table.Rows, []string{
			e.BusinessDate.Format(dateLayout), e.ProductName, e.TankCode, e.VariancePct,
			e.LossLitres, strconv.FormatBool(e.OverTol),
			strings.Join(e.ShiftLabels, ", "), strings.Join(e.Attendants, ", "),
		})
	}

	if totalEvents == 0 && len(events) == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "warning",
			Message: "No reconciliations recorded for this station in the window — loss and pattern figures are unavailable.",
		})
	}

	// ---- Drilldown: as far as the data supports, plus the rules-tuning context ----
	env.Drilldown = []drilldownLink{
		{Label: "Risk alerts", Href: "/api/v1/risk/alerts?status=open"},
		{Label: "Risk rules (tuning)", Href: "/api/v1/risk/rules"},
		{Label: "Reconciliation report", Href: fmt.Sprintf("/api/v1/reports/inventory/reconciliation?station_id=%s", sid)},
		{Label: "Inventory overview", Href: fmt.Sprintf("/api/v1/stations/%s/inventory/overview", sid)},
	}
	env.ExportOptions = []exportOption{
		{Format: "csv", URL: fmt.Sprintf("/api/v1/stations/%s/reports/reconciliation.csv", sid)},
		{Format: "xlsx", URL: fmt.Sprintf("/api/v1/stations/%s/reports/reconciliation.xlsx", sid)},
	}
	writeJSON(w, http.StatusOK, env)
}

// ---- aggregation helpers ----

// dimAgg accumulates the event count for one dimension value.
type dimAgg struct {
	label string
	count int
}

func accumDim(m map[string]*dimAgg, key, label string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	a := m[key]
	if a == nil {
		a = &dimAgg{label: label}
		m[key] = a
	}
	a.count++
}

// dimTallies flattens a dimension map into the composer's tally slice, sorted by
// count desc then label for deterministic output.
func dimTallies(m map[string]*dimAgg) []reporting.LossDimensionTally {
	out := make([]reporting.LossDimensionTally, 0, len(m))
	for k, a := range m {
		out = append(out, reporting.LossDimensionTally{Key: k, Label: a.label, Count: a.count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// trendAgg accumulates a day's loss litres/value (decimal strings) + event count.
type trendAgg struct {
	lossLitres float64
	lossValue  float64
	events     int
}

func (t *trendAgg) addLoss(litres, value string, valueShown bool) {
	if lv, ok := parseFloatSafe(litres); ok {
		t.lossLitres += lv
	}
	if valueShown {
		if vv, ok := parseFloatSafe(value); ok {
			t.lossValue += vv
		}
	}
}

// trendSeries renders the per-day loss trend chronologically (decimal strings).
func trendSeries(byDate map[string]*trendAgg, valueShown bool) []riskLossTrendPoint {
	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	out := make([]riskLossTrendPoint, 0, len(dates))
	for _, d := range dates {
		a := byDate[d]
		pt := riskLossTrendPoint{
			Date:       d,
			LossLitres: strconv.FormatFloat(a.lossLitres, 'f', 3, 64),
			Events:     a.events,
		}
		if valueShown {
			pt.LossValue = strconv.FormatFloat(a.lossValue, 'f', 2, 64)
		}
		out = append(out, pt)
	}
	return out
}

// riskHeatmap builds the station × risk-type heatmap rows from the scope's
// tallies. With a single station in scope it is one row; the cell counts are
// integer event/alert/investigation tallies (no money), so the front-end derives
// each cell's intensity from the row max.
func riskHeatmap(station string, events, repeated, alerts, investigations int) ([]riskHeatRow, []string) {
	types := []string{"Variance events", "Repeated tanks", "Open alerts", "Open investigations"}
	if strings.TrimSpace(station) == "" {
		station = "This station"
	}
	row := riskHeatRow{
		Station: station,
		Cells: map[string]int{
			"Variance events":     events,
			"Repeated tanks":      repeated,
			"Open alerts":         alerts,
			"Open investigations": investigations,
		},
	}
	return []riskHeatRow{row}, types
}

// patternDistribution turns the §5.11 dimension tallies into a root-cause donut:
// the leading pump/shift/attendant/product concentration, each a slice sized by
// its event count. Falls back to an empty slice when there are no events.
func patternDistribution(in reporting.RiskLossInput) []reporting.DonutDatum {
	out := []reporting.DonutDatum{}
	add := func(prefix string, tallies []reporting.LossDimensionTally) {
		if len(tallies) == 0 || tallies[0].Count == 0 {
			return
		}
		out = append(out, reporting.DonutDatum{
			Key:   prefix + ":" + tallies[0].Key,
			Label: prefix + " " + tallies[0].Label,
			Value: strconv.Itoa(tallies[0].Count),
		})
	}
	add("Pump", in.ByPump)
	add("Shift", in.ByShift)
	add("Attendant", in.ByAttendant)
	add("Product", in.ByProduct)
	return out
}

// alertBoard maps the open-alert severity tallies onto the StatusBoard chips,
// highest severity first, with a semantic tone.
func alertBoard(bySeverity map[string]int) []riskAlertChip {
	order := []struct {
		sev, label, tone string
	}{
		{"critical", "Critical", "at_risk"},
		{"high", "High", "at_risk"},
		{"medium", "Medium", "pending"},
		{"low", "Low", "pending"},
		{"info", "Info", "neutral"},
	}
	out := make([]riskAlertChip, 0, len(order))
	for _, o := range order {
		n := bySeverity[o.sev]
		status := "None"
		tone := "neutral"
		if n > 0 {
			status = strconv.Itoa(n) + " open"
			tone = o.tone
		}
		out = append(out, riskAlertChip{
			Key: o.sev, Label: o.label, Status: status, Tone: tone, Count: n,
			Detail: o.label + " severity alerts",
		})
	}
	return out
}

// investigationTimeline maps the most recent investigation cases onto the
// ShiftTimeline lifecycle: open → investigating → resolved/dismissed. Each case
// is a node whose status drives the timeline glyph (done for resolved/closed,
// failed for dismissed, current for in-progress, pending for newly opened).
func investigationTimeline(cases []risk.Case) []riskInvestigationStep {
	out := []riskInvestigationStep{}
	// Most recent first; cap at a readable number for the timeline card.
	limit := len(cases)
	if limit > 8 {
		limit = 8
	}
	for i := 0; i < limit; i++ {
		c := cases[i]
		out = append(out, riskInvestigationStep{
			Title:  c.Title,
			Status: caseTimelineStatus(c.Status),
			When:   c.CreatedAt.Format(dateLayout),
			Detail: fmt.Sprintf("%s · %s", c.CaseType, c.Status),
		})
	}
	return out
}

// caseTimelineStatus maps an investigation case status onto the ShiftTimeline
// milestone vocabulary (done | current | pending | failed).
func caseTimelineStatus(status string) string {
	switch status {
	case "resolved", "closed":
		return "done"
	case "dismissed":
		return "failed"
	case "assigned", "in_review", "action_required":
		return "current"
	default: // open
		return "pending"
	}
}

// caseTerminal reports whether an investigation case is in a terminal state (so
// it does not count toward "open investigations").
func caseTerminal(status string) bool {
	return status == "closed" || status == "dismissed"
}

// summarizeRiskRules projects the configured rules onto the read-only tuning
// context, and reports whether the fuel-variance rule (the one driving loss
// alerts) is disabled (a data-quality signal). It never rebuilds the CRUD — it
// only summarizes what the GET /risk/rules endpoint already owns.
func summarizeRiskRules(rules []map[string]any) ([]riskRuleSummary, bool) {
	out := make([]riskRuleSummary, 0, len(rules))
	lossRuleDisabled := false
	for _, m := range rules {
		cond := strFromAny(m["condition"])
		enabled, _ := m["enabled"].(bool)
		status := strFromAny(m["status"])
		if cond == "fuel_variance_over_tolerance" && (!enabled || status == "paused" || status == "retired") {
			lossRuleDisabled = true
		}
		out = append(out, riskRuleSummary{
			Code:      strFromAny(m["code"]),
			Name:      strFromAny(m["name"]),
			Condition: cond,
			Severity:  strFromAny(m["severity"]),
			Threshold: strFromAny(m["threshold"]),
			Enabled:   enabled,
			Status:    status,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, lossRuleDisabled
}

// strFromAny renders a rule map value (which may be a *string from the repo) as a
// plain display string.
func strFromAny(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case *string:
		if t == nil {
			return ""
		}
		return *t
	default:
		return ""
	}
}

// canViewRiskIntel reports whether the actor may see the risk.read-gated, tenant
// wide risk intelligence (the cross-station score ranking + highest-risk-station
// KPI). System admins always may; otherwise the actor must hold risk.read — the
// same permission that guards /risk/scores. Fails closed on a policy-load error.
func (s *Server) canViewRiskIntel(ctx context.Context, actor identity.Actor) bool {
	ps, err := s.policy.LoadFor(ctx, actor)
	if err != nil {
		return false
	}
	return ps.IsSystemAdmin || ps.HasPermission("risk.read")
}

// canViewInvestigations reports whether the actor may see investigation cases
// (the tenant-wide open-investigation count + the case-title timeline). System
// admins always may; otherwise the actor must hold investigation.read — the same
// permission that guards /investigations. Fails closed on a policy-load error.
func (s *Server) canViewInvestigations(ctx context.Context, actor identity.Actor) bool {
	ps, err := s.policy.LoadFor(ctx, actor)
	if err != nil {
		return false
	}
	return ps.IsSystemAdmin || ps.HasPermission("investigation.read")
}

// stationRanking builds the station risk-ranking rows from the risk scores,
// resolving each scored station's display name, and returns the rows + the
// highest-risk station's name (the KPI). When the in-scope station is not the
// top, the ranking still includes the full scored set the actor can see (the
// scores are tenant-scoped reads; ranking is a leadership signal).
func stationRanking(ctx context.Context, s *Server, tenantID uuid.UUID, scores []risk.Score, _ uuid.UUID) ([]riskStationRank, string) {
	out := make([]riskStationRank, 0, len(scores))
	highest := ""
	for i := range scores {
		name := scores[i].EntityID.String()[:8]
		if st, err := s.stations.Get(ctx, tenantID, scores[i].EntityID); err == nil && st != nil {
			name = st.Name
		}
		if i == 0 {
			highest = name
		}
		out = append(out, riskStationRank{
			Station: name, Score: scores[i].Score, Band: scores[i].Band, OpenAlerts: scores[i].OpenAlerts,
		})
	}
	return out, highest
}
