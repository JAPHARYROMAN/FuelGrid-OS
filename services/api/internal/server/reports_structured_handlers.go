package server

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/reconciliation"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
)

// Structured, permission-aware report API (REPORTS-STRUCTURED).
//
// These endpoints return the drillable ReportEnvelope (report_envelope.go): a
// headline summary, a report-specific chart payload, a generic table, the
// deterministic insights + data-quality warnings (REUSED verbatim from
// internal/reporting), and the drill-down / export affordances the frontend
// renders. No money or litre figure is recomputed here — every figure is read
// from the SAME repos the dashboards and CSV exports use and carried through as
// an exact decimal string (the insight heuristics parse to float for DISPLAY
// math only, exactly as internal/reporting documents).
//
// Each endpoint is permission-gated at the route (finance.read / revenue.read /
// reconciliation.read as appropriate) and tenant-scoped by the repos; the
// station-scoped ones additionally re-load the URL/query station so an
// out-of-scope station 403s and a cross-tenant one 404s.

// ---- Reports overview ----

// reportCategory is one card on the reports landing page: a category with its
// live headline metric and an alert/data-quality count.
type reportCategory struct {
	Key          string `json:"key"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Headline     string `json:"headline"`      // a live figure (decimal string or count)
	HeadlineUnit string `json:"headline_unit"` // e.g. "TZS", "count"
	AlertCount   int    `json:"alert_count"`   // open risk alerts / DQ flags in this category
	Href         string `json:"href"`          // structured endpoint for the category
}

// reportsOverviewResponse is the reports landing payload.
type reportsOverviewResponse struct {
	GeneratedAt string           `json:"generated_at"`
	Categories  []reportCategory `json:"categories"`
}

// handleReportsOverview returns the report categories, each with a live headline
// metric and an alert/data-quality count. Tenant-wide; gated by finance.read at
// the route. The headline figures are tenant-level rollups read from the same
// services the dashboards use.
func (s *Server) handleReportsOverview(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()

	// Open risk alerts give the alert badge on the loss/reconciliation cards.
	openAlerts := 0
	if alerts, aerr := s.risk.ListAlerts(ctx, actor.TenantID, "open", ""); aerr == nil {
		openAlerts = len(alerts)
	}

	// Tenant receivables exposure for the receivables card headline.
	arTotal := "0"
	arCount := 0
	if rows, rerr := s.receivables.Aging(ctx, actor.TenantID); rerr == nil {
		var sum float64
		for i := range rows {
			if v, ok := parseFloatSafe(rows[i].Balance); ok && v > 0 {
				sum += v
				arCount++
			}
		}
		arTotal = strconv.FormatFloat(sum, 'f', 2, 64)
	}

	out := reportsOverviewResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Categories: []reportCategory{
			{
				Key: "inventory-reconciliation", Title: "Inventory Reconciliation",
				Description: "Per-tank book-vs-physical waterfall, variance, and tolerance breaches.",
				Headline:    strconv.Itoa(openAlerts), HeadlineUnit: "open alerts",
				AlertCount: openAlerts, Href: "/api/v1/reports/inventory/reconciliation",
			},
			{
				Key: "station-close", Title: "Daily Station Close",
				Description: "Sales, stock variance, cash position, deliveries, and approval status for a day.",
				Headline:    "", HeadlineUnit: "",
				AlertCount: 0, Href: "/api/v1/reports/station-close",
			},
			{
				Key: "cash-reconciliation", Title: "Cash Reconciliation",
				Description: "Expected vs submitted vs deposited cash, shortages and excesses by shift.",
				Headline:    "", HeadlineUnit: "",
				AlertCount: 0, Href: "/api/v1/reports/cash-reconciliation",
			},
			{
				Key: "fuel-loss", Title: "Fuel Loss",
				Description: "Loss litres and value, variance %, repeated incidents, and loss patterns.",
				Headline:    strconv.Itoa(openAlerts), HeadlineUnit: "open alerts",
				AlertCount: openAlerts, Href: "/api/v1/reports/fuel-loss",
			},
			{
				Key: "receivables", Title: "Receivables Aging",
				Description: "Outstanding credit-customer balances and concentration risk.",
				Headline:    arTotal, HeadlineUnit: "TZS",
				AlertCount: 0, Href: "/api/v1/reports/customer-aging/insights",
			},
			{
				Key: "profitability", Title: "Profitability",
				Description: "Revenue, COGS, gross margin, expenses and net operating result by station and product.",
				Headline:    "", HeadlineUnit: "",
				AlertCount: 0, Href: "/api/v1/reports/profitability",
			},
			{
				Key: "station-comparison", Title: "Station Comparison",
				Description: "Per-station ranking by revenue, litres, margin, stock variance, expenses, risk alerts and collections.",
				Headline:    "", HeadlineUnit: "",
				AlertCount: openAlerts, Href: "/api/v1/reports/station-comparison",
			},
			{
				Key: "credit-cashflow", Title: "Credit & Cashflow",
				Description: "Sales by tender, collections, outstanding and overdue receivables, supplier payments, cash variance and projected cash position.",
				Headline:    arTotal, HeadlineUnit: "TZS",
				AlertCount: 0, Href: "/api/v1/reports/credit-cashflow",
			},
		},
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- Inventory reconciliation report ----

// handleReconciliationReport returns the per-tank reconciliation waterfall for a
// station's day as a ReportEnvelope. Station-scoped: gated by reconciliation.read
// for the ?station_id at the route group, plus an in-handler authorizeStation so
// an out-of-scope station 403s and a cross-tenant one 404s. ?period is accepted
// for forward-compatibility (the report always resolves the latest active day or
// an explicit ?operating_day_id).
func (s *Server) handleReconciliationReport(w http.ResponseWriter, r *http.Request) {
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
	env := newEnvelope("inventory-reconciliation", "Inventory Reconciliation", period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["period"] = period

	dayID, businessDate, ok := s.resolveReportDay(w, r, actor, stationID)
	if !ok {
		return
	}
	env.FiltersUsed["business_date"] = businessDate

	var recs []reconLine
	if dayID != uuid.Nil {
		raw, rerr := s.reconciliation.ListForStationDayWithProduct(ctx, actor.TenantID, stationID, dayID)
		if rerr != nil {
			s.logger.Error("recon report: list", "error", rerr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for i := range raw {
			recs = append(recs, reconLineFromStationDay(raw[i]))
		}
	}

	// Physical dips tell us which tanks are book-only (a data-quality signal).
	dips, _ := s.readings.LatestDipsForStation(ctx, actor.TenantID, stationID)

	// The signature layout's chart payload carries BOTH the per-tank waterfall
	// rows (the centerpiece visual) AND a variance heatmap cell per tank/product,
	// so the page renders the waterfall and the over-tolerance heatmap from one
	// envelope. The heatmap cell keeps the signed variance %, the tolerance, the
	// over-tolerance flag (text + color, never color alone), the priced variance
	// value, and the product identity.
	env.Table.Columns = []string{
		"tank", "product", "opening", "deliveries", "sales", "adjustments",
		"expected_closing", "actual_closing", "variance", "variance_pct", "variance_value",
		"tolerance", "over_tolerance", "sealed",
	}
	type chartTank struct {
		Tank            string `json:"tank"`
		Product         string `json:"product"`
		ProductColor    string `json:"product_color"`
		Opening         string `json:"opening"`
		Deliveries      string `json:"deliveries"`
		Sales           string `json:"sales"`
		Adjustments     string `json:"adjustments"`
		ExpectedClosing string `json:"expected_closing"`
		ActualClosing   string `json:"actual_closing"`
		Variance        string `json:"variance"`
		VariancePct     string `json:"variance_pct"`
		VarianceValue   string `json:"variance_value"`
		Priced          bool   `json:"priced"`
		Tolerance       string `json:"tolerance"`
		OverTolerance   bool   `json:"over_tolerance"`
		Sealed          bool   `json:"sealed"`
	}
	chart := make([]chartTank, 0, len(recs))
	var reconIn reporting.StockReconInput
	reconIn.AllShiftsClosed = true
	if dayID != uuid.Nil {
		if n, nerr := s.operations.UnapprovedShiftCountForDay(ctx, actor.TenantID, dayID); nerr == nil {
			reconIn.AllShiftsClosed = n == 0
		}
	}
	// KPI-hero aggregates, all summed in float for the DISPLAY headline only (the
	// per-line decimal strings are never mutated): the net signed variance litres
	// and its absolute total, the over-tolerance count, and the priced variance
	// value (only when every contributing tank is priced, so the figure is honest).
	var exceptions, missingDips, pricedTanks int
	var netVarianceLitres, absVarianceLitres, totalExpected, varianceValue float64
	for i := range recs {
		rc := recs[i]
		sealed := rc.Status == "sealed"
		_, hasDip := dips[rc.TankID]
		if !hasDip {
			missingDips++
		}
		over := rc.Status == "exception" || overTolerance(rc.VarianceLitres, rc.ExpectedClosing, rc.TolerancePercent)
		if over {
			exceptions++
		}
		litreVariance, hasLitre := parseFloatSafe(rc.VarianceLitres)
		if hasLitre {
			netVarianceLitres += litreVariance
			absVarianceLitres += math.Abs(litreVariance)
		}
		if e, ok := parseFloatSafe(rc.ExpectedClosing); ok {
			totalExpected += math.Abs(e)
		}
		if rc.Priced {
			pricedTanks++
			// VarianceValue is unsigned from SQL (|litres| × price). The KPI sums
			// the NET signed monetary variance so it agrees in direction with the
			// signed "Total variance" litres beside it (a shortage subtracts) — an
			// absolute sum would read shortages and excesses with the same sign.
			if vv, ok := parseFloatSafe(rc.VarianceValue); ok {
				if hasLitre && litreVariance < 0 {
					varianceValue -= vv
				} else {
					varianceValue += vv
				}
			}
		}
		env.Table.Rows = append(env.Table.Rows, []string{
			rc.TankLabel, rc.ProductName, rc.OpeningBook, rc.DeliveriesTotal, rc.SalesTotal,
			rc.AdjustmentsTotal, rc.ExpectedClosing, rc.ClosingPhysical, rc.VarianceLitres,
			rc.VariancePercent, rc.VarianceValue, rc.TolerancePercent, strconv.FormatBool(over),
			strconv.FormatBool(sealed),
		})
		chart = append(chart, chartTank{
			Tank: rc.TankLabel, Product: rc.ProductName, ProductColor: rc.ProductColor,
			Opening: rc.OpeningBook, Deliveries: rc.DeliveriesTotal, Sales: rc.SalesTotal,
			Adjustments: rc.AdjustmentsTotal, ExpectedClosing: rc.ExpectedClosing,
			ActualClosing: rc.ClosingPhysical, Variance: rc.VarianceLitres, VariancePct: rc.VariancePercent,
			VarianceValue: rc.VarianceValue, Priced: rc.Priced, Tolerance: rc.TolerancePercent,
			OverTolerance: over, Sealed: sealed,
		})
		reconIn.Tanks = append(reconIn.Tanks, reporting.TankRecon{
			TankLabel:        rc.TankLabel,
			VariancePercent:  rc.VariancePercent,
			TolerancePercent: rc.TolerancePercent,
			Status:           reconStatusForReporting(rc.Status),
			HasPhysicalDip:   hasDip,
		})
	}
	env.ChartData = chart

	// KPI hero (blueprint §20.3): total variance litres, variance %, value, and
	// the over-tolerance tank count. Variance % is the net signed variance over
	// the total expected book volume (a display ratio); the value KPI is the NET
	// signed monetary variance (sign-aligned with the litre figure) and is only
	// surfaced when every reconciled tank is priced, otherwise it would understate.
	env.Summary = []summaryMetric{
		{Label: "Total variance", Value: strconv.FormatFloat(netVarianceLitres, 'f', 3, 64), Unit: "L"},
		{Label: "Variance %", Value: overallVariancePct(netVarianceLitres, totalExpected)},
		{Label: "Over-tolerance tanks", Value: strconv.Itoa(exceptions), Unit: "count"},
		{Label: "Tanks reconciled", Value: strconv.Itoa(len(recs)), Unit: "count"},
	}
	if len(recs) > 0 && pricedTanks == len(recs) {
		env.Summary = append(env.Summary, summaryMetric{
			Label: "Variance value", Value: strconv.FormatFloat(varianceValue, 'f', 2, 64), Unit: "TZS",
		})
	}
	env.applyReport(reporting.StockReconciliation(reconIn))

	// Harden data-quality beyond the composer: an empty day (no reconciliations)
	// reads honestly, and an unpriced tank means the variance VALUE is incomplete.
	if len(recs) == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "warning",
			Message: "No tanks have been reconciled for this station's active day yet — reconciliation figures are unavailable.",
		})
	} else if pricedTanks < len(recs) {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "warning",
			Message: fmt.Sprintf("%d of %d tank(s) have no product price — the variance value is omitted until prices are set.", len(recs)-pricedTanks, len(recs)),
		})
	}
	_ = missingDips // the composer raises the missing-dip data-quality warning.

	// Drilldown into the underlying readings/adjustments (blueprint §20.3): the
	// per-station reconciliation overview (variance history + closing dips) and
	// the inventory overview (the live book balances + adjustment movements).
	env.Drilldown = []drilldownLink{
		{Label: "Reconciliation console", Href: fmt.Sprintf("/api/v1/stations/%s/reconciliation-overview", sid)},
		{Label: "Inventory overview", Href: fmt.Sprintf("/api/v1/stations/%s/inventory-overview", sid)},
	}
	env.ExportOptions = []exportOption{
		{Format: "csv", URL: fmt.Sprintf("/api/v1/stations/%s/reports/reconciliation.csv", sid)},
		{Format: "xlsx", URL: fmt.Sprintf("/api/v1/stations/%s/reports/reconciliation.xlsx", sid)},
	}
	writeJSON(w, http.StatusOK, env)
}

// overallVariancePct is the day's net signed variance as a percent of the total
// expected book volume — a DISPLAY ratio over the already-computed decimal
// figures (parsed to float for the headline only). Returns "0" when there is no
// expected volume to divide by.
func overallVariancePct(netVariance, totalExpected float64) string {
	if totalExpected == 0 {
		return "0"
	}
	return strconv.FormatFloat(netVariance/totalExpected*100, 'f', 2, 64)
}

// ---- Daily station close report ----

// handleStationCloseReport returns the daily station close for a station/date as
// a ReportEnvelope: sales/litres, stock variance, cash expected/submitted,
// deliveries, open exceptions, and approval status. Station-scoped, gated by
// revenue.read. ?date is accepted (the period label); the figures come from the
// latest revenue day (or the day matching ?operating_day_id).
func (s *Server) handleStationCloseReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, ok := s.resolveStationScoped(w, r, actor, "revenue.read")
	if !ok {
		return
	}
	ctx := r.Context()
	dateParam := r.URL.Query().Get("date")
	sid := stationID.String()
	env := newEnvelope("station-close", "Daily Station Close", dateParam, &sid)
	env.FiltersUsed["station_id"] = sid
	if dateParam != "" {
		env.FiltersUsed["date"] = dateParam
	}

	pts, latestLocked, perr := s.loadRevenuePoints(ctx, actor.TenantID, stationID)
	if perr != nil {
		s.logger.Error("station-close report: revenue points", "error", perr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	days, derr := s.revenue.RecentDays(ctx, actor.TenantID, stationID, 30)
	if derr != nil {
		s.logger.Error("station-close report: recent days", "error", derr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	cashVariance := s.latestCashVariance(ctx, actor.TenantID, stationID)
	unclosed := s.unclosedShiftCount(ctx, actor.TenantID, stationID)

	// Headline day: the latest revenue day on record (RecentDays is newest-first).
	if len(days) > 0 {
		d := days[0]
		env.FiltersUsed["business_date"] = d.BusinessDate.Format(dateLayout)
		// Reconcile the SAME day's cash: resolve the cash reconciliation for THIS
		// revenue day's operating day, and surface its OWN expected/counted/variance
		// — never the recorded tender plus a global-latest recon's variance (those
		// can belong to two different business days, yielding a "submitted" figure
		// that existed for neither). Expected cash is the recon's seeded expected
		// when a recon exists, else the day's recorded cash tender; submitted and
		// variance are shown only once the drawer has actually been counted
		// (submitted/approved/posted), so a draft recon never fabricates them.
		recon := s.cashReconForDay(ctx, actor.TenantID, stationID, d.OperatingDayID)
		expectedCash := d.CashTotal
		submittedCash := "not submitted"
		varianceDisplay := d.CashVariance
		if recon != nil {
			expectedCash = recon.ExpectedCash
			if cashRowCounted(recon.Status) {
				submittedCash = recon.CountedCash
				varianceDisplay = recon.Variance
			}
		}
		env.Summary = []summaryMetric{
			{Label: "Sales value", Value: d.GrossRevenue, Unit: "TZS"},
			{Label: "Net revenue", Value: d.NetRevenue, Unit: "TZS"},
			{Label: "Margin", Value: d.MarginTotal, Unit: "TZS"},
			{Label: "Total tendered", Value: d.TenderTotal, Unit: "TZS"},
			{Label: "Expected cash", Value: expectedCash, Unit: "TZS"},
			{Label: "Submitted cash", Value: submittedCash, Unit: "TZS"},
			{Label: "Cash variance", Value: varianceDisplay, Unit: "TZS"},
			{Label: "Open exceptions", Value: strconv.Itoa(unclosed), Unit: "count"},
			{Label: "Approval status", Value: dayApprovalStatus(d.Status, unclosed)},
		}
		// Additive tender-mix breakdown (cash / mobile-money / card / credit /
		// voucher) read straight from the revenue_days rollup — decimal strings,
		// no recompute. Powers the signature donut.
		env.TenderMix = &tenderMix{
			Cash:        d.CashTotal,
			MobileMoney: d.MobileMoneyTotal,
			Card:        d.CardTotal,
			Credit:      d.CreditTotal,
			Voucher:     d.VoucherTotal,
			Total:       d.TenderTotal,
		}
	} else {
		env.Summary = []summaryMetric{
			{Label: "Approval status", Value: "no_data"},
			{Label: "Open exceptions", Value: strconv.Itoa(unclosed), Unit: "count"},
		}
	}

	// Trend table + chart over recent days (chronological for the chart).
	env.Table.Columns = []string{
		"business_date", "status", "gross", "net", "margin", "tendered", "cash_variance",
	}
	type chartDay struct {
		Date         string `json:"date"`
		Gross        string `json:"gross"`
		Margin       string `json:"margin"`
		Tendered     string `json:"tendered"`
		CashVariance string `json:"cash_variance"`
	}
	chart := make([]chartDay, 0, len(days))
	for i := len(days) - 1; i >= 0; i-- {
		d := days[i]
		chart = append(chart, chartDay{
			Date: d.BusinessDate.Format(dateLayout), Gross: d.GrossRevenue,
			Margin: d.MarginTotal, Tendered: d.TenderTotal, CashVariance: d.CashVariance,
		})
	}
	for i := range days {
		d := days[i]
		env.Table.Rows = append(env.Table.Rows, []string{
			d.BusinessDate.Format(dateLayout), d.Status, d.GrossRevenue, d.NetRevenue,
			d.MarginTotal, d.TenderTotal, d.CashVariance,
		})
	}
	env.ChartData = chart

	grossPts := grossSeries(pts, func(p reportingRevenuePoint) string { return p.gross })
	env.applyReport(reporting.DailyClose(reporting.DailyCloseInput{
		GrossSeries: grossPts, CashVariance: cashVariance,
		UnclosedShiftCount: unclosed, DayLocked: latestLocked,
	}))

	// Harden data-quality for the close-specific gaps the composer does not see:
	// no revenue day at all, and no cash reconciliation submitted for the day.
	// These are surfaced prominently so the report never reads as final when the
	// close is incomplete.
	if len(days) == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "warning",
			Message: "No revenue day has been computed for this station yet — close figures are unavailable.",
		})
	} else if cashVariance == "" {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "warning",
			Message: "Cash has not been submitted/reconciled for this station yet — the cash position is unverified.",
		})
	}

	env.Drilldown = []drilldownLink{
		{Label: "Operations overview", Href: fmt.Sprintf("/api/v1/stations/%s/operations/overview", sid)},
		{Label: "Reconciliation report", Href: fmt.Sprintf("/api/v1/reports/inventory/reconciliation?station_id=%s", sid)},
		{Label: "Cash reconciliation report", Href: fmt.Sprintf("/api/v1/reports/cash-reconciliation?station_id=%s", sid)},
	}
	env.ExportOptions = []exportOption{
		{Format: "csv", URL: fmt.Sprintf("/api/v1/stations/%s/reports/revenue.csv", sid)},
		{Format: "pdf", URL: fmt.Sprintf("/api/v1/stations/%s/reports/daily-close.pdf", sid)},
		{Format: "xlsx", URL: fmt.Sprintf("/api/v1/stations/%s/reports/revenue.xlsx", sid)},
	}
	writeJSON(w, http.StatusOK, env)
}

// ---- Cash reconciliation report ----

// handleCashReconciliationReport returns the station's cash position as a
// ReportEnvelope: expected vs submitted (counted) vs the resulting variance,
// broken down by reconciliation (day). Station-scoped, gated by finance.read.
func (s *Server) handleCashReconciliationReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, ok := s.resolveStationScoped(w, r, actor, "finance.read")
	if !ok {
		return
	}
	ctx := r.Context()
	period := reportPeriodParam(r)
	sid := stationID.String()
	env := newEnvelope("cash-reconciliation", "Cash Reconciliation", period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["period"] = period

	recons, rerr := s.banking.ListCashReconciliations(ctx, actor.TenantID, stationID)
	if rerr != nil {
		s.logger.Error("cash report: list", "error", rerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Bank deposits give the "deposited" leg of the cash flow (a deposit is only
	// settled once its status is 'posted'). Never fatal to the report — if the
	// deposits read fails we degrade to a no-deposit picture + a DQ note.
	deposits, derr := s.banking.ListDeposits(ctx, actor.TenantID, stationID)
	if derr != nil {
		s.logger.Error("cash report: deposits", "error", derr)
		deposits = nil
	}

	// Per-reconciliation flow rows (expected -> submitted -> variance) drive the
	// drillable table + the §20.5 cash-flow bar. Figures stay decimal strings;
	// the float coercion below is for the display-only headline aggregates, never
	// fed back into a persisted figure.
	env.Table.Columns = []string{"created_at", "status", "expected", "submitted", "variance", "shortage", "excess"}
	type flowRow struct {
		CreatedAt string `json:"created_at"`
		Status    string `json:"status"`
		Expected  string `json:"expected"`
		Submitted string `json:"submitted"`
		Variance  string `json:"variance"`
		Shortage  string `json:"shortage"`
		Excess    string `json:"excess"`
	}
	flow := make([]flowRow, 0, len(recons))
	var totalExpected, totalSubmitted, totalShortage, totalExcess float64
	var anyPosted, anyUnsubmitted bool
	for i := range recons {
		c := recons[i]
		// A drawer is only COUNTED once the recon is submitted/approved/posted; a
		// draft/rejected recon seeds expected_cash from cash tenders but leaves
		// counted_cash/variance at their 0 default (migration 0042). Folding a
		// draft's seeded expected (with counted 0, variance 0) into the headline
		// aggregates makes "Net variance" read as a huge shortage while
		// "Variance status"/shortage/excess (driven by per-row variance) stay
		// balanced — three contradictory figures. So expected/submitted/variance
		// only accumulate for counted recons; draft expected stays in the
		// per-row table/flow (shown as-is) but never in the hero totals.
		counted := cashRowCounted(c.Status)
		variance := c.Variance
		vf, _ := parseFloatSafe(variance)
		shortage, excess := "0", "0"
		if counted {
			if vf < 0 {
				shortage = strconv.FormatFloat(math.Abs(vf), 'f', 2, 64)
				totalShortage += math.Abs(vf)
			} else if vf > 0 {
				excess = strconv.FormatFloat(vf, 'f', 2, 64)
				totalExcess += vf
			}
			if ev, ok := parseFloatSafe(c.ExpectedCash); ok {
				totalExpected += ev
			}
			if cv, ok := parseFloatSafe(c.CountedCash); ok {
				totalSubmitted += cv
			}
		}
		switch c.Status {
		case "posted":
			anyPosted = true
		case "draft", "rejected":
			anyUnsubmitted = true
		}
		env.Table.Rows = append(env.Table.Rows, []string{
			c.CreatedAt.Format(time.RFC3339), c.Status, c.ExpectedCash, c.CountedCash,
			variance, shortage, excess,
		})
		flow = append(flow, flowRow{
			CreatedAt: c.CreatedAt.Format(time.RFC3339), Status: c.Status,
			Expected: c.ExpectedCash, Submitted: c.CountedCash, Variance: variance,
			Shortage: shortage, Excess: excess,
		})
	}

	// Deposited cash = bank deposits the bank has already received. Per the
	// 0043 lifecycle (draft -> prepared -> in_transit -> confirmed -> posted),
	// funds move into the bank account at CONFIRMATION; 'posted' only adds the GL
	// entry. So both 'confirmed' and 'posted' are banked. The genuine at-risk set
	// — cash prepared but not yet banked — is draft/prepared/in_transit only;
	// counting 'confirmed' as pending would overstate risk on cash the bank has
	// already acknowledged.
	var totalDeposited, pendingDeposited float64
	var bankedDeposits, pendingDeposits int
	for i := range deposits {
		d := deposits[i]
		amt, _ := parseFloatSafe(d.Amount)
		switch d.Status {
		case "confirmed", "posted":
			totalDeposited += amt
			bankedDeposits++
		case "draft", "prepared", "in_transit":
			pendingDeposited += amt
			pendingDeposits++
		}
		// 'voided' deposits are ignored (neither banked nor at risk).
	}

	// KPI hero (§20.5): expected, submitted, deposited, variance, and the
	// shortage/excess status word. Variance = submitted − expected over the
	// window; the status reads short / over / balanced for the headline.
	netVariance := totalSubmitted - totalExpected
	varStatus := "Balanced"
	if totalShortage > totalExcess {
		varStatus = "Shortage"
	} else if totalExcess > totalShortage {
		varStatus = "Excess"
	}
	env.Summary = []summaryMetric{
		{Label: "Expected cash", Value: strconv.FormatFloat(totalExpected, 'f', 2, 64), Unit: "TZS"},
		{Label: "Submitted cash", Value: strconv.FormatFloat(totalSubmitted, 'f', 2, 64), Unit: "TZS"},
		{Label: "Deposited cash", Value: strconv.FormatFloat(totalDeposited, 'f', 2, 64), Unit: "TZS"},
		{Label: "Net variance", Value: strconv.FormatFloat(netVariance, 'f', 2, 64), Unit: "TZS"},
		{Label: "Total shortage", Value: strconv.FormatFloat(totalShortage, 'f', 2, 64), Unit: "TZS"},
		{Label: "Total excess", Value: strconv.FormatFloat(totalExcess, 'f', 2, 64), Unit: "TZS"},
		{Label: "Variance status", Value: varStatus},
		{Label: "Reconciliations", Value: strconv.Itoa(len(recons)), Unit: "count"},
	}

	// Recent revenue days drive (a) the latest-day tender-split donut and (b) the
	// mobile-money/card settlement totals. Read straight from the revenue_days
	// rollup (decimal strings, no recompute). The mm/card chips must cover the
	// SAME window as the period-wide cash and deposit chips, otherwise the board
	// puts an all-time cash figure beside a single-day mobile-money figure and a
	// viewer reads them as the same period; so we sum mm/card over every day in
	// the window, not just the latest. The donut still shows the latest day's mix
	// (its own labelled context), and the latest day tells us if it's locked.
	pts, latestLocked, _ := s.loadRevenuePoints(ctx, actor.TenantID, stationID)
	cashPts := grossSeries(pts, func(p reportingRevenuePoint) string { return p.cash })
	var mmTotal, cardTotal float64
	if days, derr := s.revenue.RecentDays(ctx, actor.TenantID, stationID, 30); derr == nil && len(days) > 0 {
		latest := days[0]
		env.TenderMix = &tenderMix{
			Cash: latest.CashTotal, MobileMoney: latest.MobileMoneyTotal, Card: latest.CardTotal,
			Credit: latest.CreditTotal, Voucher: latest.VoucherTotal, Total: latest.TenderTotal,
		}
		for i := range days {
			if v, ok := parseFloatSafe(days[i].MobileMoneyTotal); ok {
				mmTotal += v
			}
			if v, ok := parseFloatSafe(days[i].CardTotal); ok {
				cardTotal += v
			}
		}
	}

	// Settlement-status board (§20.5, net-new): cash / mobile-money / card / bank
	// deposit chips, each settled or pending derived deterministically from the
	// real domain state (no settlement-batch table exists, so the honest signal
	// is the cash-reconciliation/deposit lifecycle + the day-lock state). Every
	// chip covers the SAME period window (cash submitted, mm/card tendered, and
	// deposits banked over the recent days), so the figures are on one scale. The
	// amounts are exact decimal strings; the tone/status are text, never
	// colour-alone, so the front-end board reads accessibly.
	settlement := []settlementChip{
		cashSettlementChip(anyPosted, anyUnsubmitted, len(recons), strconv.FormatFloat(totalSubmitted, 'f', 2, 64)),
		mediumSettlementChip("mobile_money", "Mobile money", mmTotal, latestLocked),
		mediumSettlementChip("card", "Card", cardTotal, latestLocked),
		depositSettlementChip(bankedDeposits, pendingDeposits,
			strconv.FormatFloat(totalDeposited, 'f', 2, 64),
			strconv.FormatFloat(pendingDeposited, 'f', 2, 64)),
	}

	env.ChartData = struct {
		Flow       []flowRow        `json:"flow"`
		Settlement []settlementChip `json:"settlement"`
	}{Flow: flow, Settlement: settlement}

	// Latest variance feeds the reusable cash-recon insight composer.
	latestVariance := ""
	if len(recons) > 0 {
		latestVariance = recons[0].Variance
	}
	env.applyReport(reporting.CashReconciliation(reporting.CashReconInput{
		Variance: latestVariance, GrossSeries: cashPts, PeriodLocked: latestLocked,
	}))

	// Harden data-quality for the §20.5 gaps the composer does not see: no cash
	// submitted, mobile-money/card settlement still pending (day unlocked while
	// non-cash tenders exist), and deposits prepared-but-not-posted. Each tempers
	// the headline so the report never reads as final while money is in flight.
	if len(recons) == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "warning",
			Message: "No cash reconciliation has been recorded for this station yet — the cash position is unverified.",
		})
	} else if anyUnsubmitted {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "warning",
			Message: "Cash has not been submitted for every reconciliation — figures may change once the drawer is counted.",
		})
	}
	if !latestLocked && (mmTotal > 0 || cardTotal > 0) {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "warning",
			Message: "Mobile-money/card settlement is pending — the operating day is not locked, so non-cash tenders are unconfirmed.",
		})
	}
	if pendingDeposits > 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "warning",
			Message: fmt.Sprintf("%d bank deposit(s) are prepared but not yet banked — the bank has not received this cash.", pendingDeposits),
		})
	}

	// Drill-down targets point at REGISTERED routes (server_routes.go): the
	// station-scoped cash-reconciliations list and the bank-deposits list filtered
	// by station. (There is no station-scoped collection-receipts route — receipts
	// are shift-scoped — so it is dropped rather than linking a 404.) The
	// operations overview keeps the cross-report drilldown convention.
	env.Drilldown = []drilldownLink{
		{Label: "Cash reconciliations", Href: fmt.Sprintf("/api/v1/stations/%s/cash-reconciliations", sid)},
		{Label: "Bank deposits", Href: fmt.Sprintf("/api/v1/bank-deposits?station_id=%s", sid)},
		{Label: "Operations overview", Href: fmt.Sprintf("/api/v1/stations/%s/operations/overview", sid)},
	}
	writeJSON(w, http.StatusOK, env)
}

// settlementChip is one medium on the Cash Reconciliation settlement-status
// board (§20.5): a payment medium with its settled/pending status as TEXT, a
// semantic tone, and the exact decimal-string amount. Carried in the cash
// report's chart_data (the envelope wire shape is unchanged — chart_data is
// generic), it powers the reusable @fuelgrid/ui StatusBoard.
type settlementChip struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Tone   string `json:"tone"` // settled | pending | at_risk | neutral
	Amount string `json:"amount"`
	Detail string `json:"detail"`
}

// cashRowCounted reports whether a cash reconciliation's drawer has actually
// been counted — true once it leaves draft/rejected for submitted/approved/
// posted. Only counted recons carry a real counted_cash/variance (a draft seeds
// only expected_cash), so only they may feed the expected/submitted/variance
// headline aggregates.
func cashRowCounted(status string) bool {
	switch status {
	case "submitted", "approved", "posted":
		return true
	default:
		return false
	}
}

// cashSettlementChip describes the cash medium's settlement state from the
// reconciliation lifecycle: posted reconciliations are settled, unsubmitted
// drawers are pending, an all-submitted-but-not-posted state is "submitted".
func cashSettlementChip(anyPosted, anyUnsubmitted bool, count int, submitted string) settlementChip {
	chip := settlementChip{Key: "cash", Label: "Cash", Amount: submitted}
	switch {
	case count == 0:
		chip.Status, chip.Tone, chip.Detail = "No data", "neutral", "No cash reconciliation recorded"
	case anyUnsubmitted:
		chip.Status, chip.Tone, chip.Detail = "Pending", "pending", "Drawer not yet counted/submitted"
	case anyPosted:
		chip.Status, chip.Tone, chip.Detail = "Settled", "settled", "Reconciliation posted to the ledger"
	default:
		chip.Status, chip.Tone, chip.Detail = "Submitted", "pending", "Submitted, awaiting approval/posting"
	}
	return chip
}

// mediumSettlementChip describes a non-cash tender medium (mobile money / card),
// with the amount tendered over the report window. With no settlement-batch
// table, the honest settlement signal is the latest day's lock state: a locked
// latest day means non-cash tenders are confirmed/settled; an unlocked latest
// day with recorded tenders is pending. A zero medium reads as "None".
func mediumSettlementChip(key, label string, total float64, latestDayLocked bool) settlementChip {
	amt := strconv.FormatFloat(total, 'f', 2, 64)
	switch {
	case total <= 0:
		return settlementChip{
			Key: key, Label: label, Status: "None", Tone: "neutral", Amount: amt,
			Detail: "No " + label + " tendered",
		}
	case latestDayLocked:
		return settlementChip{
			Key: key, Label: label, Status: "Settled", Tone: "settled", Amount: amt,
			Detail: "Latest day locked — tenders confirmed",
		}
	default:
		return settlementChip{
			Key: key, Label: label, Status: "Pending", Tone: "pending", Amount: amt,
			Detail: "Latest day not locked — awaiting settlement",
		}
	}
}

// depositSettlementChip describes the bank-deposit medium: banked deposits
// (confirmed/posted — the bank has received the funds) are settled; deposits
// still in draft/prepared/in_transit are at risk (prepared but not yet banked).
func depositSettlementChip(banked, pending int, bankedAmt, pendingAmt string) settlementChip {
	switch {
	case pending > 0:
		return settlementChip{
			Key: "bank_deposit", Label: "Bank deposit", Status: "Not banked", Tone: "at_risk",
			Amount: pendingAmt, Detail: fmt.Sprintf("%d deposit(s) prepared, not yet banked", pending),
		}
	case banked > 0:
		return settlementChip{
			Key: "bank_deposit", Label: "Bank deposit", Status: "Banked", Tone: "settled",
			Amount: bankedAmt, Detail: fmt.Sprintf("%d deposit(s) received by the bank", banked),
		}
	default:
		return settlementChip{
			Key: "bank_deposit", Label: "Bank deposit", Status: "None", Tone: "neutral",
			Amount: "0", Detail: "No bank deposit recorded",
		}
	}
}

// ---- Fuel loss report ----

// handleFuelLossReport returns the station's fuel-loss picture as a
// ReportEnvelope: loss litres and value, variance %, repeated-incident count,
// and a simple pattern summary, derived from the reconciliation variance history
// and open risk alerts. Station-scoped, gated by reconciliation.read.
func (s *Server) handleFuelLossReport(w http.ResponseWriter, r *http.Request) {
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
	env := newEnvelope("fuel-loss", "Fuel Loss", period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["period"] = period

	tankRows, terr := s.tanks.List(ctx, actor.TenantID, []uuid.UUID{stationID})
	if terr != nil {
		s.logger.Error("fuel-loss report: tanks", "error", terr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	env.Table.Columns = []string{"tank", "business_date", "variance_litres", "variance_pct", "tolerance_pct", "over_tolerance", "status"}
	type lossPoint struct {
		Tank           string `json:"tank"`
		BusinessDate   string `json:"business_date"`
		VarianceLitres string `json:"variance_litres"`
		VariancePct    string `json:"variance_pct"`
	}
	chart := make([]lossPoint, 0)

	var lossLitres float64 // sum of negative (loss) variances, in litres
	var repeated int       // tanks with 2+ over-tolerance reconciliations
	var breaches int
	for i := range tankRows {
		tank := tankRows[i]
		recent, rerr := s.reconciliation.RecentForTank(ctx, actor.TenantID, tank.ID, 30)
		if rerr != nil {
			s.logger.Error("fuel-loss report: recent for tank", "error", rerr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		tankBreaches := 0
		for j := range recent {
			rr := recent[j]
			over := overTolerance(rr.VarianceLitres, rr.ClosingBook, rr.TolerancePercent)
			if over {
				tankBreaches++
				breaches++
			}
			if vf, okv := parseFloatSafe(rr.VarianceLitres); okv && vf < 0 {
				lossLitres += math.Abs(vf)
			}
			env.Table.Rows = append(env.Table.Rows, []string{
				tank.Code, rr.BusinessDate.Format(dateLayout), rr.VarianceLitres,
				rr.VariancePercent, rr.TolerancePercent, strconv.FormatBool(over), rr.Status,
			})
			chart = append(chart, lossPoint{
				Tank: tank.Code, BusinessDate: rr.BusinessDate.Format(dateLayout),
				VarianceLitres: rr.VarianceLitres, VariancePct: rr.VariancePercent,
			})
		}
		if tankBreaches >= 2 {
			repeated++
		}
	}
	env.ChartData = chart

	// Open risk alerts for the station give the incident/pattern context.
	openAlerts := 0
	if alerts, aerr := s.risk.ListAlerts(ctx, actor.TenantID, "open", ""); aerr == nil {
		for i := range alerts {
			if alerts[i].StationID != nil && *alerts[i].StationID == stationID {
				openAlerts++
			}
		}
	}

	env.Summary = []summaryMetric{
		{Label: "Total loss litres", Value: strconv.FormatFloat(lossLitres, 'f', 3, 64), Unit: "L"},
		{Label: "Tolerance breaches", Value: strconv.Itoa(breaches), Unit: "count"},
		{Label: "Repeated-incident tanks", Value: strconv.Itoa(repeated), Unit: "count"},
		{Label: "Open risk alerts", Value: strconv.Itoa(openAlerts), Unit: "count"},
	}

	// Simple, transparent pattern summary + DQ.
	if repeated > 0 {
		env.Insights = append(env.Insights, reporting.Insight{
			Severity:          reporting.SeverityWarning,
			Message:           fmt.Sprintf("%d tank(s) breached tolerance on 2 or more days — a recurring loss pattern.", repeated),
			RecommendedAction: "Open a loss investigation for the repeating tanks and check meter calibration.",
		})
		env.RecommendedActions = append(env.RecommendedActions,
			"Open a loss investigation for the repeating tanks and check meter calibration.")
	} else if breaches > 0 {
		env.Insights = append(env.Insights, reporting.Insight{
			Severity: reporting.SeverityInfo,
			Message:  fmt.Sprintf("%d tolerance breach(es) recorded, but no tank repeated — likely isolated events.", breaches),
		})
	}
	if len(env.Table.Rows) == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level: "warning", Message: "No reconciliations recorded for this station yet — loss figures are unavailable.",
		})
	}

	env.Drilldown = []drilldownLink{
		{Label: "Risk alerts", Href: "/api/v1/risk/alerts?status=open"},
		{Label: "Reconciliation report", Href: fmt.Sprintf("/api/v1/reports/inventory/reconciliation?station_id=%s", sid)},
		{Label: "Inventory overview", Href: fmt.Sprintf("/api/v1/stations/%s/inventory/overview", sid)},
	}
	writeJSON(w, http.StatusOK, env)
}

// ---- Unified export ----

// exportReportRequest is the unified export body.
type exportReportRequest struct {
	ReportKey string            `json:"report_key"`
	Format    string            `json:"format"`
	Filters   map[string]string `json:"filters"`
}

// exportReportResponse returns the same-origin URL of the existing export
// endpoint the request maps to, so the BFF can stream the file. The act of
// requesting an export is itself audited (action 'report.exported').
type exportReportResponse struct {
	ReportKey string `json:"report_key"`
	Format    string `json:"format"`
	URL       string `json:"url"`
}

// handleExportReport unifies the export entry point: it validates the
// {report_key, format, filters}, audits the request, and DELEGATES to the
// existing CSV/PDF/XLSX export endpoint by returning its URL (the existing
// endpoints remain mounted and authoritative for the actual file bytes + their
// own per-station permission gate). Gated by finance.read at the route; the
// downstream file endpoint re-checks the station/finance permission when fetched.
func (s *Server) handleExportReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req exportReportRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.ReportKey = strings.TrimSpace(req.ReportKey)
	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	if req.Filters == nil {
		req.Filters = map[string]string{}
	}

	url, ok := buildExportURL(req)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported report_key/format combination")
		return
	}

	// Audit the export request itself, mirroring the file handlers' audit path.
	if !s.auditExportRequest(w, r, actor, req) {
		return
	}
	writeJSON(w, http.StatusOK, exportReportResponse{
		ReportKey: req.ReportKey, Format: req.Format, URL: url,
	})
}

// buildExportURL maps a {report_key, format, filters} request onto the existing
// export endpoint's same-origin URL. Returns false for an unsupported combo.
func buildExportURL(req exportReportRequest) (string, bool) {
	station := req.Filters["station_id"]
	period := req.Filters["period"]
	dayID := req.Filters["operating_day_id"]
	stationQS := ""
	if dayID != "" {
		stationQS = "?operating_day_id=" + dayID
	}
	periodQS := ""
	if period != "" {
		periodQS = "?period=" + period
	}
	switch req.ReportKey {
	case "revenue", "station-close", "sales":
		// Sales (§5.2) reuses the recognized-sales export endpoints: the revenue
		// CSV/XLSX cover the sale facts and the daily-close PDF is the printable
		// sales summary — the same files the daily-close report streams.
		if station == "" {
			return "", false
		}
		switch req.Format {
		case "csv":
			return fmt.Sprintf("/api/v1/stations/%s/reports/revenue.csv", station), true
		case "xlsx":
			return fmt.Sprintf("/api/v1/stations/%s/reports/revenue.xlsx", station), true
		case "pdf":
			return fmt.Sprintf("/api/v1/stations/%s/reports/daily-close.pdf%s", station, stationQS), true
		}
	case "inventory":
		if station == "" || req.Format != "csv" {
			return "", false
		}
		return fmt.Sprintf("/api/v1/stations/%s/reports/inventory.csv", station), true
	case "reconciliation", "inventory-reconciliation":
		if station == "" {
			return "", false
		}
		switch req.Format {
		case "csv":
			return fmt.Sprintf("/api/v1/stations/%s/reports/reconciliation.csv%s", station, stationQS), true
		case "xlsx":
			return fmt.Sprintf("/api/v1/stations/%s/reports/reconciliation.xlsx%s", station, stationQS), true
		}
	case "financials":
		switch req.Format {
		case "csv":
			return "/api/v1/reports/financials.csv" + periodQS, true
		case "pdf":
			return "/api/v1/reports/financials.pdf" + periodQS, true
		case "xlsx":
			return "/api/v1/reports/financials.xlsx" + periodQS, true
		}
	case "ar-aging", "customer-aging", "receivables":
		if req.Format == "csv" {
			return "/api/v1/reports/ar-aging.csv", true
		}
	}
	return "", false
}

// ---- shared helpers ----

// resolveStationScoped parses the required ?station_id, loads the station within
// the tenant (404 cross-tenant / not found), and runs the in-handler
// authorizeStation check for perm (403 out-of-scope). Returns the station id and
// true on success; otherwise it has written the error.
func (s *Server) resolveStationScoped(w http.ResponseWriter, r *http.Request, actor identity.Actor, perm string) (uuid.UUID, bool) {
	raw := r.URL.Query().Get("station_id")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "station_id is required")
		return uuid.Nil, false
	}
	stationID, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid station_id")
		return uuid.Nil, false
	}
	if _, err := s.stations.Get(r.Context(), actor.TenantID, stationID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "station not found")
		return uuid.Nil, false
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return uuid.Nil, false
	}
	if !s.authorizeStation(w, r, actor, perm, stationID) {
		return uuid.Nil, false
	}
	return stationID, true
}

// resolveReportDay resolves the operating day for a station-scoped report: an
// explicit ?operating_day_id (validated within the tenant), else the latest
// active day. Returns (uuid.Nil, "", true) when there is simply no active day,
// so reports render an empty-but-valid envelope. On a hard error it writes the
// response and returns ok=false.
func (s *Server) resolveReportDay(w http.ResponseWriter, r *http.Request, actor identity.Actor, stationID uuid.UUID) (uuid.UUID, string, bool) {
	ctx := r.Context()
	if raw := r.URL.Query().Get("operating_day_id"); raw != "" {
		dayID, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid operating_day_id")
			return uuid.Nil, "", false
		}
		day, derr := s.operations.GetDay(ctx, actor.TenantID, dayID)
		if errors.Is(derr, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "operating day not found")
			return uuid.Nil, "", false
		}
		if derr != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return uuid.Nil, "", false
		}
		return dayID, day.BusinessDate.Format(dateLayout), true
	}
	day, derr := s.operations.LatestActiveDayForStation(ctx, actor.TenantID, stationID)
	if derr == nil {
		return day.ID, day.BusinessDate.Format(dateLayout), true
	}
	if !errors.Is(derr, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "internal error")
		return uuid.Nil, "", false
	}
	return uuid.Nil, "", true
}

// auditExportRequest records the unified export request as a 'report.exported'
// audit event (mirroring the file handlers' audit path) within a tx. Returns
// false (after writing the error) on failure.
func (s *Server) auditExportRequest(w http.ResponseWriter, r *http.Request, actor identity.Actor, req exportReportRequest) bool {
	exportID := uuid.New()
	newValue := map[string]any{
		"report_type": req.ReportKey, "format": req.Format, "delegated": true,
	}
	for k, v := range req.Filters {
		newValue["filter_"+k] = v
	}
	ctx := r.Context()
	tx, terr := s.deps.DB.Begin(ctx)
	if terr != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := audit.WriteWithOutbox(ctx, tx, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "report.exported", EventType: "ReportExported",
		EntityType: "report_export", EntityID: exportID.String(),
		NewValue:  newValue,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		RequestID: chimiddleware.GetReqID(ctx),
	}); err != nil {
		s.logger.Error("unified export audit", "error", err, "report_key", req.ReportKey)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	return true
}

// reportPeriodParam returns the ?period filter or a default label.
func reportPeriodParam(r *http.Request) string {
	if p := strings.TrimSpace(r.URL.Query().Get("period")); p != "" {
		return p
	}
	return "current"
}

// parseFloatSafe parses a decimal string to float64 for DISPLAY/aggregation math
// only (totals, shortage/excess split). It never feeds a persisted figure.
func parseFloatSafe(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// dayApprovalStatus derives a coarse approval status for the close summary.
func dayApprovalStatus(status string, unclosed int) string {
	if status == "locked" {
		return "approved"
	}
	if unclosed > 0 {
		return "pending_shifts"
	}
	return "draft"
}

// reconStatusForReporting maps a persisted reconciliation status onto the
// reporting package's vocabulary (it only distinguishes over_tolerance).
func reconStatusForReporting(status string) string {
	if status == "exception" {
		return "over_tolerance"
	}
	return "within_tolerance"
}

// reconLine is a local projection of a reconciliation row for the report,
// pre-computing the expected closing (opening + deliveries - sales + adjustments)
// as a decimal STRING via string-safe arithmetic-free passthrough of the
// persisted ClosingBook (which already is the expected book balance). The
// product identity + the litre variance's monetary value come enriched from
// the station-day projection so the signature layout can render a variance
// heatmap (tank × product) and a variance-value KPI — every figure an exact
// decimal string, the value computed in SQL numeric (never recomputed in Go).
type reconLine struct {
	TankID           uuid.UUID
	TankLabel        string
	ProductCode      string
	ProductName      string
	ProductColor     string
	OpeningBook      string
	DeliveriesTotal  string
	SalesTotal       string
	AdjustmentsTotal string
	ExpectedClosing  string
	ClosingPhysical  string
	VarianceLitres   string
	VariancePercent  string
	VarianceValue    string // |variance_litres| × product price, decimal string
	Priced           bool
	TolerancePercent string
	Status           string
}

// reconLineFromStationDay projects a product-enriched station-day recon line.
// ClosingBook is the expected (book) closing balance; ClosingPhysical is the
// actual measured closing; VarianceValue is the litre variance priced in SQL.
// All are exact decimal strings, passed through.
func reconLineFromStationDay(line reconciliation.StationDayReconLine) reconLine {
	return reconLine{
		TankID:           line.TankID,
		TankLabel:        tankLabelFor(line),
		ProductCode:      line.ProductCode,
		ProductName:      line.ProductName,
		ProductColor:     line.ProductColor,
		OpeningBook:      line.OpeningBook,
		DeliveriesTotal:  line.DeliveriesTotal,
		SalesTotal:       line.SalesTotal,
		AdjustmentsTotal: line.AdjustmentsTotal,
		ExpectedClosing:  line.ClosingBook,
		ClosingPhysical:  line.ClosingPhysical,
		VarianceLitres:   line.VarianceLitres,
		VariancePercent:  line.VariancePercent,
		VarianceValue:    line.VarianceValue,
		Priced:           line.Priced,
		TolerancePercent: line.TolerancePercent,
		Status:           line.Status,
	}
}

// tankLabelFor names a tank line: the product code when present (the readable
// fuel identity), otherwise the tank-id prefix used historically.
func tankLabelFor(line reconciliation.StationDayReconLine) string {
	if strings.TrimSpace(line.ProductCode) != "" {
		return line.ProductCode
	}
	return line.TankID.String()[:8]
}
