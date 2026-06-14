package server

import (
	"strconv"

	"github.com/japharyroman/fuelgrid-os/internal/reporting"
)

// Executive cockpit chart payload (blueprint §5.1 / §20.1). The report-specific
// chart_data the executive page renders, carried in one envelope: the automated
// management narrative, a per-station revenue + volume ranking (the network
// league table chart), a P&L waterfall (revenue → margin → expenses → net
// operating), period-comparison cards, and a loss/variance summary. Every
// money/litre figure is an exact decimal STRING; the only floats are the
// caller's already-computed display aggregates. Sensitive series (margin
// waterfall, loss value) are present ONLY when marginShown — omit, never zero.

// execChartStation is one station's slice of the revenue/volume ranking chart.
type execChartStation struct {
	Station      string `json:"station"`
	Revenue      string `json:"revenue"`
	Litres       string `json:"litres"`
	NetOperating string `json:"net_operating"`
	RiskAlerts   int    `json:"risk_alerts"`
}

// execWaterfallStep is one step of the network P&L waterfall (reuses the
// @fuelgrid/ui FinancialWaterfall contract: base/delta/total + a decimal value).
type execWaterfallStep struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
	Kind  string `json:"kind"` // base | delta | total
}

// execComparisonCard is one period-over-period comparison card: a metric with
// its current + prior decimal-string values and the display-only percent delta.
type execComparisonCard struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Current  string `json:"current"`
	Prior    string `json:"prior"`
	DeltaPct string `json:"delta_pct"` // signed, e.g. "+14.0" or "" when no base
	Unit     string `json:"unit"`      // TZS | L
}

// execLossSummary is the loss/variance summary block (litres always; value
// gated). StockVariance is the scope-wide absolute reconciliation variance.
type execLossSummary struct {
	LossLitres    string `json:"loss_litres"`
	LossValue     string `json:"loss_value,omitempty"`
	StockVariance string `json:"stock_variance"`
	ValueShown    bool   `json:"value_shown"`
}

// execChartData is the executive report's report-specific chart_data payload.
type execChartData struct {
	Narrative   reporting.ExecutiveNarrative `json:"narrative"`
	Stations    []execChartStation           `json:"stations"`
	Waterfall   []execWaterfallStep          `json:"waterfall"`
	Comparison  []execComparisonCard         `json:"comparison"`
	LossSummary execLossSummary              `json:"loss_summary"`
	MarginShown bool                         `json:"margin_shown"`
}

// buildExecutiveChart assembles the §5.1 cockpit chart payload from the already
// aggregated rollup. No money is recomputed — the per-station decimal strings
// pass through verbatim; the scope-wide totals are the caller's display
// aggregates. The P&L waterfall and the margin comparison card are present only
// when marginShown (sensitive figures omitted for non-holders). The
// period-comparison deltas are display-only percentages computed here.
func (s *Server) buildExecutiveChart(
	in reporting.ExecutiveInput,
	rollups []execStationRollup,
	marginShown bool,
	narrative reporting.ExecutiveNarrative,
	totalRevenue, totalMargin, totalExpenses, totalNetOp, totalStockVar float64,
) execChartData {
	stations := make([]execChartStation, 0, len(rollups))
	for i := range rollups {
		c := rollups[i].row
		label := c.StationCode
		if label == "" {
			label = c.StationName
		}
		stations = append(stations, execChartStation{
			Station: label, Revenue: c.Revenue, Litres: c.LitresSold,
			NetOperating: c.NetOperating, RiskAlerts: c.RiskAlerts,
		})
	}

	// Network P&L waterfall (revenue → −COGS → gross margin → −expenses → net
	// operating). Only surfaced when margin is permitted (COGS/margin are
	// sensitive). The COGS delta is revenue − gross margin (both already summed).
	var waterfall []execWaterfallStep
	if marginShown {
		cogs := totalRevenue - totalMargin
		waterfall = []execWaterfallStep{
			{Key: "revenue", Label: "Net revenue", Value: f2(totalRevenue), Kind: "base"},
			{Key: "cogs", Label: "COGS", Value: f2(-cogs), Kind: "delta"},
			{Key: "gross_margin", Label: "Gross margin", Value: f2(totalMargin), Kind: "total"},
			{Key: "expenses", Label: "Operating expenses", Value: f2(-totalExpenses), Kind: "delta"},
			{Key: "net_operating", Label: "Net operating result", Value: f2(totalNetOp), Kind: "total"},
		}
	}

	// Period-comparison cards. Revenue + litres are always shown; the margin card
	// only when permitted.
	comparison := []execComparisonCard{
		comparisonCard("revenue", "Revenue", in.Revenue.Current, in.Revenue.Prior, "TZS"),
		comparisonCard("litres", "Litres", in.Litres.Current, in.Litres.Prior, "L"),
	}
	if marginShown {
		comparison = append(comparison,
			comparisonCard("net_margin", "Net margin", in.NetMargin.Current, in.NetMargin.Prior, "TZS"))
	}

	loss := execLossSummary{
		LossLitres:    in.LossLitres,
		StockVariance: f3(totalStockVar),
		ValueShown:    marginShown,
	}
	if marginShown {
		loss.LossValue = in.LossValue
	}

	return execChartData{
		Narrative:   narrative,
		Stations:    stations,
		Waterfall:   waterfall,
		Comparison:  comparison,
		LossSummary: loss,
		MarginShown: marginShown,
	}
}

// comparisonCard builds one period-comparison card with a display-only signed
// percent delta (blank when there is no comparable positive prior base, so the
// card reads honestly rather than printing a misleading figure).
func comparisonCard(key, label, current, prior, unit string) execComparisonCard {
	card := execComparisonCard{Key: key, Label: label, Current: current, Prior: prior, Unit: unit}
	if pct, ok := execDisplayPct(current, prior); ok {
		card.DeltaPct = pct
	}
	return card
}

// execDisplayPct computes a signed display-only percent change string ("+14.0" /
// "-8.2") from two decimal strings, ok=false when there is no comparable positive
// prior base. This is a pure presentation figure (parseFloatSafe → float → a
// string); no money is recomputed.
func execDisplayPct(current, prior string) (string, bool) {
	c, okC := parseFloatSafe(current)
	p, okP := parseFloatSafe(prior)
	if !okC || !okP || p <= 0 {
		return "", false
	}
	pct := (c - p) / p * 100
	sign := ""
	if pct >= 0 {
		sign = "+"
	}
	return sign + strconv.FormatFloat(pct, 'f', 1, 64), true
}
