package server

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/reporting"
	"github.com/japharyroman/fuelgrid-os/internal/revenue"
)

// Executive cockpit report (blueprint §5.1 / §20.1). The cross-domain rollup
// that consolidates Phases 2-9 into one drillable leadership view: a company-wide
// (or region/scope-wide) summary of revenue, litres, margin (gated), stock,
// loss (value gated), cash, credit (gated), risk and approvals, plus the
// DETERMINISTIC §5.1 automated management narrative and the reusable visuals.
//
// CRITICAL — this report REUSES the existing report figures; it does NOT
// recompute money. It calls the SAME repos the per-station structured reports
// call (revenue.StationComparison for the per-station revenue / litres / margin /
// net-operating / stock-variance / risk-alert / collections rollup;
// revenue.ProfitabilityByProduct for the product-growth narrative;
// revenue.WindowLockState for the provisional-day data-quality; the risk repo
// for open alerts / investigations; receivables for tenant-wide credit exposure)
// and AGGREGATES their exact decimal-string outputs. The only float coercion is
// the display-only headline summation every structured report already does
// (parseFloatSafe → a total string), never a persisted figure.
//
// SCOPE — the cockpit aggregates ONLY the actor's permitted station scope. A
// tenant-wide actor rolls up every station; a regional/station-restricted actor
// rolls up EXACTLY their granted stations (stationScope, the same gate the
// station-comparison report uses), so cross-scope leakage is impossible (a
// scoped manager can never see a station outside their grant — covered by the
// leakage test). Gated by finance.read held anywhere at the route.
//
// GATING — MARGIN, LOSS VALUE and CREDIT EXPOSURE are sensitive. Margin/loss
// value are gated by margin.view across the in-scope stations (omit, never
// zero); credit exposure is gated by customer_credit.read. A non-holder sees the
// non-sensitive figures in full plus a data-quality note explaining each
// omission.

// execStationRollup is one in-scope station's aggregated headline figures over
// the period, reused from the station-comparison rollup. Decimal strings for
// money/litres; the float fields are display-only sort keys.
type execStationRollup struct {
	row          revenue.StationComparisonRow
	netOperating float64 // parsed for ranking/sort only
}

