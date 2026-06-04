package reporting

import (
	"fmt"
	"math"
)

// This file holds the report-specific composers: each takes a small,
// already-computed input struct (built by the HTTP handler from existing
// overview/service data) and returns the Report (insights + data-quality) for
// one report key. Every rule is a transparent, deterministic threshold — no
// configuration, no engine. The set covers the ~8-10 signature checks:
//
//   1. period-over-period gross delta            (sales / daily-close)
//   2. variance-vs-recent-average on gross       (sales / daily-close)
//   3. cash variance over tolerance              (daily-close / cash-recon)
//   4. unclosed shifts → figures may change      (daily-close, data-quality)
//   5. tank reconciliation over tolerance        (stock-recon)
//   6. missing physical dips → book-only         (stock-recon, data-quality)
//   7. negative / shrinking margin               (sales)
//   8. overdue / concentrated receivables        (customer-aging)
//   9. unbalanced books                          (finance, data-quality)
//  10. period not yet locked → provisional       (any, data-quality)

// ---- Daily Station Close ----

// DailyCloseInput is the already-computed slice of a station's day a handler
// passes in. GrossSeries is the recent-day trend (oldest→newest) used for the
// delta/variance heuristics; the remaining fields describe the focus day.
type DailyCloseInput struct {
	GrossSeries        []PeriodPoint
	CashVariance       string // decimal string; signed
	CashVarianceTol    string // absolute tolerance, decimal string ("" => default)
	UnclosedShiftCount int    // shifts on the day not yet closed/approved
	DayLocked          bool   // the revenue day is locked/sealed
}

// DailyClose builds the Daily Station Close report annotations.
func DailyClose(in DailyCloseInput) Report {
	var rep Report
	if ins, ok := PeriodOverPeriod("Gross revenue", in.GrossSeries); ok {
		rep.Insights = append(rep.Insights, ins)
	}
	if ins, ok := VarianceVs30dAverage("Gross revenue", in.GrossSeries, 20); ok {
		rep.Insights = append(rep.Insights, ins)
	}
	if ins, ok := cashVarianceInsight(in.CashVariance, in.CashVarianceTol); ok {
		rep.Insights = append(rep.Insights, ins)
	}
	if in.UnclosedShiftCount > 0 {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: fmt.Sprintf(
				"%d shift(s) on this day are not yet closed — the figures may still change.",
				in.UnclosedShiftCount),
		})
	}
	if !in.DayLocked {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "This day is not locked yet, so its totals are provisional.",
		})
	}
	return rep
}

// cashVarianceInsight flags an over-tolerance cash variance. tol defaults to a
// conservative absolute floor when blank.
func cashVarianceInsight(variance, tol string) (Insight, bool) {
	v, ok := parseDec(variance)
	if !ok || v == 0 {
		return Insight{}, false
	}
	limit, hasLimit := parseDec(tol)
	if !hasLimit || limit <= 0 {
		limit = 0 // any non-zero variance is at least info
	}
	abs := math.Abs(v)
	if abs <= limit {
		return Insight{}, false
	}
	dir := "over"
	if v < 0 {
		dir = "short"
	}
	sev := SeverityWarning
	if limit > 0 && abs > limit*2 {
		sev = SeverityCritical
	}
	return Insight{
		Severity:          sev,
		Message:           fmt.Sprintf("Cash drawer is %s by %s — beyond tolerance.", dir, variance),
		RecommendedAction: "Reconcile the drawer and confirm tender breakdown before locking the day.",
	}, true
}

// ---- Sales Summary ----

// SalesInput captures the gross + margin trend for the sales summary.
type SalesInput struct {
	GrossSeries  []PeriodPoint
	MarginSeries []PeriodPoint // aligned with GrossSeries, latest last
	PeriodLocked bool
}

// SalesSummary builds the Sales Summary annotations.
func SalesSummary(in SalesInput) Report {
	var rep Report
	if ins, ok := PeriodOverPeriod("Gross revenue", in.GrossSeries); ok {
		rep.Insights = append(rep.Insights, ins)
	}
	if ins, ok := VarianceVs30dAverage("Gross revenue", in.GrossSeries, 20); ok {
		rep.Insights = append(rep.Insights, ins)
	}
	if ins, ok := marginInsight(in.MarginSeries); ok {
		rep.Insights = append(rep.Insights, ins)
	}
	if !in.PeriodLocked {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "The latest period is still open — sales figures are provisional.",
		})
	}
	return rep
}

