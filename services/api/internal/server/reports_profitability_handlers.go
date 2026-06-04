package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
)

// Profitability + station-comparison structured reports (Features 10.4 / 10.6).
//
// Both return the shared ReportEnvelope (report_envelope.go) and reuse the SAME
// recognized-sales/expense facts the dashboards and CSV exports use — no money
// or litre figure is recomputed in Go float. Every total is summed in SQL
// ::numeric (internal/revenue.Profitability / StationComparison) and carried
// through as an exact decimal string. The deterministic insights + data-quality
// warnings come from internal/reporting verbatim.
//
// Profitability is station-scoped (?station_id, revenue.read) with an in-handler
// authorizeStation so an out-of-scope station 403s and a cross-tenant one 404s.
// Station-comparison is tenant-wide-gated (revenue.read held anywhere) but the
// ROW SET is filtered to the actor's accessible stations via stationScope, so a
// station-restricted actor only ever sees and ranks the stations they may read.

// ---- Profitability report (10.4) ----

// handleProfitabilityReport returns a station's profit-and-loss over a period as
// a ReportEnvelope: revenue, COGS, gross margin, operating expenses, net
// operating result, and a per-product breakdown. Station-scoped, gated by
// revenue.read. ?period selects the date window (this-month default).
func (s *Server) handleProfitabilityReport(w http.ResponseWriter, r *http.Request) {
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
	from, to, period := resolveReportPeriod(r.URL.Query().Get("period"), time.Now())
	sid := stationID.String()
	env := newEnvelope("profitability", "Profitability", period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["period"] = period
	env.FiltersUsed["from"] = from.Format(dateLayout)
	env.FiltersUsed["to"] = to.Format(dateLayout)

	totals, terr := s.revenue.Profitability(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	if terr != nil {
		s.logger.Error("profitability report: totals", "error", terr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	products, perr := s.revenue.ProfitabilityByProduct(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	if perr != nil {
		s.logger.Error("profitability report: by product", "error", perr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	days, unlocked, lerr := s.revenue.WindowLockState(ctx, actor.TenantID, stationID, from, to)
	if lerr != nil {
		s.logger.Error("profitability report: lock state", "error", lerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	env.Summary = []summaryMetric{
		{Label: "Net revenue", Value: totals.Revenue, Unit: "TZS"},
		{Label: "COGS", Value: totals.Cogs, Unit: "TZS"},
		{Label: "Gross margin", Value: totals.GrossMargin, Unit: "TZS"},
		{Label: "Operating expenses", Value: totals.Expenses, Unit: "TZS"},
		{Label: "Net operating result", Value: totals.NetOperating, Unit: "TZS"},
		{Label: "Litres sold", Value: totals.LitresSold, Unit: "L"},
	}

	// Per-product table + chart (decimal strings throughout).
	env.Table.Columns = []string{"product", "litres", "revenue", "cogs", "gross_margin"}
	type productSlice struct {
		Product     string `json:"product"`
		LitresSold  string `json:"litres"`
		Revenue     string `json:"revenue"`
		Cogs        string `json:"cogs"`
		GrossMargin string `json:"gross_margin"`
	}
	chart := make([]productSlice, 0, len(products))
	for i := range products {
		p := products[i]
		env.Table.Rows = append(env.Table.Rows, []string{
			p.ProductName, p.LitresSold, p.Revenue, p.Cogs, p.GrossMargin,
		})
		chart = append(chart, productSlice{
			Product: p.ProductName, LitresSold: p.LitresSold, Revenue: p.Revenue,
			Cogs: p.Cogs, GrossMargin: p.GrossMargin,
		})
	}
	env.ChartData = chart

	env.applyReport(reporting.Profitability(reporting.ProfitabilityInput{
		NetRevenue:   totals.Revenue,
		Cogs:         totals.Cogs,
		GrossMargin:  totals.GrossMargin,
		Expenses:     totals.Expenses,
		NetOperating: totals.NetOperating,
		HasSales:     totals.SaleCount > 0,
		PeriodLocked: days > 0 && unlocked == 0,
	}))
	if days == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level: "warning", Message: "No revenue days recorded for this station in the period.",
		})
	}

	env.Drilldown = []drilldownLink{
		{Label: "Daily station close", Href: "/api/v1/reports/station-close?station_id=" + sid},
		{Label: "Operations overview", Href: "/api/v1/stations/" + sid + "/operations/overview"},
	}
	env.ExportOptions = []exportOption{
		{Format: "csv", URL: "/api/v1/reports/financials.csv?period=" + period},
		{Format: "xlsx", URL: "/api/v1/reports/financials.xlsx?period=" + period},
		{Format: "pdf", URL: "/api/v1/reports/financials.pdf?period=" + period},
	}
	writeJSON(w, http.StatusOK, env)
}

// ---- Station comparison report (10.6) ----

// handleStationComparisonReport returns a per-station ranking over a period as a
// ReportEnvelope: revenue, litres sold, margin, stock variance, expenses, open
// risk alerts and outstanding collections. Gated by revenue.read held anywhere;
// the ROWS are filtered to the actor's accessible stations (stationScope) so a
// station-restricted actor never sees a station outside their grant.
func (s *Server) handleStationComparisonReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()

	// Resolve the actor's read scope. A tenant-wide actor compares every station;
	// a restricted actor compares exactly their granted stations (and a restricted
	// actor with no grants is 403'd by stationReadFilter's default-deny).
	tenantWide, scope, scopeOK := s.stationScope(w, r, actor)
	if !scopeOK {
		return
	}
	var stationIDs []uuid.UUID
	if tenantWide {
		all, lerr := s.stations.List(ctx, actor.TenantID, nil, nil)
		if lerr != nil {
			s.logger.Error("station-comparison report: list stations", "error", lerr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for i := range all {
			stationIDs = append(stationIDs, all[i].ID)
		}
	} else {
		if len(scope) == 0 {
			writeError(w, http.StatusForbidden, "no station access")
			return
		}
		stationIDs = scope
	}

	from, to, period := resolveReportPeriod(r.URL.Query().Get("period"), time.Now())
	env := newEnvelope("station-comparison", "Station Comparison", period, nil)
	env.FiltersUsed["period"] = period
	env.FiltersUsed["from"] = from.Format(dateLayout)
	env.FiltersUsed["to"] = to.Format(dateLayout)
	env.FiltersUsed["stations_in_scope"] = strconv.Itoa(len(stationIDs))

	rows, rerr := s.revenue.StationComparison(ctx, actor.TenantID, stationIDs, from, to)
	if rerr != nil {
		s.logger.Error("station-comparison report: rank", "error", rerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	env.Table.Columns = []string{
		"station", "revenue", "litres", "gross_margin", "expenses",
		"net_operating", "stock_variance", "risk_alerts", "collections",
	}
	type rankSlice struct {
		Station      string `json:"station"`
		Revenue      string `json:"revenue"`
		LitresSold   string `json:"litres"`
		GrossMargin  string `json:"gross_margin"`
		Expenses     string `json:"expenses"`
		NetOperating string `json:"net_operating"`
		RiskAlerts   int    `json:"risk_alerts"`
	}
	chart := make([]rankSlice, 0, len(rows))
	cmpStations := make([]reporting.ComparisonStation, 0, len(rows))
	var totalAlerts int
	for i := range rows {
		c := rows[i]
		label := c.StationCode
		if label == "" {
			label = c.StationName
		}
		totalAlerts += c.RiskAlerts
		env.Table.Rows = append(env.Table.Rows, []string{
			label, c.Revenue, c.LitresSold, c.GrossMargin, c.Expenses,
			c.NetOperating, c.StockVariance, strconv.Itoa(c.RiskAlerts), c.Collections,
		})
		chart = append(chart, rankSlice{
			Station: label, Revenue: c.Revenue, LitresSold: c.LitresSold,
			GrossMargin: c.GrossMargin, Expenses: c.Expenses, NetOperating: c.NetOperating,
			RiskAlerts: c.RiskAlerts,
		})
		cmpStations = append(cmpStations, reporting.ComparisonStation{
			Name: label, NetOperating: c.NetOperating, GrossMargin: c.GrossMargin,
			NetRevenue: c.Revenue, RiskAlerts: c.RiskAlerts,
		})
	}
	env.ChartData = chart

	env.Summary = []summaryMetric{
		{Label: "Stations compared", Value: strconv.Itoa(len(rows)), Unit: "count"},
		{Label: "Open risk alerts", Value: strconv.Itoa(totalAlerts), Unit: "count"},
	}

	env.applyReport(reporting.StationComparison(reporting.StationComparisonInput{
		Stations: cmpStations,
		Scoped:   !tenantWide,
	}))

	env.Drilldown = []drilldownLink{
		{Label: "Enterprise overview", Href: "/api/v1/enterprise/overview"},
		{Label: "Station ranking", Href: "/api/v1/enterprise/station-ranking"},
	}
	writeJSON(w, http.StatusOK, env)
}
