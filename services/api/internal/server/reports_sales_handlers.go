package server

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
	"github.com/japharyroman/fuelgrid-os/internal/revenue"
)

// Sales report (Reports Center §5.2) — the signature Sales suite as a structured
// ReportEnvelope (report_envelope.go).
//
// Station-scoped via ?station_id (gated by revenue.read at the route, plus an
// in-handler authorizeStation so an out-of-scope station 403s and a cross-tenant
// one 404s). ?period selects the business-date window (this-month default),
// reusing resolveReportPeriod.
//
// Every money/litre figure is summed in SQL ::numeric (internal/revenue
// SalesSummaryTotals / SalesBy*) and carried through as an exact decimal STRING
// — no figure is recomputed in Go float (the growth % and avg-price ratios parse
// to float for the DISPLAY headline only, exactly as the merged reports do).
//
// SENSITIVE-METRIC GATING (blueprint §14): margin is supplier-cost-derived, so it
// is only surfaced (KPI + the margin column on every dimension table) to an actor
// holding margin.view at the station. A non-margin actor sees litres / revenue /
// average price / transaction count — never margin or COGS.

// salesDimRow is one row of a sales breakdown carried in chart_data: a stable key
// (uuid/label for drilldown), a human label, the litres/revenue figures and the
// transaction count. Margin is a *string so it is OMITTED entirely (not zeroed)
// for an actor without margin.view.
type salesDimRow struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Color    string  `json:"color,omitempty"`
	Litres   string  `json:"litres"`
	Gross    string  `json:"gross"`
	Net      string  `json:"net"`
	Margin   *string `json:"margin,omitempty"`
	TxnCount int     `json:"txn_count"`
}

// salesTrendDay is one business-day point of the revenue-trend line.
type salesTrendDay struct {
	Date   string `json:"date"`
	Gross  string `json:"gross"`
	Litres string `json:"litres"`
}

// salesHourCell is one hour-of-day bucket for the peak-hours heatmap.
type salesHourCell struct {
	Hour   int    `json:"hour"`
	Gross  string `json:"gross"`
	Litres string `json:"litres"`
	Txn    int    `json:"txn"`
}

// salesChartData is the Sales report's report-specific chart payload: the trend
// line, the per-dimension breakdowns (product/payment/shift/attendant/nozzle),
// the hour grid, and the optional station ranking. Every figure is a decimal
// string; margin is omitted on each row when the actor lacks margin.view.
type salesChartData struct {
	Trend       []salesTrendDay `json:"trend"`
	ByProduct   []salesDimRow   `json:"by_product"`
	ByShift     []salesDimRow   `json:"by_shift"`
	ByAttendant []salesDimRow   `json:"by_attendant"`
	ByNozzle    []salesDimRow   `json:"by_nozzle"`
	ByHour      []salesHourCell `json:"by_hour"`
	Stations    []salesDimRow   `json:"stations"`
	MarginShown bool            `json:"margin_shown"`
}