// marginInsight flags a negative latest margin (critical) or a margin shrinking
// vs the prior period (warning).
func marginInsight(series []PeriodPoint) (Insight, bool) {
	n := len(series)
	if n == 0 {
		return Insight{}, false
	}
	cur, ok := parseDec(series[n-1].Value)
	if !ok {
		return Insight{}, false
	}
	if cur < 0 {
		return Insight{
			Severity:          SeverityCritical,
			Message:           "Latest margin is negative — sales are running below cost.",
			RecommendedAction: "Review pump pricing and COGS for the period.",
		}, true
	}
	if n >= 2 {
		if prev, ok := parseDec(series[n-2].Value); ok {
			if p, ok := pctChange(cur, prev); ok && p <= -15 {
				return Insight{
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("Margin contracted %s vs the prior period.", fmtPct(p)),
				}, true
			}
		}
	}
	return Insight{}, false
}

// ---- Fuel Stock Reconciliation ----

// TankRecon is one tank's already-computed reconciliation line.
type TankRecon struct {
	TankLabel        string
	VariancePercent  string // signed decimal string
	TolerancePercent string // decimal string
	Status           string // e.g. "over_tolerance", "within_tolerance"
	HasPhysicalDip   bool
}

// StockReconInput bundles the tanks and day-close state.
type StockReconInput struct {
	Tanks           []TankRecon
	AllShiftsClosed bool
	DayLocked       bool
}

// StockReconciliation builds the Fuel Stock Reconciliation annotations.
func StockReconciliation(in StockReconInput) Report {
	var rep Report
	var missingDips int
	for _, t := range in.Tanks {
		if tankOverTolerance(t) {
			rep.Insights = append(rep.Insights, Insight{
				Severity: SeverityWarning,
				Message: fmt.Sprintf(
					"%s variance %s exceeds its %s tolerance.",
					t.TankLabel, withPct(t.VariancePercent), withPct(t.TolerancePercent)),
				RecommendedAction: "Investigate possible loss, theft, or a miscalibrated dip.",
			})
		}
		if !t.HasPhysicalDip {
			missingDips++
		}
	}
	if missingDips > 0 {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: fmt.Sprintf(
				"%d tank(s) have no physical dip recorded — those lines are book-only.",
				missingDips),
		})
	}
	if !in.AllShiftsClosed {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "Not all shifts are closed — book balances may still change.",
		})
	}
	return rep
}

// tankOverTolerance is true when the status marks it, or |variance| > tolerance.
func tankOverTolerance(t TankRecon) bool {
	if t.Status == "over_tolerance" {
		return true
	}
	v, okV := parseDec(t.VariancePercent)
	tol, okT := parseDec(t.TolerancePercent)
	if !okV || !okT || tol <= 0 {
		return false
	}
	return math.Abs(v) > tol
}

func withPct(s string) string {
	if s == "" {
		return "0%"
	}
	return s + "%"
}

// ---- Cash Reconciliation ----

// CashReconInput captures a station's cash variance + close state.
type CashReconInput struct {
	Variance     string
	Tolerance    string
	GrossSeries  []PeriodPoint // cash-tender trend for context
	PeriodLocked bool
}

// CashReconciliation builds the Cash Reconciliation annotations.
func CashReconciliation(in CashReconInput) Report {
	var rep Report
	if ins, ok := cashVarianceInsight(in.Variance, in.Tolerance); ok {
		rep.Insights = append(rep.Insights, ins)
	}
	if ins, ok := PeriodOverPeriod("Cash collected", in.GrossSeries); ok {
		rep.Insights = append(rep.Insights, ins)
	}
	if !in.PeriodLocked {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "The period is not locked — the cash position is provisional.",
		})
	}
	return rep
}

// ---- Customer Aging ----

// AgingCustomer is one credit customer's outstanding balance.
type AgingCustomer struct {
	Name    string
	Balance string
}

// CustomerAgingInput bundles all customers with a balance.
type CustomerAgingInput struct {
	Customers []AgingCustomer
}