// handleExecutiveReport returns the §5.1 / §20.1 Executive cockpit as a
// ReportEnvelope. Tenant-wide gate (finance.read held anywhere); the ROLLUP is
// restricted to the actor's accessible stations so cross-scope leakage is
// impossible. Margin / loss value / credit exposure are gated and OMITTED for
// non-holders.
func (s *Server) handleExecutiveReport(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx := r.Context()

	// Resolve the actor's permitted station scope — the SAME gate the
	// station-comparison report uses. A tenant-wide actor rolls up every station;
	// a restricted actor rolls up exactly their granted stations (and a restricted
	// actor with no grants is 403'd by the default-deny below).
	tenantWide, scope, scopeOK := s.stationScope(w, r, actor)
	if !scopeOK {
		return
	}
	stationIDs, ok := s.executiveScopeStations(w, ctx, actor, tenantWide, scope)
	if !ok {
		return
	}

	from, to, period := resolveReportPeriod(r.URL.Query().Get("period"), time.Now())
	priorFrom, priorTo, priorLabel := priorReportPeriod(period, time.Now())

	env := newEnvelope("executive", "Executive Business Report", period, nil)
	env.FiltersUsed["period"] = period
	env.FiltersUsed["from"] = from.Format(dateLayout)
	env.FiltersUsed["to"] = to.Format(dateLayout)
	env.FiltersUsed["stations_in_scope"] = strconv.Itoa(len(stationIDs))

	// MARGIN / LOSS VALUE gate: the actor may see margin only when they hold
	// margin.view at EVERY in-scope station (a station-scoped margin grant that
	// covers a subset would let an aggregate leak a station's margin the actor
	// can't see individually). Tenant-wide margin.view shows it; otherwise we
	// require the grant on all in-scope stations.
	marginShown := s.canViewMarginAcrossStations(ctx, actor, stationIDs)
	// exposurePermitted is the raw customer_credit.read grant; exposureShown is
	// additionally narrowed to tenant-wide actors below (AR can't be decomposed
	// per station). Keeping both lets the composer explain the omission reason.
	exposurePermitted := s.canViewCreditExposure(ctx, actor)
	exposureShown := exposurePermitted

	// ---- The cross-station rollup (CURRENT period) — reuse the station-comparison
	// repo so every figure is the SAME decimal string the per-station reports use.
	rows, rerr := s.revenue.StationComparison(ctx, actor.TenantID, stationIDs, from, to)
	if rerr != nil {
		s.logger.Error("executive report: station comparison (current)", "error", rerr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// PRIOR period, same scope — drives the period-over-period narrative.
	priorRows, perr := s.revenue.StationComparison(ctx, actor.TenantID, stationIDs, priorFrom, priorTo)
	if perr != nil {
		s.logger.Error("executive report: station comparison (prior)", "error", perr)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Aggregate the current-period decimal-string figures (display-only float sums,
	// exactly as every other structured report does for its headline aggregates).
	var totalRevenue, totalLitres, totalMargin, totalNetOp, totalExpenses, totalStockVar float64
	var stationsWithActivity, stationsNoData int
	stationLines := make([]reporting.ExecStationLine, 0, len(rows))
	rollups := make([]execStationRollup, 0, len(rows))
	for i := range rows {
		c := rows[i]
		rev, _ := parseFloatSafe(c.Revenue)
		lit, _ := parseFloatSafe(c.LitresSold)
		mar, _ := parseFloatSafe(c.GrossMargin)
		net, _ := parseFloatSafe(c.NetOperating)
		exp, _ := parseFloatSafe(c.Expenses)
		sv, _ := parseFloatSafe(c.StockVariance)
		totalRevenue += rev
		totalLitres += lit
		totalMargin += mar
		totalNetOp += net
		totalExpenses += exp
		totalStockVar += sv
		hasActivity := rev != 0 || lit != 0 || c.RiskAlerts > 0
		if hasActivity {
			stationsWithActivity++
		} else {
			stationsNoData++
		}
		label := c.StationCode
		if label == "" {
			label = c.StationName
		}
		stationLines = append(stationLines, reporting.ExecStationLine{
			Name: label, NetRevenue: c.Revenue, NetOperating: c.NetOperating, RiskAlerts: c.RiskAlerts,
		})
		rollups = append(rollups, execStationRollup{row: c, netOperating: net})
	}

	priorRevenue, priorMargin, priorNetOp, priorLitres := sumPriorTotals(priorRows)

	// ---- Top / underperforming stations (by net operating result) ----
	top, weak := topAndWeakStations(stationLines, rollups)

	// ---- Product growth (the fastest-growing-product narrative) — reuse
	// ProfitabilityByProduct over the scope, current vs prior litres per product.
	products, prodErr := s.executiveProductGrowth(ctx, actor.TenantID, stationIDs, from, to, priorFrom, priorTo)
	if prodErr != nil {
		s.logger.Error("executive report: product growth", "error", prodErr)
		// Non-fatal: the product sentence is simply omitted (honest partial state).
		products = nil
	}
	fastest, _ := reporting.FastestGrowingProduct(products)

	// ---- Loss litres + value (gated), cash shortages, stockout risk, approvals,
	// open alerts/investigations across the scope ----
	lossLitres, lossValue := s.executiveLossTotals(ctx, actor.TenantID, stationIDs, marginShown, from, to)
	cashShortages := s.executiveCashShortages(ctx, actor.TenantID, stationIDs, from, to)
	stockoutRisk := s.executiveStockoutRisk(ctx, actor.TenantID, stationIDs)
	pendingApprovals, unlockedDays := s.executiveApprovals(ctx, actor.TenantID, stationIDs, from, to)
	// Open investigations are risk.read/investigation.read-gated, tenant-wide data
	// (cases carry no station id); count them only for an actor permitted to see
	// investigations, so a scoped/under-permissioned cockpit never leaks the whole
	// tenant's open-case volume. investShown gates the KPI + narrative focus.
	investShown := s.canViewInvestigations(ctx, actor)
	openAlerts, openInvest := s.executiveRiskCounts(ctx, actor.TenantID, stationIDs, tenantWide, investShown)
	supplierIssues := s.executiveSupplierIssues(ctx, actor.TenantID, stationIDs, from, to)

	// ---- Credit exposure. AR is a CUSTOMER ledger (ar_entries is keyed by
	// customer, with no station column), so it is inherently tenant-level and
	// cannot be correctly decomposed per station. A tenant-wide actor sees the
	// authoritative ar_entries aging; for a SCOPED actor we OMIT the figure rather
	// than present a station-approximation (sum of in-scope invoices' outstanding)
	// that would not reconcile with the tenant-wide ar_entries total — surfacing
	// two different numbers to a CFO vs a regional manager undermines the cockpit.
	// Gated by customer_credit.read AND tenant-wide reach; a DQ note explains the
	// omission for scoped actors.
	exposureShown = exposureShown && tenantWide
	creditExposure := s.executiveCreditExposure(ctx, actor.TenantID, tenantWide)

	// ---- Assemble the composer input + the deterministic narrative ----
	in := reporting.ExecutiveInput{
		Period:             period,
		PriorPeriod:        priorLabel,
		StationCount:       stationsWithActivity,
		Revenue:            reporting.ExecMetricDelta{Current: f2(totalRevenue), Prior: f2(priorRevenue)},
		Litres:             reporting.ExecMetricDelta{Current: f3(totalLitres), Prior: f3(priorLitres)},
		GrossMargin:        reporting.ExecMetricDelta{Current: f2(totalMargin), Prior: f2(priorMargin)},
		NetMargin:          reporting.ExecMetricDelta{Current: f2(totalNetOp), Prior: f2(priorNetOp)},
		MarginShown:        marginShown,
		LossLitres:         lossLitres,
		LossValue:          lossValue,
		LossValueShown:     marginShown,
		CreditExposure:     creditExposure,
		ExposureShown:      exposureShown,
		ExposurePermitted:  exposurePermitted,
		CashShortages:      cashShortages,
		StockoutRisk:       stockoutRisk,
		OpenAlerts:         openAlerts,
		OpenInvestigations: openInvest,
		PendingApprovals:   pendingApprovals,
		SupplierIssues:     supplierIssues,
		FastestProduct:     fastest,
		TopStation:         top,
		WeakStation:        weak,
		UnlockedDays:       unlockedDays,
		StationsNoData:     stationsNoData,
		Scoped:             !tenantWide,
	}
	narrative := reporting.ExecutiveNarrativeText(in)

	// ---- KPI hero (§5.1). Sensitive metrics are OMITTED (not zeroed) for
	// non-holders, with a data-quality note from the composer explaining each. ----
	env.Summary = []summaryMetric{
		{Label: "Total revenue", Value: f2(totalRevenue), Unit: "TZS"},
		{Label: "Total litres", Value: f3(totalLitres), Unit: "L"},
	}
	if marginShown {
		env.Summary = append(env.Summary,
			summaryMetric{Label: "Gross margin", Value: f2(totalMargin), Unit: "TZS"},
			summaryMetric{Label: "Net margin", Value: f2(totalNetOp), Unit: "TZS"},
		)
	}
	env.Summary = append(env.Summary,
		summaryMetric{Label: "Total loss litres", Value: lossLitres, Unit: "L"},
	)
	if marginShown {
		env.Summary = append(env.Summary, summaryMetric{Label: "Loss value", Value: lossValue, Unit: "TZS"})
	}
	env.Summary = append(env.Summary,
		summaryMetric{Label: "Cash shortages", Value: cashShortages, Unit: "TZS"},
		summaryMetric{Label: "Stockout risk", Value: strconv.Itoa(stockoutRisk), Unit: "count"},
		summaryMetric{Label: "Open risk alerts", Value: strconv.Itoa(openAlerts), Unit: "count"},
	)
	if investShown {
		env.Summary = append(env.Summary,
			summaryMetric{Label: "Open investigations", Value: strconv.Itoa(openInvest), Unit: "count"})
	}
	env.Summary = append(env.Summary,
		summaryMetric{Label: "Pending approvals", Value: strconv.Itoa(pendingApprovals), Unit: "count"},
		summaryMetric{Label: "Supplier issues", Value: strconv.Itoa(supplierIssues), Unit: "count"},
		summaryMetric{Label: "Stations in scope", Value: strconv.Itoa(len(stationIDs)), Unit: "count"},
	)
	if exposureShown {
		env.Summary = append(env.Summary, summaryMetric{Label: "Credit exposure", Value: creditExposure, Unit: "TZS"})
	}
	if top != nil {
		env.Summary = append(env.Summary, summaryMetric{Label: "Top station", Value: top.Name})
	}
	if weak != nil {
		env.Summary = append(env.Summary, summaryMetric{Label: "Underperforming station", Value: weak.Name})
	}

	// ---- Compose the deterministic insights + data-quality ----
	env.applyReport(reporting.Executive(in))
	if !investShown {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "info",
			Message: "Open investigations are hidden — they require the investigation.read permission.",
		})
	}

	// ---- Chart payload: the cross-domain visuals (revenue+volume trend, P&L
	// waterfall steps, station ranking, period-comparison cards, loss summary). ----
	env.ChartData = s.buildExecutiveChart(in, rollups, marginShown, narrative,
		totalRevenue, totalMargin, totalExpenses, totalNetOp, totalStockVar)

	// ---- Drillable table: per-station rollup (the network league table) ----
	env.Table.Columns = []string{"station", "revenue", "litres", "net_operating", "stock_variance", "risk_alerts", "collections"}
	for i := range rollups {
		c := rollups[i].row
		label := c.StationCode
		if label == "" {
			label = c.StationName
		}
		env.Table.Rows = append(env.Table.Rows, []string{
			label, c.Revenue, c.LitresSold, c.NetOperating, c.StockVariance,
			strconv.Itoa(c.RiskAlerts), c.Collections,
		})
	}

	if len(rollups) == 0 {
		env.DataQuality = append(env.DataQuality, dataQualityItem{
			Level:   "warning",
			Message: "No stations in scope have recognized activity for this period — the cockpit is empty.",
		})
	}

	// ---- Drilldown into the per-domain reports (no dangling 403 routes) ----
	env.Drilldown = []drilldownLink{
		{Label: "Station comparison", Href: "/api/v1/reports/station-comparison?period=" + period},
		{Label: "Customer credit", Href: "/api/v1/reports/customer-credit?period=" + period},
		{Label: "Enterprise overview", Href: "/api/v1/enterprise/overview"},
		{Label: "Risk alerts", Href: "/api/v1/risk/alerts?status=open"},
	}
	env.ExportOptions = []exportOption{
		{Format: "pdf", URL: "/api/v1/reports/financials.pdf?period=" + period},
		{Format: "xlsx", URL: "/api/v1/reports/financials.xlsx?period=" + period},
		{Format: "csv", URL: "/api/v1/reports/financials.csv?period=" + period},
	}
	writeJSON(w, http.StatusOK, env)
}

// executiveScopeStations resolves the station id slice the cockpit rolls up: every
// station for a tenant-wide actor, exactly the granted stations for a restricted
// one. A restricted actor with no grants is 403'd (the AUTH-20 default-deny), so
// cross-scope leakage is impossible. Returns ok=false after writing the error.
func (s *Server) executiveScopeStations(w http.ResponseWriter, ctx context.Context, actor identity.Actor, tenantWide bool, scope []uuid.UUID) ([]uuid.UUID, bool) {
	if tenantWide {
		all, lerr := s.stations.List(ctx, actor.TenantID, nil, nil)
		if lerr != nil {
			s.logger.Error("executive report: list stations", "error", lerr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return nil, false
		}
		ids := make([]uuid.UUID, 0, len(all))
		for i := range all {
			ids = append(ids, all[i].ID)
		}
		return ids, true
	}
	if len(scope) == 0 {
		writeError(w, http.StatusForbidden, "no station access")
		return nil, false
	}
	return scope, true
}

// canViewMarginAcrossStations reports whether the actor may see MARGIN (and the
// margin-gated LOSS VALUE) in an aggregate over the supplied stations. A
// tenant-wide margin.view holder always may; a station-scoped holder may only
// when the grant covers EVERY in-scope station (otherwise the aggregate would
// leak a station's margin the actor can't see on its own). This reuses the
// per-station canViewMarginAtStation gate, applied to all in-scope stations.
func (s *Server) canViewMarginAcrossStations(ctx context.Context, actor identity.Actor, stationIDs []uuid.UUID) bool {
	if len(stationIDs) == 0 {
		return false
	}
	for _, id := range stationIDs {
		if !s.canViewMarginAtStation(ctx, actor, id) {
			return false
		}
	}
	return true
}

// executiveProductGrowth aggregates per-product litres across the in-scope
// stations for the current and prior windows, returning the current/prior litres
// per product for the fastest-growing-product narrative. It reuses
// ProfitabilityByProduct (the same per-product P&L the profitability report
// uses); every litre is an exact decimal string summed in SQL — the only float
// here is the display-only per-product running total. Money/COGS are ignored
// (not needed for a litre-growth comparison and not narrated).
func (s *Server) executiveProductGrowth(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID, from, to, priorFrom, priorTo time.Time) ([]reporting.ExecProductGrowth, error) {
	cur := map[string]float64{}
	prior := map[string]float64{}
	for _, sid := range stationIDs {
		c, err := s.revenue.ProfitabilityByProduct(ctx, s.deps.DB, tenantID, sid, from, to)
		if err != nil {
			return nil, err
		}
		for i := range c {
			if v, ok := parseFloatSafe(c[i].LitresSold); ok {
				cur[c[i].ProductName] += v
			}
		}
		p, err := s.revenue.ProfitabilityByProduct(ctx, s.deps.DB, tenantID, sid, priorFrom, priorTo)
		if err != nil {
			return nil, err
		}
		for i := range p {
			if v, ok := parseFloatSafe(p[i].LitresSold); ok {
				prior[p[i].ProductName] += v
			}
		}
	}
	names := make([]string, 0, len(cur))
	for name := range cur {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic order before the composer's stable sort
	out := make([]reporting.ExecProductGrowth, 0, len(names))
	for _, name := range names {
		out = append(out, reporting.ExecProductGrowth{
			Name: name, Current: f3(cur[name]), Prior: f3(prior[name]),
		})
	}
	return out, nil
}

// executiveLossTotals sums fuel-loss litres (always) and loss value (gated) over
// the in-scope stations from the reconciliation variance history — the SAME
// shortage definition the fuel-loss/risk-loss reports use (negative variance,
// priced in SQL). The events are restricted to the report's PERIOD window (the
// operating day's business_date) so the loss figure matches the period header and
// every other windowed cockpit figure (revenue / litres / margin / stock
// variance), rather than reading as an all-time lifetime total. Loss VALUE is
// summed ONLY when marginShown, so a non-holder never receives a money figure;
// loss LITRES are always returned.
func (s *Server) executiveLossTotals(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID, marginShown bool, from, to time.Time) (litres, value string) {
	if len(stationIDs) == 0 {
		return "0.000", ""
	}
	var l, v float64
	_ = s.deps.DB.QueryRow(ctx, `
		WITH ev AS (
			SELECT tr.variance_litres,
			       CASE WHEN p.default_price > 0 THEN p.default_price ELSE NULL END AS price
			FROM tank_reconciliations tr
			JOIN tanks t          ON t.id = tr.tank_id AND t.tenant_id = tr.tenant_id
			JOIN operating_days od ON od.id = tr.operating_day_id AND od.tenant_id = tr.tenant_id
			JOIN products p        ON p.id = t.product_id AND p.tenant_id = t.tenant_id
			WHERE tr.tenant_id = $1 AND od.station_id = ANY($2) AND tr.variance_litres < 0
			  AND od.business_date BETWEEN $3 AND $4
		)
		SELECT COALESCE(SUM(ABS(variance_litres)), 0)::float8,
		       COALESCE(SUM(CASE WHEN price IS NOT NULL THEN ABS(variance_litres) * price ELSE 0 END), 0)::float8
		FROM ev
	`, tenantID, stationIDs, from, to).Scan(&l, &v)
	litres = f3(l)
	if marginShown {
		value = f2(v)
	}
	return litres, value
}

// executiveCashShortages sums the absolute cash shortage (counted shortfall) over
// the in-scope stations' counted reconciliations — the same counted definition
// the cash-reconciliation report uses (submitted/approved/posted only). The
// reconciliations are restricted to the report's PERIOD window (the operating
// day's business_date), so the figure matches the period header and the other
// windowed cockpit metrics instead of being an all-time lifetime total. Returns a
// decimal string.
func (s *Server) executiveCashShortages(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID, from, to time.Time) string {
	if len(stationIDs) == 0 {
		return "0.00"
	}
	var shortage float64
	_ = s.deps.DB.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN cr.variance < 0 THEN ABS(cr.variance) ELSE 0 END), 0)::float8
		FROM cash_reconciliations cr
		JOIN operating_days od ON od.id = cr.operating_day_id AND od.tenant_id = cr.tenant_id
		WHERE cr.tenant_id = $1 AND cr.station_id = ANY($2)
		  AND cr.status IN ('submitted', 'approved', 'posted')
		  AND od.business_date BETWEEN $3 AND $4
	`, tenantID, stationIDs, from, to).Scan(&shortage)
	return f2(shortage)
}

// executiveStockoutRisk counts tanks at stockout risk across the in-scope
// stations — an active tank whose latest physical dip is at or below its
// configured safe-minimum (safe_min_litres) volume, or, when no safe-min is set,
// at or below its dead-stock floor. Honest partial state: a tank with no dip
// recorded never counts (no false positives). Pure count, no money.
func (s *Server) executiveStockoutRisk(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID) int {
	if len(stationIDs) == 0 {
		return 0
	}
	var n int
	_ = s.deps.DB.QueryRow(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (d.tank_id) d.tank_id, d.volume_litres
			FROM tank_dip_readings d
			JOIN tanks t ON t.id = d.tank_id AND t.tenant_id = d.tenant_id
			WHERE d.tenant_id = $1 AND d.status = 'active' AND t.station_id = ANY($2)
			ORDER BY d.tank_id, d.recorded_at DESC
		)
		SELECT COUNT(*)
		FROM latest l
		JOIN tanks t ON t.id = l.tank_id AND t.tenant_id = $1
		WHERE t.status = 'active'
		  AND l.volume_litres <= GREATEST(t.safe_min_litres, t.dead_stock_litres)
		  AND GREATEST(t.safe_min_litres, t.dead_stock_litres) > 0
	`, tenantID, stationIDs).Scan(&n)
	return n
}

// executiveApprovals counts two DISTINCT sign-off signals over the in-scope
// stations in the window: pending approvals — closed shifts that have not yet been
// approved (a real approval queue: status 'closed', not 'approved'/'open') — and
// the unlocked-revenue-day count (operating days not yet locked: the provisional
// figures data-quality signal). These are different predicates over different
// tables, so the two figures are independent (a previous version derived both
// from the same `od.status <> 'locked'` filter, making them trivially identical
// and the "approvals" KPI uninformative). Reuses the rollup's business-date window.
func (s *Server) executiveApprovals(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID, from, to time.Time) (pending, unlockedDays int) {
	if len(stationIDs) == 0 {
		return 0, 0
	}
	_ = s.deps.DB.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM shifts sh
		JOIN operating_days od ON od.id = sh.operating_day_id AND od.tenant_id = sh.tenant_id
		WHERE sh.tenant_id = $1 AND sh.station_id = ANY($2)
		  AND sh.status = 'closed'
		  AND od.business_date BETWEEN $3 AND $4
	`, tenantID, stationIDs, from, to).Scan(&pending)
	_ = s.deps.DB.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM operating_days od
		WHERE od.tenant_id = $1 AND od.station_id = ANY($2)
		  AND od.status <> 'locked'
		  AND od.business_date BETWEEN $3 AND $4
	`, tenantID, stationIDs, from, to).Scan(&unlockedDays)
	return pending, unlockedDays
}

