package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/procurement"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
)

// Delivery & Procurement report (Reports Center §5.7) — the signature Delivery
// suite as a structured ReportEnvelope (report_envelope.go).
//
// Station-scoped via ?station_id (gated by station.read at the route, plus an
// in-handler authorizeStation so an out-of-scope station 403s and a cross-tenant
// one 404s). ?period selects the business-date window (this-month default),
// reusing resolveReportPeriod.
//
// Every litre/money figure is summed in SQL ::numeric (internal/procurement
// DeliveryTotals / DeliveryComparison / DeliveryLines / SupplierScorecardFacts)
// and carried through as an exact decimal STRING — no figure is recomputed in Go
// float (the fulfilment % and score math parse to float for DISPLAY only).
//
// SENSITIVE-METRIC GATING (blueprint §14): supplier COST is sensitive. The fuel
// cost / landed cost / price-competitiveness dimension is only surfaced to an
// actor holding margin.view at the station (the same gate the Sales report uses
// for margin/COGS). A non-cost actor sees ordered/loaded/received litres,
// delivery variance, status, delays and the scorecard WITHOUT its price dimension
// — cost figures are OMITTED entirely (not zeroed).

// deliveryComparisonRow is one product's ordered/loaded/received litres for the
// §5.7 comparison bar chart. Decimal strings throughout.
type deliveryComparisonRow struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Color    string `json:"color,omitempty"`
	Ordered  string `json:"ordered"`
	Loaded   string `json:"loaded"`
	Received string `json:"received"`
}

// deliveryLineRow is one delivery receipt for the variance chart + table.
type deliveryLineRow struct {
	Key         string  `json:"key"`
	ReceivedAt  string  `json:"received_at"`
	Supplier    string  `json:"supplier"`
	Product     string  `json:"product"`
	Volume      string  `json:"volume"`
	DipVariance string  `json:"dip_variance"`
	MatchStatus string  `json:"match_status"`
	Late        bool    `json:"late"`
	LandedCost  *string `json:"landed_cost,omitempty"`
}

// pipelineStage is one purchase-order-status bucket for the procurement pipeline.
type pipelineStage struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// deliveryChartData is the Delivery report's report-specific chart payload: the
// per-product ordered/loaded/received comparison, the per-delivery variance
// series, the supplier scorecards, and the procurement pipeline. cost_shown
// mirrors the Sales report's margin_shown flag so the page knows whether the cost
// dimension is present.
type deliveryChartData struct {
	Comparison []deliveryComparisonRow   `json:"comparison"`
	Deliveries []deliveryLineRow         `json:"deliveries"`
	Scorecards []reporting.SupplierScore `json:"scorecards"`
	Pipeline   []pipelineStage           `json:"pipeline"`
	CostShown  bool                      `json:"cost_shown"`
}