// CustomerAging builds the Customer Aging annotations: total exposure, a
// concentration warning when one customer dominates, and a count summary.
func CustomerAging(in CustomerAgingInput) Report {
	var rep Report
	var total float64
	var top float64
	var topName string
	var withBalance int
	for _, c := range in.Customers {
		v, ok := parseDec(c.Balance)
		if !ok || v <= 0 {
			continue
		}
		withBalance++
		total += v
		if v > top {
			top, topName = v, c.Name
		}
	}
	if withBalance == 0 || total <= 0 {
		return rep
	}
	rep.Insights = append(rep.Insights, Insight{
		Severity: SeverityInfo,
		Message:  fmt.Sprintf("%d customer(s) carry an outstanding receivable balance.", withBalance),
	})
	if share := top / total * 100; share >= 50 && withBalance > 1 {
		rep.Insights = append(rep.Insights, Insight{
			Severity: SeverityWarning,
			Message: fmt.Sprintf(
				"%s alone holds %.0f%% of total receivables — concentrated credit risk.",
				topName, share),
			RecommendedAction: "Review the customer's credit limit and chase the overdue balance.",
		})
	}
	return rep
}

// ---- Profitability (Feature 10.4) ----

// ProfitabilityInput is a station's already-computed P&L slice over a window.
// Every field is an exact decimal string (summed in SQL ::numeric); the
// heuristics below parse them to float64 for DISPLAY math only.
type ProfitabilityInput struct {
	NetRevenue   string // net (ex-tax) revenue recognized
	Cogs         string // cost of goods sold
	GrossMargin  string // net revenue − COGS
	Expenses     string // operating expenses booked to the station
	NetOperating string // gross margin − operating expenses
	HasSales     bool   // any recognized sales in the window
	PeriodLocked bool   // every revenue day in the window is locked
}

// Profitability builds the Profitability report annotations: a negative net
// operating result is critical; a thin gross margin (< 5% of revenue) warns;
// expenses outrunning gross margin warns; and an unlocked window is flagged as
// provisional data-quality.
func Profitability(in ProfitabilityInput) Report {
	var rep Report
	if !in.HasSales {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "No recognized sales in this period — profitability figures are empty.",
		})
		return rep
	}
	net, okNet := parseDec(in.NetOperating)
	rev, okRev := parseDec(in.NetRevenue)
	margin, okMargin := parseDec(in.GrossMargin)
	expenses, okExp := parseDec(in.Expenses)

	switch {
	case okNet && net < 0:
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityCritical,
			Message:           fmt.Sprintf("Net operating result is negative (%s) — the station is running at a loss for the period.", in.NetOperating),
			RecommendedAction: "Review pump pricing, COGS, and operating expenses before locking the period.",
		})
	case okMargin && margin < 0:
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityCritical,
			Message:           "Gross margin is negative — sales are running below cost.",
			RecommendedAction: "Check pump pricing against the costing basis (below-cost guard).",
		})
	case okRev && okMargin && rev > 0:
		if pct := margin / rev * 100; pct < 5 {
			rep.Insights = append(rep.Insights, Insight{
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("Gross margin is thin at %.1f%% of revenue.", pct),
			})
		}
	}
	if okExp && okMargin && expenses > 0 && margin > 0 && expenses > margin {
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityWarning,
			Message:           "Operating expenses exceed gross margin for the period.",
			RecommendedAction: "Review the expense ledger for the station and reduce discretionary spend.",
		})
	}
	if !in.PeriodLocked {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "Not all revenue days in this period are locked — the result is provisional.",
		})
	}
	return rep
}

// ---- Station comparison (Feature 10.6) ----

// ComparisonStation is one station's already-computed comparison line.
type ComparisonStation struct {
	Name         string
	NetOperating string // decimal string; signed
	GrossMargin  string
	NetRevenue   string
	RiskAlerts   int
}

// StationComparisonInput bundles the stations being compared plus whether the
// caller's view was scoped to a subset of the tenant's stations.
type StationComparisonInput struct {
	Stations []ComparisonStation
	Scoped   bool // the caller sees only a subset of the tenant's stations
}