// handleSalesReport returns the §5.2 Sales report for a station over a period as
// a ReportEnvelope: a litres/revenue/avg-price/txn-count/growth KPI hero, a
// revenue trend, product / payment / shift / attendant / nozzle breakdowns, a
// peak-hours grid, an optional station ranking (tenant-wide actors only), and the
// deterministic sales insights + honest data-quality. Station-scoped, gated by
// revenue.read.
func (s *Server) handleSalesReport(w http.ResponseWriter, r *http.Request) {
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
	env := newEnvelope("sales", "Sales", period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["period"] = period
	env.FiltersUsed["from"] = from.Format(dateLayout)
	env.FiltersUsed["to"] = to.Format(dateLayout)

	// Margin is supplier-cost-derived (sensitive): only attach it when the actor
	// can read margin at this station. Decided once, applied to every figure.
	marginAllowed := s.canViewMarginAtStation(ctx, actor, stationID)

	totals, terr := s.revenue.SalesSummaryTotals(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	if terr != nil {
		s.logger.Error("sales report: totals", "error", terr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Previous period of equal length for the growth KPI (period-over-period).
	prevFrom, prevTo := previousWindow(from, to)
	prevGross, pgErr := s.revenue.SalesGrossForWindow(ctx, s.deps.DB, actor.TenantID, stationID, prevFrom, prevTo)
	if pgErr != nil {
		s.logger.Error("sales report: prev gross", "error", pgErr)
		prevGross = "0"
	}

	trend, trErr := s.revenue.SalesByDay(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	if trErr != nil {
		s.logger.Error("sales report: by day", "error", trErr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	byProduct, _ := s.revenue.SalesByProduct(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	byShift, _ := s.revenue.SalesByShift(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	byAttendant, _ := s.revenue.SalesByAttendant(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	byNozzle, _ := s.revenue.SalesByNozzle(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	byHour, _ := s.revenue.SalesByHour(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	tenders, _ := s.revenue.SalesTenders(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	dq, _ := s.revenue.SalesWindowQuality(ctx, s.deps.DB, actor.TenantID, stationID, from, to)

	// ---- KPI hero (§5.2): litres, revenue, avg selling price, txn count, growth ----
	growthDelta, growthDir := growthVsPrevious(totals.GrossRevenue, prevGross)
	env.Summary = []summaryMetric{
		{Label: "Litres sold", Value: totals.LitresSold, Unit: "L"},
		{Label: "Revenue", Value: totals.GrossRevenue, Unit: "TZS", Delta: growthDelta, Direction: growthDir},
		{Label: "Average selling price", Value: totals.AvgSellingPrice, Unit: "TZS"},
		{Label: "Transactions", Value: strconv.Itoa(totals.TxnCount), Unit: "count"},
	}
	if marginAllowed {
		env.Summary = append(env.Summary, summaryMetric{Label: "Gross margin", Value: totals.Margin, Unit: "TZS"})
	}

	// ---- tender mix (reuses the envelope's tender_mix shape for the donut) ----
	if tenders.Total != "0" && tenders.Total != "" {
		env.TenderMix = &tenderMix{
			Cash: tenders.Cash, MobileMoney: tenders.MobileMoney, Card: tenders.Card,
			Credit: tenders.Credit, Voucher: tenders.Voucher, Total: tenders.Total,
		}
	}

	// ---- station ranking (additive): only a tenant-wide actor may see a
	// cross-station total, otherwise it would leak stations they cannot read. ----
	var stationRows []salesDimRow
	if tenantWide, _, scopeOK := s.salesActorTenantWide(ctx, actor); scopeOK && tenantWide {
		all, lerr := s.stations.List(ctx, actor.TenantID, nil, nil)
		if lerr == nil {
			ids := make([]uuid.UUID, 0, len(all))
			for i := range all {
				ids = append(ids, all[i].ID)
			}
			if ranks, rerr := s.revenue.StationComparison(ctx, actor.TenantID, ids, from, to); rerr == nil {
				for i := range ranks {
					c := ranks[i]
					label := c.StationCode
					if label == "" {
						label = c.StationName
					}
					// StationComparison only carries NET revenue (net of voids), so the
					// ranking row leaves Gross empty rather than mislabelling net as
					// gross — the page reads Net for the ranking "Revenue" column.
					row := salesDimRow{
						Key: c.StationID.String(), Label: label,
						Litres: c.LitresSold, Net: c.Revenue,
					}
					if marginAllowed {
						m := c.GrossMargin
						row.Margin = &m
					}
					stationRows = append(stationRows, row)
				}
			}
		}
	}

	// ---- chart_data: trend + every dimension (margin omitted per gate) ----
	chart := salesChartData{
		Trend:       make([]salesTrendDay, 0, len(trend)),
		ByProduct:   dimRows(byProduct, marginAllowed),
		ByShift:     dimRows(byShift, marginAllowed),
		ByAttendant: dimRows(byAttendant, marginAllowed),
		ByNozzle:    dimRows(byNozzle, marginAllowed),
		ByHour:      fillHourGrid(byHour),
		Stations:    stationRows,
		MarginShown: marginAllowed,
	}
	for i := range trend {
		chart.Trend = append(chart.Trend, salesTrendDay{
			Date: trend[i].BusinessDate, Gross: trend[i].Gross, Litres: trend[i].Litres,
		})
	}
	env.ChartData = chart

	// ---- drillable table: the by-product breakdown (the headline dimension) ----
	if marginAllowed {
		env.Table.Columns = []string{"product", "litres", "gross", "net", "margin", "txn"}
	} else {
		env.Table.Columns = []string{"product", "litres", "gross", "net", "txn"}
	}
	for i := range byProduct {
		p := byProduct[i]
		_, name := splitProductKey(p.Key, p.Label)
		row := []string{name, p.Litres, p.Gross, p.Net}
		if marginAllowed {
			row = append(row, p.Margin)
		}
		row = append(row, strconv.Itoa(p.TxnCount))
		env.Table.Rows = append(env.Table.Rows, row)
	}

	// ---- deterministic insights (reuse the merged SalesSummary composer) ----
	grossPts := salesDayPoints(trend, func(d revenue.SalesDayPoint) string { return d.Gross })
	marginPts := salesDayPoints(trend, func(d revenue.SalesDayPoint) string { return d.Margin })
	env.applyReport(reporting.SalesSummary(reporting.SalesInput{
		GrossSeries:  grossPts,
		MarginSeries: marginPts,
		PeriodLocked: dq.RevenueDays > 0 && dq.UnlockedDays == 0,
	}))

	// ---- honest data-quality: empty window, unapproved shifts, unlocked days ----
	if totals.TxnCount == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level: "warning", Message: "No recognized sales for this station in the period — sales figures are empty.",
		})
	}
	if dq.UnapprovedShifts > 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level: "warning",
			Message: strconv.Itoa(dq.UnapprovedShifts) +
				" shift(s) in this period are not yet approved — their sales are not yet recognized, so these figures may rise.",
		})
	}
	if !marginAllowed {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "info",
			Message: "Margin and cost are hidden — they require the margin.view permission.",
		})
	}

	// ---- drilldown (§5.2: Total → station → product → … → shift → attendant) ----
	env.Drilldown = []drilldownLink{
		{Label: "Daily station close", Href: "/api/v1/reports/station-close?station_id=" + sid},
		{Label: "Profitability", Href: "/api/v1/reports/profitability?station_id=" + sid},
		{Label: "Operations overview", Href: "/api/v1/stations/" + sid + "/operations/overview"},
	}
	// Revenue CSV/XLSX cover the recognized sales facts; the daily-close PDF is the
	// printable sales summary. Reuse the wired station export endpoints.
	env.ExportOptions = []exportOption{
		{Format: "csv", URL: "/api/v1/stations/" + sid + "/reports/revenue.csv"},
		{Format: "pdf", URL: "/api/v1/stations/" + sid + "/reports/daily-close.pdf"},
		{Format: "xlsx", URL: "/api/v1/stations/" + sid + "/reports/revenue.xlsx"},
	}
	writeJSON(w, http.StatusOK, env)
}

// dimRows maps repo SalesDimensionRow values onto the chart_data salesDimRow,
// stripping the margin string entirely when marginAllowed is false and splitting
// the product key's embedded color (uuid|#color) into Key + Color.
func dimRows(in []revenue.SalesDimensionRow, marginAllowed bool) []salesDimRow {
	out := make([]salesDimRow, 0, len(in))
	for i := range in {
		d := in[i]
		key, color := splitColorKey(d.Key)
		row := salesDimRow{
			Key: key, Label: d.Label, Color: color,
			Litres: d.Litres, Gross: d.Gross, Net: d.Net, TxnCount: d.TxnCount,
		}
		if marginAllowed {
			m := d.Margin
			row.Margin = &m
		}
		out = append(out, row)
	}
	return out
}

// splitColorKey splits a "uuid|#color" key into (uuid, color); a key with no '|'
// returns (key, "").
func splitColorKey(key string) (id, color string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

// splitProductKey returns the product id + a display name from a product
// dimension row. The repo packs (uuid|#color) into Key and the name into Label.
func splitProductKey(key, label string) (id, name string) {
	id, _ = splitColorKey(key)
	return id, label
}

// fillHourGrid expands the sparse hour buckets into a full 0..23 grid so the
// heatmap always renders 24 cells (zero hours read as empty, not missing).
func fillHourGrid(cells []revenue.SalesHourCell) []salesHourCell {
	byHour := map[int]revenue.SalesHourCell{}
	for i := range cells {
		byHour[cells[i].Hour] = cells[i]
	}
	out := make([]salesHourCell, 0, 24)
	for h := 0; h < 24; h++ {
		if c, ok := byHour[h]; ok {
			out = append(out, salesHourCell{Hour: h, Gross: c.Gross, Litres: c.Litres, Txn: c.Txn})
		} else {
			out = append(out, salesHourCell{Hour: h, Gross: "0", Litres: "0", Txn: 0})
		}
	}
	return out
}

// salesDayPoints projects the trend onto reporting.PeriodPoint (oldest→newest,
// as SalesByDay already returns), picking gross or margin via pick.
func salesDayPoints(days []revenue.SalesDayPoint, pick func(revenue.SalesDayPoint) string) []reporting.PeriodPoint {
	pts := make([]reporting.PeriodPoint, 0, len(days))
	for i := range days {
		pts = append(pts, reporting.PeriodPoint{Label: days[i].BusinessDate, Value: pick(days[i])})
	}
	return pts
}

// previousWindow returns the immediately-preceding window of the SAME length as
// [from, to] (inclusive days), for the period-over-period growth KPI.
func previousWindow(from, to time.Time) (prevFrom, prevTo time.Time) {
	// Inclusive day span; the previous window ends the day before `from`.
	prevTo = from.AddDate(0, 0, -1)
	days := int(to.Sub(from).Hours()/24) + 1
	if days < 1 {
		days = 1
	}
	prevFrom = prevTo.AddDate(0, 0, -(days - 1))
	return prevFrom, prevTo
}

// growthVsPrevious computes the period-over-period growth of the current gross
// vs the previous-period gross as a signed percent DELTA string (e.g. "+12.4%")
// and a direction (up/down/flat) for the MetricCard. Parses to float for the
// DISPLAY ratio only; the underlying money figures stay decimal strings.
func growthVsPrevious(current, previous string) (*string, string) {
	cur, okC := parseFloatSafe(current)
	prev, okP := parseFloatSafe(previous)
	if !okC || !okP || prev == 0 {
		return nil, "flat"
	}
	pct := (cur - prev) / prev * 100
	dir := "flat"
	sign := ""
	switch {
	case pct > 0.05:
		dir, sign = "up", "+"
	case pct < -0.05:
		dir = "down"
	}
	out := sign + strconv.FormatFloat(pct, 'f', 1, 64) + "%"
	return &out, dir
}

// canViewMarginAtStation reports whether the actor may read margin/cost at the
// station — the sensitive-metric gate (blueprint §14). Margin.view is the
// existing margin/cost permission (migration 0004 / 0033). A failed policy check
// is treated as "not allowed" (fail-closed) so cost never leaks on an error.
func (s *Server) canViewMarginAtStation(ctx context.Context, actor identity.Actor, stationID uuid.UUID) bool {
	ps, err := s.policy.LoadFor(ctx, actor)
	if err != nil {
		return false
	}
	if ps.IsSystemAdmin {
		return true
	}
	if !ps.HasPermission("margin.view") {
		return false
	}
	if !ps.StationScoped["margin.view"] {
		return true
	}
	// Station-scoped margin.view: honour the per-station grant.
	if ps.TenantWide {
		return true
	}
	_, scoped := ps.StationIDs[stationID]
	return scoped
}

// salesActorTenantWide reports whether the actor has tenant-wide revenue reach
// (so a cross-station ranking is honest for them). Mirrors stationScope but never
// writes a response — the ranking is additive, so a non-tenant-wide actor simply
// gets no ranking rather than an error.
func (s *Server) salesActorTenantWide(ctx context.Context, actor identity.Actor) (tenantWide, hasScope, ok bool) {
	ps, err := s.policy.LoadFor(ctx, actor)
	if err != nil {
		return false, false, false
	}
	if ps.IsSystemAdmin || ps.TenantWide {
		return true, true, true
	}
	return false, len(ps.StationIDs) > 0, true
}