// handleDeliveryReport returns the §5.7 Delivery & Procurement report for a
// station over a period as a ReportEnvelope: an ordered/loaded/received + variance
// + delivery-count KPI hero (cost KPIs gated), the ordered-vs-received comparison,
// the per-delivery variance, the supplier scorecard, the procurement pipeline, the
// deterministic delivery insights, and honest data-quality (unmatched deliveries,
// pending invoices, open discrepancies). Station-scoped, gated by station.read.
func (s *Server) handleDeliveryReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	stationID, ok := s.resolveStationScoped(w, r, actor, "station.read")
	if !ok {
		return
	}
	ctx := r.Context()
	from, to, period := resolveReportPeriod(r.URL.Query().Get("period"), time.Now())
	sid := stationID.String()
	env := newEnvelope("delivery", "Delivery & Procurement", period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["period"] = period
	env.FiltersUsed["from"] = from.Format(dateLayout)
	env.FiltersUsed["to"] = to.Format(dateLayout)

	// Supplier COST is sensitive: only attach cost figures (fuel cost KPI, landed
	// cost column, price-competitiveness scorecard dimension) when the actor can
	// read margin/cost at the station. Decided once, applied to every figure.
	costAllowed := s.canViewMarginAtStation(ctx, actor, stationID)

	totals, terr := s.procurement.DeliveryTotals(ctx, actor.TenantID, stationID, from, to, costAllowed)
	if terr != nil {
		s.logger.Error("delivery report: totals", "error", terr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	comparison, cerr := s.procurement.DeliveryComparison(ctx, actor.TenantID, stationID, from, to)
	if cerr != nil {
		s.logger.Error("delivery report: comparison", "error", cerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	lines, lerr := s.procurement.DeliveryLines(ctx, actor.TenantID, stationID, from, to, costAllowed)
	if lerr != nil {
		s.logger.Error("delivery report: lines", "error", lerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	pipeline, perr := s.procurement.PurchaseOrderPipeline(ctx, actor.TenantID, stationID, from, to)
	if perr != nil {
		s.logger.Error("delivery report: pipeline", "error", perr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	facts, ferr := s.procurement.SupplierScorecardFacts(ctx, actor.TenantID, stationID, from, to, costAllowed)
	if ferr != nil {
		s.logger.Error("delivery report: scorecard facts", "error", ferr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	dq, derr := s.procurement.DeliveryWindowQuality(ctx, actor.TenantID, stationID, from, to)
	if derr != nil {
		s.logger.Error("delivery report: quality", "error", derr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// ---- KPI hero (§5.7): ordered, received, variance, delivery count, late ----
	env.Summary = []summaryMetric{
		{Label: "Ordered", Value: totals.OrderedLitres, Unit: "L"},
		{Label: "Received", Value: totals.ReceivedLitres, Unit: "L"},
		{Label: "Delivery variance", Value: totals.VarianceLitres, Unit: "L", Direction: varianceDirection(totals.VarianceLitres)},
		{Label: "Deliveries", Value: strconv.Itoa(totals.DeliveryCount), Unit: "count"},
		{Label: "Late deliveries", Value: strconv.Itoa(totals.LateDeliveries), Unit: "count"},
	}
	if costAllowed {
		env.Summary = append(env.Summary,
			summaryMetric{Label: "Fuel cost", Value: totals.FuelCostTotal, Unit: "TZS"},
			summaryMetric{Label: "Avg cost / litre", Value: totals.AvgCostPerLitre, Unit: "TZS"},
		)
	}

	// ---- supplier scorecards (deterministic; price dimension gated) ----
	scorecards := reporting.RankSuppliers(deliverySupplierFacts(facts))

	// ---- chart_data: comparison + per-delivery variance + scorecards + pipeline ----
	chart := deliveryChartData{
		Comparison: deliveryComparisonRows(comparison),
		Deliveries: deliveryLineRows(lines, costAllowed),
		Scorecards: scorecards,
		Pipeline:   pipelineStages(pipeline),
		CostShown:  costAllowed,
	}
	env.ChartData = chart

	// ---- drillable table: the delivery receipts (the headline fact) ----
	if costAllowed {
		env.Table.Columns = []string{"received_at", "supplier", "product", "volume", "dip_variance", "status", "late", "landed_cost"}
	} else {
		env.Table.Columns = []string{"received_at", "supplier", "product", "volume", "dip_variance", "status", "late"}
	}
	for i := range lines {
		l := lines[i]
		row := []string{
			l.ReceivedAt.Format(time.RFC3339), l.SupplierName, l.ProductName,
			l.VolumeLitres, l.DipVariance, l.MatchStatus, strconv.FormatBool(l.Late),
		}
		if costAllowed {
			row = append(row, l.LandedCost)
		}
		env.Table.Rows = append(env.Table.Rows, row)
	}

	// ---- deterministic insights (the delivery composer) ----
	env.applyReport(reporting.Delivery(reporting.DeliveryInput{
		OrderedLitres:   totals.OrderedLitres,
		ReceivedLitres:  totals.ReceivedLitres,
		VarianceLitres:  totals.VarianceLitres,
		DeliveryCount:   totals.DeliveryCount,
		UnapprovedCount: dq.UnmatchedDeliveries,
		PendingInvoices: dq.PendingInvoices,
		OpenDiscreps:    dq.OpenDiscrepancies,
		LateDeliveries:  dq.LateDeliveries,
		PeriodComplete:  dq.OpenPurchaseOrders == 0,
	}))

	// ---- honest data-quality beyond the composer ----
	if totals.DeliveryCount == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "warning",
			Message: "No deliveries were received for this station in the period — delivery figures are empty.",
		})
	}
	if !costAllowed {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "info",
			Message: "Supplier cost and price competitiveness are hidden — they require the margin.view permission.",
		})
	}

	// ---- drilldown (§5.7) ----
	env.Drilldown = []drilldownLink{
		{Label: "Purchase orders", Href: "/api/v1/purchase-orders?station_id=" + sid},
		{Label: "Suppliers", Href: "/api/v1/suppliers"},
		{Label: "Inventory reconciliation", Href: "/api/v1/reports/inventory/reconciliation?station_id=" + sid},
		{Label: "Operations overview", Href: "/api/v1/stations/" + sid + "/operations/overview"},
	}
	// Deliveries are part of the recognized inventory facts; the station revenue
	// CSV/XLSX cover the day's movements. Reuse the wired station export endpoints.
	env.ExportOptions = []exportOption{
		{Format: "csv", URL: "/api/v1/stations/" + sid + "/reports/inventory.csv"},
		{Format: "xlsx", URL: "/api/v1/stations/" + sid + "/reports/reconciliation.xlsx"},
	}
	writeJSON(w, http.StatusOK, env)
}

// deliverySupplierFacts maps the repo's SupplierFactsRow onto the reporting
// package's SupplierFacts (the deterministic scorecard input).
func deliverySupplierFacts(in []procurement.SupplierFactsRow) []reporting.SupplierFacts {
	out := make([]reporting.SupplierFacts, 0, len(in))
	for i := range in {
		f := in[i]
		out = append(out, reporting.SupplierFacts{
			SupplierID:          f.SupplierID.String(),
			SupplierName:        f.SupplierName,
			OnTimeCount:         f.OnTimeCount,
			OnTimeTotal:         f.OnTimeTotal,
			QtyAccuracy:         f.QtyAccuracy,
			QtyAccuracyHas:      f.QtyAccuracyHas,
			DisputeCount:        f.DisputeCount,
			DeliveryNum:         f.DeliveryCount,
			InvoicesApproved:    f.InvoicesApproved,
			InvoicesTotal:       f.InvoicesTotal,
			DipVarianceBreaches: f.DipVarianceBreaches,
			PriceRatio:          f.PriceRatio,
			PriceKnown:          f.PriceKnown,
		})
	}
	return out
}

// deliveryComparisonRows maps repo comparison rows onto the chart_data shape.
func deliveryComparisonRows(in []procurement.DeliveryComparisonRow) []deliveryComparisonRow {
	out := make([]deliveryComparisonRow, 0, len(in))
	for i := range in {
		c := in[i]
		out = append(out, deliveryComparisonRow{
			Key: c.ProductID.String(), Label: c.ProductName, Color: c.ProductColor,
			Ordered: c.Ordered, Loaded: c.Loaded, Received: c.Received,
		})
	}
	return out
}

// deliveryLineRows maps repo delivery lines onto the chart_data shape, omitting
// the landed-cost field entirely when cost is gated.
func deliveryLineRows(in []procurement.DeliveryLine, costAllowed bool) []deliveryLineRow {
	out := make([]deliveryLineRow, 0, len(in))
	for i := range in {
		l := in[i]
		row := deliveryLineRow{
			Key: l.DeliveryID.String(), ReceivedAt: l.ReceivedAt.Format(time.RFC3339),
			Supplier: l.SupplierName, Product: l.ProductName, Volume: l.VolumeLitres,
			DipVariance: l.DipVariance, MatchStatus: l.MatchStatus, Late: l.Late,
		}
		if costAllowed {
			lc := l.LandedCost
			row.LandedCost = &lc
		}
		out = append(out, row)
	}
	return out
}

// pipelineStages maps repo PO-status rows onto the chart_data pipeline shape.
func pipelineStages(in []procurement.PurchaseOrderStatusRow) []pipelineStage {
	out := make([]pipelineStage, 0, len(in))
	for i := range in {
		out = append(out, pipelineStage{Status: in[i].Status, Count: in[i].Count})
	}
	return out
}

// varianceDirection grades a signed delivery-variance litres string for the
// MetricCard arrow: a shortfall (received < ordered) is "down", an over-delivery
// "up", exact "flat". Parses to float for the DISPLAY direction only.
func varianceDirection(variance string) string {
	v, ok := parseFloatSafe(variance)
	if !ok || v == 0 {
		return "flat"
	}
	if v < 0 {
		return "down"
	}
	return "up"
}
