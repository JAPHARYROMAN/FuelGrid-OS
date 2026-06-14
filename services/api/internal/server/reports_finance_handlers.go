package server

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/accounting"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
)

// Finance P&L report (Reports Center §5.8) — the signature finance statement as a
// structured ReportEnvelope (report_envelope.go).
//
// Station-scoped via ?station_id (gated by finance.read at the route, plus an
// in-handler authorizeStation so an out-of-scope station 403s and a cross-tenant
// one 404s). ?period selects the business-date window (this-month default),
// reusing resolveReportPeriod.
//
// REUSE, DON'T RECOMPUTE: every money figure is summed in SQL ::numeric by the
// SAME repo facts the Profitability + Credit/Cashflow reports use
// (revenue.Profitability / revenue.Cashflow) and carried through as an exact
// decimal STRING — no figure is recomputed in Go float (the net-margin % parses
// to float for the DISPLAY headline only, exactly as the merged reports do).
//
// SENSITIVE-METRIC GATING (blueprint §14): COGS / gross margin / net margin are
// supplier-cost-derived, so they are only surfaced (the KPI + the cost steps of
// the P&L waterfall + the per-product margin column) to an actor holding
// margin.view at the station — the SAME gate the Sales / Delivery reports use
// (canViewMarginAtStation). A non-margin actor sees revenue, expenses, the cash
// position and the non-cost statements, but never COGS or margin: those fields
// are OMITTED entirely (not zeroed), with a cost_shown:false flag + data-quality
// note. Locked-period + approval status are surfaced as data-quality.

// financeWaterfallStep is one step of the P&L cascade carried in chart_data: a
// label, an exact decimal-string value, the step kind (base | delta | total) and
// a negative flag (a deduction). The frontend FinancialWaterfall renders these.
type financeWaterfallStep struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Value    string `json:"value"`
	Kind     string `json:"kind"`               // base | delta | total
	Negative bool   `json:"negative,omitempty"` // a deduction (cost/expense)
}

// financeProductRow is one product's P&L contribution carried in chart_data.
// Cogs / Margin are *string so they are OMITTED entirely (not zeroed) for an
// actor without margin.view.
type financeProductRow struct {
	Product string  `json:"product"`
	Litres  string  `json:"litres"`
	Revenue string  `json:"revenue"`
	Cogs    *string `json:"cogs,omitempty"`
	Margin  *string `json:"margin,omitempty"`
}

// financeStatementLink surfaces an existing finance JSON sub-report (trial
// balance / P&L / balance sheet / GL) as an accessible link the page renders in
// a "financial statements" rail. Permission is the route's own gate.
type financeStatementLink struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Endpoint   string `json:"endpoint"`
	Permission string `json:"permission"`
}

// financeSettlementChip is one settlement-status chip (reusing the Phase 5 status
// board): an accounting period or a cash-settlement medium with a status word +
// tone. Amount is an exact decimal string (or empty).
type financeSettlementChip struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Tone   string `json:"tone"` // settled | pending | at_risk | neutral
	Detail string `json:"detail,omitempty"`
}

// financeChartData is the Finance report's report-specific chart payload: the P&L
// waterfall steps (the key net-new viz), the per-product breakdown (margin
// gated), the settlement/period status chips, the embedded finance statements,
// and the cost_shown gate flag.
type financeChartData struct {
	Waterfall   []financeWaterfallStep  `json:"waterfall"`
	ByProduct   []financeProductRow     `json:"by_product"`
	Settlements []financeSettlementChip `json:"settlements"`
	Statements  []financeStatementLink  `json:"statements"`
	CostShown   bool                    `json:"cost_shown"`
}