// executiveRiskCounts counts open risk alerts (attributed to in-scope stations)
// and open investigation cases. Alerts carry a nullable station id; only alerts
// whose station is in scope are counted, so a scoped actor never sees an
// out-of-scope station's alerts. A tenant-wide actor counts alerts with no
// station too (tenant-level alerts). Investigation cases carry no station id (the
// list is inherently tenant-wide), so the open-investigation count is computed
// ONLY when investShown — an actor holding investigation.read; otherwise it is
// left at 0 and omitted upstream, so a scoped/under-permissioned cockpit never
// leaks the whole tenant's open-case volume.
func (s *Server) executiveRiskCounts(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID, tenantWide, investShown bool) (openAlerts, openInvest int) {
	inScope := map[uuid.UUID]bool{}
	for _, id := range stationIDs {
		inScope[id] = true
	}
	if alerts, aerr := s.risk.ListAlerts(ctx, tenantID, "open", ""); aerr == nil {
		for i := range alerts {
			if alerts[i].StationID == nil {
				// A tenant-level alert (no station) counts only for a tenant-wide actor.
				if tenantWide {
					openAlerts++
				}
				continue
			}
			if inScope[*alerts[i].StationID] {
				openAlerts++
			}
		}
	}
	if investShown {
		if cases, cerr := s.risk.ListCases(ctx, tenantID, ""); cerr == nil {
			for i := range cases {
				if !caseTerminal(cases[i].Status) {
					openInvest++
				}
			}
		}
	}
	return openAlerts, openInvest
}