// StationComparison builds the cross-station comparison annotations: it names
// the strongest and weakest stations by net operating result, flags any
// loss-making station, surfaces a station carrying open risk alerts, and notes
// when the comparison is scoped to the caller's accessible stations.
func StationComparison(in StationComparisonInput) Report {
	var rep Report
	if len(in.Stations) == 0 {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "No stations in scope for this comparison.",
		})
		return rep
	}

	var best, worst *ComparisonStation
	var bestVal, worstVal float64
	var lossMakers, alertCarriers int
	var topAlertName string
	var topAlerts int
	for i := range in.Stations {
		s := &in.Stations[i]
		if v, ok := parseDec(s.NetOperating); ok {
			if best == nil || v > bestVal {
				best, bestVal = s, v
			}
			if worst == nil || v < worstVal {
				worst, worstVal = s, v
			}
			if v < 0 {
				lossMakers++
			}
		}
		if s.RiskAlerts > 0 {
			alertCarriers++
			if s.RiskAlerts > topAlerts {
				topAlerts, topAlertName = s.RiskAlerts, s.Name
			}
		}
	}

	if best != nil && len(in.Stations) > 1 {
		rep.Insights = append(rep.Insights, Insight{
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("%s leads on net operating result (%s).", best.Name, best.NetOperating),
		})
	}
	if lossMakers > 0 && worst != nil {
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityWarning,
			Message:           fmt.Sprintf("%d station(s) ran a negative net operating result; %s is the lowest (%s).", lossMakers, worst.Name, worst.NetOperating),
			RecommendedAction: "Open the profitability report for the lowest station to find the driver.",
		})
	}
	if topAlerts > 0 {
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityWarning,
			Message:           fmt.Sprintf("%s carries the most open risk alerts (%d).", topAlertName, topAlerts),
			RecommendedAction: "Review the station's open risk alerts and loss reconciliations.",
		})
	}
	if in.Scoped {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "This comparison is limited to the stations you have access to.",
		})
	}
	return rep
}

// ---- Credit & cashflow (Feature 10.5) ----

// CashflowInput is a station's already-computed credit & cashflow slice over a
// window. Every figure is an exact decimal string (summed in SQL ::numeric); the
// heuristics below parse them to float64 for DISPLAY math only.
type CashflowInput struct {
	CreditSales      string // credit tenders billed to AR
	TotalTendered    string // all tenders (cash + mobile + card + credit + voucher)
	Collections      string // posted customer payments against the station's invoices
	OutstandingAR    string // outstanding balance on the station's invoices
	OverdueAR        string // outstanding balance past due
	SupplierPayments string // supplier payments in the window (tenant-wide)
	CashVariance     string // sum of cash-reconciliation variances (signed)
	ProjectedCashPos string // realized net cash movement in the window (signed)
	HasActivity      bool   // any tender, collection, or receivable in the window
	PeriodLocked     bool   // every revenue day in the window is locked
}

// CreditCashflow builds the Credit & Cashflow report annotations: overdue
// receivables warn (escalating to critical when overdue dominates outstanding);
// a credit-heavy sales mix warns (collections are deferred); a negative realized
// cash position warns; a cash variance is flagged; and an unlocked window is
// noted as provisional data-quality.
func CreditCashflow(in CashflowInput) Report {
	var rep Report
	if !in.HasActivity {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "No tenders, collections, or receivables for this station in the period — cashflow figures are empty.",
		})
		return rep
	}

	outstanding, okOut := parseDec(in.OutstandingAR)
	overdue, okOver := parseDec(in.OverdueAR)
	if okOver && overdue > 0 {
		sev := SeverityWarning
		if okOut && outstanding > 0 && overdue/outstanding >= 0.5 {
			sev = SeverityCritical
		}
		rep.Insights = append(rep.Insights, Insight{
			Severity:          sev,
			Message:           fmt.Sprintf("%s of receivables is overdue.", in.OverdueAR),
			RecommendedAction: "Chase the overdue balances and review the affected customers' credit standing.",
		})
	}

	credit, okCredit := parseDec(in.CreditSales)
	total, okTotal := parseDec(in.TotalTendered)
	if okCredit && okTotal && total > 0 && credit > 0 {
		if share := credit / total * 100; share >= 40 {
			rep.Insights = append(rep.Insights, Insight{
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("Credit sales are %.0f%% of tendered revenue — a large share is deferred to collections.", share),
			})
		}
	}

	if proj, ok := parseDec(in.ProjectedCashPos); ok && proj < 0 {
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityWarning,
			Message:           fmt.Sprintf("Realized cash movement is negative (%s) — cash out (supplier payments) outran cash in for the period.", in.ProjectedCashPos),
			RecommendedAction: "Review supplier payment timing against collections to protect the cash position.",
		})
	}

	if v, ok := parseDec(in.CashVariance); ok && v != 0 {
		dir := "over"
		if v < 0 {
			dir = "short"
		}
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityWarning,
			Message:           fmt.Sprintf("Cumulative cash variance is %s by %s across reconciliations in the period.", dir, in.CashVariance),
			RecommendedAction: "Reconcile the till against expected cash before locking the period.",
		})
	}

	if !in.PeriodLocked {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "Not all revenue days in this period are locked — the cash position is provisional.",
		})
	}
	return rep
}