// handleFinanceReport returns the §5.8 Finance P&L report for a station over a
// period as a ReportEnvelope: a revenue / gross-margin / net-margin / expenses /
// cash-position KPI hero, the P&L waterfall (revenue → COGS → gross margin →
// expenses → net operating result), a per-product breakdown, settlement/period
// status chips, the embedded finance statements, the deterministic profitability
// insights and honest data-quality (locked-period + unapproved). Station-scoped,
// gated by finance.read; COGS / margin gated by margin.view in-handler.
func (s *Server) handleFinanceReport(w http.ResponseWriter, r *http.Request) {
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
	from, to, period := resolveReportPeriod(r.URL.Query().Get("period"), time.Now())
	sid := stationID.String()
	env := newEnvelope("finance", "Finance", period, &sid)
	env.FiltersUsed["station_id"] = sid
	env.FiltersUsed["period"] = period
	env.FiltersUsed["from"] = from.Format(dateLayout)
	env.FiltersUsed["to"] = to.Format(dateLayout)

	// COGS / margin are supplier-cost-derived (sensitive): only surface them when
	// the actor can read margin at this station. Decided once, applied everywhere.
	marginAllowed := s.canViewMarginAtStation(ctx, actor, stationID)

	totals, terr := s.revenue.Profitability(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	if terr != nil {
		s.logger.Error("finance report: profitability totals", "error", terr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	products, perr := s.revenue.ProfitabilityByProduct(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	if perr != nil {
		s.logger.Error("finance report: by product", "error", perr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	cash, cerr := s.revenue.Cashflow(ctx, s.deps.DB, actor.TenantID, stationID, from, to)
	if cerr != nil {
		s.logger.Error("finance report: cashflow", "error", cerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	days, unlocked, lerr := s.revenue.WindowLockState(ctx, actor.TenantID, stationID, from, to)
	if lerr != nil {
		s.logger.Error("finance report: lock state", "error", lerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// ---- period comparison: the immediately-preceding window of equal length ----
	prevFrom, prevTo := previousWindow(from, to)
	prevTotals, pErr := s.revenue.Profitability(ctx, s.deps.DB, actor.TenantID, stationID, prevFrom, prevTo)
	if pErr != nil {
		s.logger.Error("finance report: prev totals", "error", pErr)
		prevTotals = totals // fail soft: no delta rather than a 500
	}

	// ---- KPI hero (§5.8): revenue, gross margin, net margin, expenses, cash ----
	revDelta, revDir := growthVsPrevious(totals.Revenue, prevTotals.Revenue)
	env.Summary = []summaryMetric{
		{Label: "Net revenue", Value: totals.Revenue, Unit: "TZS", Delta: revDelta, Direction: revDir},
		{Label: "Operating expenses", Value: totals.Expenses, Unit: "TZS"},
		{Label: "Cash position", Value: cash.ProjectedCashPos, Unit: "TZS"},
	}
	if marginAllowed {
		// Gross margin + net margin are cost-derived → margin.view only.
		env.Summary = append(env.Summary,
			summaryMetric{Label: "Gross margin", Value: totals.GrossMargin, Unit: "TZS"},
			summaryMetric{Label: "Net operating result", Value: totals.NetOperating, Unit: "TZS"},
			summaryMetric{Label: "Net margin %", Value: pctOfTotal(totals.NetOperating, totals.Revenue)},
		)
	}

	// ---- chart_data: the P&L waterfall (key net-new viz) + product breakdown ----
	// Non-margin actors see revenue → expenses → net-of-expenses (no COGS/margin
	// steps, so cost never leaks); margin actors see the full cascade.
	var waterfall []financeWaterfallStep
	if marginAllowed {
		waterfall = []financeWaterfallStep{
			{Key: "revenue", Label: "Net revenue", Value: totals.Revenue, Kind: "base"},
			{Key: "cogs", Label: "COGS", Value: absMoney(totals.Cogs), Kind: "delta", Negative: true},
			{Key: "gross_margin", Label: "Gross margin", Value: totals.GrossMargin, Kind: "total"},
			{Key: "expenses", Label: "Operating expenses", Value: absMoney(totals.Expenses), Kind: "delta", Negative: true},
			{Key: "net_operating", Label: "Net operating result", Value: totals.NetOperating, Kind: "total"},
		}
	} else {
		waterfall = []financeWaterfallStep{
			{Key: "revenue", Label: "Net revenue", Value: totals.Revenue, Kind: "base"},
			{Key: "expenses", Label: "Operating expenses", Value: absMoney(totals.Expenses), Kind: "delta", Negative: true},
			{Key: "net_of_expenses", Label: "Revenue after expenses", Value: subMoney(totals.Revenue, totals.Expenses), Kind: "total"},
		}
	}

	prodRows := make([]financeProductRow, 0, len(products))
	for i := range products {
		p := products[i]
		row := financeProductRow{Product: p.ProductName, Litres: p.LitresSold, Revenue: p.Revenue}
		if marginAllowed {
			cogs, margin := p.Cogs, p.GrossMargin
			row.Cogs = &cogs
			row.Margin = &margin
		}
		prodRows = append(prodRows, row)
	}

	env.ChartData = financeChartData{
		Waterfall:   waterfall,
		ByProduct:   prodRows,
		Settlements: financeSettlementChips(ctx, s, actor.TenantID, from, to),
		Statements:  financeStatementLinks(from, to),
		CostShown:   marginAllowed,
	}

	// ---- drillable table: the per-product P&L (cost columns gated) ----
	if marginAllowed {
		env.Table.Columns = []string{"product", "litres", "revenue", "cogs", "gross_margin"}
	} else {
		env.Table.Columns = []string{"product", "litres", "revenue"}
	}
	for i := range products {
		p := products[i]
		row := []string{p.ProductName, p.LitresSold, p.Revenue}
		if marginAllowed {
			row = append(row, p.Cogs, p.GrossMargin)
		}
		env.Table.Rows = append(env.Table.Rows, row)
	}

	// ---- deterministic insights (reuse the §5.8 Profitability composer) ----
	env.applyReport(reporting.Profitability(reporting.ProfitabilityInput{
		NetRevenue:   totals.Revenue,
		Cogs:         totals.Cogs,
		GrossMargin:  totals.GrossMargin,
		Expenses:     totals.Expenses,
		NetOperating: totals.NetOperating,
		HasSales:     totals.SaleCount > 0,
		PeriodLocked: days > 0 && unlocked == 0,
	}))

	// ---- honest data-quality: empty window, unlocked days, gating, lock state ----
	if days == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level: "warning", Message: "No revenue days recorded for this station in the period.",
		})
	}
	if !marginAllowed {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "info",
			Message: "COGS, gross margin and net margin are hidden — they require the margin.view permission.",
		})
	}
	// Locked-period indicator: an accounting period whose window overlaps the
	// report range and is closed/locked means the books for this period are sealed.
	if note, ok := financeLockedPeriodNote(ctx, s, actor.TenantID, from, to); ok {
		env.DataQuality = append(env.DataQuality, dataQualityItem{Level: "info", Message: note})
	}

	// ---- drilldown to source (journals / expenses / sales) + statements ----
	env.Drilldown = []drilldownLink{
		{Label: "Profitability (by product)", Href: "/api/v1/reports/profitability?station_id=" + sid},
		{Label: "Credit & cashflow", Href: "/api/v1/reports/credit-cashflow?station_id=" + sid},
		{Label: "Trial balance (journals)", Href: "/api/v1/finance/reports/trial-balance"},
		{Label: "Profit & loss statement", Href: "/api/v1/finance/reports/profit-loss"},
		{Label: "Balance sheet", Href: "/api/v1/finance/reports/balance-sheet"},
		{Label: "Daily station close (sales)", Href: "/api/v1/reports/station-close?station_id=" + sid},
	}
	env.ExportOptions = []exportOption{
		{Format: "csv", URL: "/api/v1/reports/financials.csv?period=" + period},
		{Format: "xlsx", URL: "/api/v1/reports/financials.xlsx?period=" + period},
		{Format: "pdf", URL: "/api/v1/reports/financials.pdf?period=" + period},
	}
	writeJSON(w, http.StatusOK, env)
}

// financeStatementLinks builds the embedded finance JSON sub-report links (§5.8):
// the trial balance, P&L, balance sheet and general ledger. These statements are
// tenant-wide (the ledger has no station), so they take no station_id; each
// carries its own route permission so the page can gate the link. The GL needs
// an account_id, so it is surfaced without one (the page opens the GL picker).
func financeStatementLinks(from, to time.Time) []financeStatementLink {
	f := from.Format(dateLayout)
	t := to.Format(dateLayout)
	return []financeStatementLink{
		{Key: "profit-loss", Label: "Profit & Loss statement", Endpoint: "/api/v1/finance/reports/profit-loss?from=" + f + "&to=" + t, Permission: "finance.read"},
		{Key: "trial-balance", Label: "Trial Balance", Endpoint: "/api/v1/finance/reports/trial-balance?as_of=" + t, Permission: "finance.read"},
		{Key: "balance-sheet", Label: "Balance Sheet", Endpoint: "/api/v1/finance/reports/balance-sheet?as_of=" + t, Permission: "finance.read"},
		{Key: "general-ledger", Label: "General Ledger", Endpoint: "/api/v1/finance/reports/general-ledger", Permission: "finance.read"},
	}
}

// financeSettlementChips surfaces the accounting-period close/lock status as
// settlement chips (reusing the Phase 5 status-board vocabulary): the current and
// prior overlapping periods rendered with a settled/pending/at_risk tone. A
// failed lookup yields no chips (additive, never an error).
func financeSettlementChips(ctx context.Context, s *Server, tenantID uuid.UUID, from, to time.Time) []financeSettlementChip {
	periods, err := s.accounting.ListPeriods(ctx, tenantID)
	if err != nil {
		return []financeSettlementChip{}
	}
	chips := make([]financeSettlementChip, 0, 4)
	for i := range periods {
		p := periods[i]
		// Only the periods whose window overlaps the report range are relevant.
		if p.EndDate.Before(from) || p.StartDate.After(to) {
			continue
		}
		tone, status := financePeriodTone(p.Status)
		chips = append(chips, financeSettlementChip{
			Key:    p.ID.String(),
			Label:  p.StartDate.Format(dateLayout) + " – " + p.EndDate.Format(dateLayout),
			Status: status,
			Tone:   tone,
		})
		if len(chips) >= 4 {
			break
		}
	}
	return chips
}

// financePeriodTone maps an accounting-period status onto a status-board tone +
// display word: open is pending (books still moving), closed/locked is settled.
func financePeriodTone(status string) (tone, word string) {
	switch status {
	case accounting.PeriodLocked:
		return "settled", "Locked"
	case accounting.PeriodClosed:
		return "settled", "Closed"
	case accounting.PeriodClosing:
		return "pending", "Closing"
	default:
		return "pending", "Open"
	}
}

// financeLockedPeriodNote returns a data-quality note when an accounting period
// overlapping the report window is closed or locked (the books are sealed), so
// the reader knows the statement reflects a finalized period. Returns ok=false
// when no overlapping period is sealed.
func financeLockedPeriodNote(ctx context.Context, s *Server, tenantID uuid.UUID, from, to time.Time) (string, bool) {
	periods, err := s.accounting.ListPeriods(ctx, tenantID)
	if err != nil {
		return "", false
	}
	var sealed int
	for i := range periods {
		p := periods[i]
		if p.EndDate.Before(from) || p.StartDate.After(to) {
			continue
		}
		if p.Status == accounting.PeriodClosed || p.Status == accounting.PeriodLocked {
			sealed++
		}
	}
	if sealed == 0 {
		return "", false
	}
	return strconv.Itoa(sealed) + " accounting period(s) overlapping this window are closed or locked — those books are sealed and final.", true
}

// subMoney returns a − b as a decimal money string for the non-margin waterfall's
// "revenue after expenses" landing bar. Both inputs are decimal strings; the
// subtraction runs in integer cents (rounding each input to the nearest cent) so
// it cannot drift the way float subtraction would — the same discipline the
// frontend sumMoney helper uses. This is a presentational landing figure for the
// non-cost cascade, not a recomputation of a server P&L total (which margin
// actors get verbatim from SQL).
func subMoney(a, b string) string {
	ac, aok := moneyCents(a)
	bc, bok := moneyCents(b)
	if !aok || !bok {
		return a
	}
	return centsToMoney(ac - bc)
}

// moneyCents parses a decimal money string to integer cents (rounded), returning
// ok=false when unparsable. Used only for the presentational landing figure.
func moneyCents(v string) (int64, bool) {
	f, ok := parseFloatSafe(v)
	if !ok {
		return 0, false
	}
	if f < 0 {
		return -int64(-f*100 + 0.5), true
	}
	return int64(f*100 + 0.5), true
}

// centsToMoney renders integer cents as a 2dp decimal money string.
func centsToMoney(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	out := strconv.FormatInt(cents/100, 10) + "." + leftPad2(cents%100)
	if neg {
		return "-" + out
	}
	return out
}

// leftPad2 renders the cents remainder as a 2-digit string.
func leftPad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}