// executiveSupplierIssues counts delivery shortfalls over the in-scope stations
// in the window — a delivery whose measured dip variance is a material shortfall
// (received less than the gauge expected). This is the SAME shortfall signal the
// delivery report surfaces (deliveries.dip_variance_litres < 0). Deliveries are
// scoped to a station through their tank; the window uses received_at. Honest
// partial state: 0 when no deliveries. Pure count, no money.
func (s *Server) executiveSupplierIssues(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID, from, to time.Time) int {
	if len(stationIDs) == 0 {
		return 0
	}
	var n int
	_ = s.deps.DB.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM deliveries d
		JOIN tanks t ON t.id = d.tank_id AND t.tenant_id = d.tenant_id
		WHERE d.tenant_id = $1 AND t.station_id = ANY($2)
		  AND d.received_at::date BETWEEN $3 AND $4
		  AND d.dip_variance_litres IS NOT NULL
		  AND d.dip_variance_litres < 0
	`, tenantID, stationIDs, from, to).Scan(&n)
	return n
}

// executiveCreditExposure returns the tenant's authoritative credit exposure from
// the ar_entries aging ledger (the customer AR balance). It is only computed for a
// tenant-wide actor — AR is a customer ledger with no station column, so it cannot
// be correctly decomposed per station; a scoped actor's exposure is omitted by the
// caller (exposureShown && tenantWide) rather than approximated. Decimal string.
func (s *Server) executiveCreditExposure(ctx context.Context, tenantID uuid.UUID, tenantWide bool) string {
	if !tenantWide {
		return ""
	}
	var total float64
	if rows, rerr := s.receivables.Aging(ctx, tenantID); rerr == nil {
		for i := range rows {
			if v, ok := parseFloatSafe(rows[i].Balance); ok && v > 0 {
				total += v
			}
		}
	}
	return f2(total)
}

// sumPriorTotals aggregates the prior-period station-comparison rows into the
// scope-wide revenue / margin / net-operating / litres totals (display floats).
func sumPriorTotals(rows []revenue.StationComparisonRow) (revenueT, marginT, netOpT, litresT float64) {
	for i := range rows {
		if v, ok := parseFloatSafe(rows[i].Revenue); ok {
			revenueT += v
		}
		if v, ok := parseFloatSafe(rows[i].GrossMargin); ok {
			marginT += v
		}
		if v, ok := parseFloatSafe(rows[i].NetOperating); ok {
			netOpT += v
		}
		if v, ok := parseFloatSafe(rows[i].LitresSold); ok {
			litresT += v
		}
	}
	return revenueT, marginT, netOpT, litresT
}

// topAndWeakStations selects the strongest and weakest in-scope station by net
// operating result (parsed for ranking only). Deterministic: ties break by name.
// Returns nils when there are no stations to rank.
func topAndWeakStations(lines []reporting.ExecStationLine, rollups []execStationRollup) (top, weak *reporting.ExecStationLine) {
	if len(lines) == 0 {
		return nil, nil
	}
	idx := make([]int, len(lines))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		if rollups[idx[a]].netOperating != rollups[idx[b]].netOperating {
			return rollups[idx[a]].netOperating > rollups[idx[b]].netOperating
		}
		return lines[idx[a]].Name < lines[idx[b]].Name
	})
	t := lines[idx[0]]
	w := lines[idx[len(idx)-1]]
	return &t, &w
}

// f2 / f3 format a display-only float aggregate as a 2- or 3-dp decimal string,
// matching the money (2dp) / litre (3dp) conventions the other reports use.
func f2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
func f3(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }

// priorReportPeriod resolves the prior comparison window for a given period
// label: the calendar month before this-month, the month before last-month, the
// prior 30 days for last-30, and the prior full year for ytd. The label is the
// human prior-period name used in the narrative.
func priorReportPeriod(period string, now time.Time) (from, to time.Time, label string) {
	curFrom, curTo, _ := resolveReportPeriod(period, now)
	switch period {
	case "last-month":
		// The month before last month.
		prevStart := curFrom.AddDate(0, -1, 0)
		return prevStart, curFrom.AddDate(0, 0, -1), "prior month"
	case "ytd":
		y := now.Year()
		return time.Date(y-1, 1, 1, 0, 0, 0, 0, now.Location()),
			time.Date(y-1, 12, 31, 0, 0, 0, 0, now.Location()), "prior year"
	case "last-30":
		span := curTo.Sub(curFrom)
		return curFrom.Add(-span - 24*time.Hour), curFrom.AddDate(0, 0, -1), "prior 30 days"
	default: // this-month → the previous calendar month
		prevStart := curFrom.AddDate(0, -1, 0)
		return prevStart, curFrom.AddDate(0, 0, -1), "last month"
	}
}
