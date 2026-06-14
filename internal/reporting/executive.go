package reporting

import (
	"fmt"
	"sort"
	"strings"
)

// This file holds the deterministic Executive cockpit composer (blueprint §5.1 /
// §20.1). Like every other composer in this package it takes a small,
// already-computed input struct — built by the HTTP handler by AGGREGATING the
// existing per-station report figures (StationComparison rows, the receivables
// aging rollup, the risk domain) — and returns the report's insights, the
// AUTOMATED MANAGEMENT NARRATIVE, and the honest data-quality notes.
//
// There is NO AI and NO free-form generation. The management narrative is built
// from a fixed sequence of sentence templates, each filled ONLY with a figure
// the handler computed (period totals, a period-over-period % change, the
// fastest-growing product, the top/bottom station). Every sentence is therefore
// traceable to a computed figure and DETERMINISTIC for the same data — running
// the composer twice on the same input yields byte-identical prose.
//
// Money/litre figures arrive as exact decimal STRINGS and are NEVER recomputed
// here — the handler sums them in SQL numeric (by calling the existing repos)
// and passes the totals through. The only arithmetic this composer does is the
// display-only percentage math the rest of the package already does (parseDec →
// float64 → a percent), exactly as insights.go documents; no float64 ever
// becomes a persisted money/litre figure.

// ExecMetricDelta is one headline metric's current value plus its prior-period
// value, both as exact decimal strings. The composer computes the display-only
// period-over-period percentage from the pair; the strings themselves are shown
// verbatim by the handler/page.
type ExecMetricDelta struct {
	Current string // current-period total (decimal string)
	Prior   string // prior-period total (decimal string), "" when no prior period
}

// ExecProductGrowth is one product's current vs prior litres, for the
// "fastest-growing product" sentence. Litres are exact decimal strings.
type ExecProductGrowth struct {
	Name    string
	Current string // current-period litres
	Prior   string // prior-period litres
}

// ExecStationLine is one station's already-aggregated headline figures, used to
// name the top and underperforming stations in the narrative. NetOperating is
// signed; the figures are exact decimal strings.
type ExecStationLine struct {
	Name         string
	NetRevenue   string
	NetOperating string
	RiskAlerts   int
}

// ExecutiveInput is the already-computed, scope-aggregated slice of the network
// the handler passes in. Every money/litre field is an exact decimal string
// (summed in SQL ::numeric by the existing repos); the counts drive the KPI
// hero. The *Shown flags gate the sensitive figures (margin / loss value /
// credit exposure) so the narrative never mentions a figure the actor may not
// see — omit, never zero.
type ExecutiveInput struct {
	Period       string // the period label (e.g. "this-month")
	PriorPeriod  string // the prior period label (e.g. "last-month"); "" when none
	StationCount int    // stations in the actor's permitted scope with activity

	Revenue ExecMetricDelta // total net revenue, current vs prior
	Litres  ExecMetricDelta // total litres sold, current vs prior

	// Sensitive: only set + only narrated when MarginShown is true.
	GrossMargin ExecMetricDelta
	NetMargin   ExecMetricDelta
	MarginShown bool

	// Sensitive: only set + only narrated when LossValueShown is true.
	LossLitres     string // total fuel-loss litres (always shown)
	LossValue      string // total fuel-loss value (gated)
	LossValueShown bool

	// Sensitive: only set + only narrated when ExposureShown is true.
	CreditExposure string // total outstanding receivable (gated)
	ExposureShown  bool

	CashShortages      string // total cash shortage across the scope (always shown)
	StockoutRisk       int    // tanks/stations flagged at stockout risk
	OpenAlerts         int    // open risk alerts in scope
	OpenInvestigations int    // open investigation cases
	PendingApprovals   int    // unlocked days / unapproved shifts awaiting sign-off
	SupplierIssues     int    // supplier scorecard issues / delivery shortfalls

	FastestProduct ExecProductGrowth // the product whose litres grew fastest
	TopStation     *ExecStationLine  // strongest station by net operating result
	WeakStation    *ExecStationLine  // weakest station by net operating result

	// Data-quality signals the composer turns into honest banner notes.
	UnlockedDays   int  // revenue days in the window not yet locked (provisional)
	StationsNoData int  // in-scope stations with no activity in the window
	Scoped         bool // the actor sees only a subset of the tenant's stations
}

// ExecutiveNarrative is the §5.1 automated management narrative: an ordered list
// of deterministic sentences plus the single recommended-focus line. Each
// sentence is filled only with a computed figure, so the band is fully
// auditable and reproducible. The handler renders Sentences as the narrative
// band and Focus as the highlighted "recommended focus" line.
type ExecutiveNarrative struct {
	Sentences []string `json:"sentences"`
	Focus     string   `json:"focus"`
}

// ExecutiveNarrativeText builds the deterministic §5.1 management narrative from
// the aggregated input. The sentence sequence is FIXED; only the figures vary.
// The blueprint example shape is reproduced exactly:
//
//	"Your network sold <litres> L this period across <N> station(s)."
//	"Revenue <rose/fell> <pct> vs the prior period."        (only with a prior period)
//	"<Product> grew fastest at <pct>."                       (only when a clear winner)
//	"<TopStation> led on net operating result; <WeakStation> trailed."
//	"<N> cash shortage(s) / loss litres / open alert(s) need attention."
//	Recommended focus: <the single highest-priority deterministic action>.
//
// Sensitive clauses (margin %, loss value, credit exposure) are emitted ONLY
// when their *Shown gate is true. With no prior period the growth sentences are
// omitted (an honest partial state, never a fabricated "+0%").
func ExecutiveNarrativeText(in ExecutiveInput) ExecutiveNarrative {
	var out ExecutiveNarrative

	// 1) The network volume + footprint sentence (always present).
	out.Sentences = append(out.Sentences, fmt.Sprintf(
		"Your network sold %s L this period across %s.",
		strOrDash(in.Litres.Current), pluralCount(in.StationCount, "station", "stations")))

	// 2) Revenue period-over-period (only with a comparable prior period).
	if pct, ok := pctChangeStr(in.Revenue.Current, in.Revenue.Prior); ok {
		out.Sentences = append(out.Sentences, fmt.Sprintf(
			"Revenue %s %s vs the prior period (%s vs %s TZS).",
			riseFell(pct), fmtPctMagnitude(pct), strOrDash(in.Revenue.Current), strOrDash(in.Revenue.Prior)))
	}

	// 3) Fastest-growing product (only when a clear, positive winner exists).
	if pct, ok := pctChangeStr(in.FastestProduct.Current, in.FastestProduct.Prior); ok && pct > 0 && strings.TrimSpace(in.FastestProduct.Name) != "" {
		out.Sentences = append(out.Sentences, fmt.Sprintf(
			"%s grew fastest at %s.", in.FastestProduct.Name, fmtPct(pct)))
	}

	// 4) Margin (gated): a net-margin sentence only when the actor may see margin.
	if in.MarginShown {
		if pct, ok := pctChangeStr(in.NetMargin.Current, in.NetMargin.Prior); ok {
			out.Sentences = append(out.Sentences, fmt.Sprintf(
				"Net margin %s %s vs the prior period (now %s TZS).",
				riseFell(pct), fmtPctMagnitude(pct), strOrDash(in.NetMargin.Current)))
		} else if strings.TrimSpace(in.NetMargin.Current) != "" {
			out.Sentences = append(out.Sentences, fmt.Sprintf(
				"Net margin for the period is %s TZS.", in.NetMargin.Current))
		}
	}

	// 5) Station leaders/laggards (only when both ends are known and distinct).
	if in.TopStation != nil && in.WeakStation != nil && in.TopStation.Name != in.WeakStation.Name {
		out.Sentences = append(out.Sentences, fmt.Sprintf(
			"%s led on net operating result (%s TZS); %s trailed (%s TZS).",
			in.TopStation.Name, strOrDash(in.TopStation.NetOperating),
			in.WeakStation.Name, strOrDash(in.WeakStation.NetOperating)))
	} else if in.TopStation != nil {
		out.Sentences = append(out.Sentences, fmt.Sprintf(
			"%s is the strongest station on net operating result (%s TZS).",
			in.TopStation.Name, strOrDash(in.TopStation.NetOperating)))
	}

	// 6) Risk/loss/cash attention sentence (always present; the figures shown
	//    depend on the gates — loss value only when permitted).
	attention := execAttentionClause(in)
	if attention != "" {
		out.Sentences = append(out.Sentences, attention)
	}

	// Recommended focus: the single highest-priority deterministic action — chosen
	// by a fixed precedence over the computed signals so the same data always
	// yields the same focus line.
	out.Focus = execRecommendedFocus(in)
	return out
}

// execAttentionClause builds the deterministic "needs attention" sentence from
// the loss / cash / alert / pending-approval signals. Loss VALUE is only named
// when permitted; loss litres + counts are always shown.
func execAttentionClause(in ExecutiveInput) string {
	parts := []string{}
	if litres, ok := parseDec(in.LossLitres); ok && litres > 0 {
		clause := fmt.Sprintf("%s L of fuel loss", fmtLitres(litres))
		if in.LossValueShown {
			if v, okv := parseDec(in.LossValue); okv && v > 0 {
				clause = fmt.Sprintf("%s L of fuel loss (≈%s TZS)", fmtLitres(litres), in.LossValue)
			}
		}
		parts = append(parts, clause)
	}
	if v, ok := parseDec(in.CashShortages); ok && v > 0 {
		parts = append(parts, fmt.Sprintf("%s TZS in cash shortages", in.CashShortages))
	}
	if in.OpenAlerts > 0 {
		parts = append(parts, pluralCount(in.OpenAlerts, "open risk alert", "open risk alerts"))
	}
	if in.PendingApprovals > 0 {
		parts = append(parts, pluralCount(in.PendingApprovals, "pending approval", "pending approvals"))
	}
	if len(parts) == 0 {
		return "No fuel loss, cash shortages or open risk alerts need attention this period."
	}
	return joinWithAnd(parts) + " need attention this period."
}

// execRecommendedFocus picks the single highest-priority recommended focus by a
// FIXED precedence over the computed signals (so the focus is deterministic):
// recurring/repeated loss and open investigations first, then open alerts, then
// underperforming stations, then pending approvals, then overdue credit, falling
// back to a steady-state message. Each branch names the concrete next step.
func execRecommendedFocus(in ExecutiveInput) string {
	switch {
	case in.OpenInvestigations > 0:
		return fmt.Sprintf("Recommended focus: progress the %s to a disposition and verify the underlying reconciliations.",
			pluralCount(in.OpenInvestigations, "open investigation", "open investigations"))
	case in.OpenAlerts > 0:
		return fmt.Sprintf("Recommended focus: clear the %s — start with the highest-risk station's loss reconciliations.",
			pluralCount(in.OpenAlerts, "open risk alert", "open risk alerts"))
	case in.WeakStation != nil && isNegative(in.WeakStation.NetOperating):
		return fmt.Sprintf("Recommended focus: review %s — it is running a negative net operating result (%s TZS). Open its profitability report.",
			in.WeakStation.Name, strOrDash(in.WeakStation.NetOperating))
	case lossLitresPositive(in.LossLitres):
		return "Recommended focus: investigate the recorded fuel loss against the reconciliation variance history before locking the period."
	case in.PendingApprovals > 0:
		return fmt.Sprintf("Recommended focus: close the %s so the period's figures are final.",
			pluralCount(in.PendingApprovals, "pending approval", "pending approvals"))
	case in.SupplierIssues > 0:
		return fmt.Sprintf("Recommended focus: review the %s flagged on delivery — check ordered-vs-received variance.",
			pluralCount(in.SupplierIssues, "supplier issue", "supplier issues"))
	default:
		return "Recommended focus: the network is stable this period — maintain reconciliation discipline and review pump pricing against the costing basis."
	}
}

// Executive builds the §5.1 cockpit annotations: the management narrative as a
// lead insight, a margin/loss/credit health read (each gated), the
// underperforming-station and supplier signals, and the honest data-quality
// notes (unlocked days, stations with no data, a scoped view, gated figures).
// Every rule is a transparent threshold; no money is recomputed.
func Executive(in ExecutiveInput) Report {
	var rep Report

	// The management narrative leads as a single info insight — the board-level
	// summary, every sentence traceable to a computed figure.
	narrative := ExecutiveNarrativeText(in)
	if len(narrative.Sentences) > 0 {
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityInfo,
			Message:           strings.Join(narrative.Sentences, " "),
			RecommendedAction: strings.TrimPrefix(narrative.Focus, "Recommended focus: "),
		})
	}

	// Revenue period-over-period as its own graded insight (warns on a big swing).
	if ins, ok := PeriodOverPeriod("Network revenue",
		[]PeriodPoint{{Value: in.Revenue.Prior}, {Value: in.Revenue.Current}}); ok {
		rep.Insights = append(rep.Insights, ins)
	}

	// A loss-making station is a leadership-level warning.
	if in.WeakStation != nil && isNegative(in.WeakStation.NetOperating) {
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityWarning,
			Message:           fmt.Sprintf("%s ran a negative net operating result (%s TZS) this period.", in.WeakStation.Name, in.WeakStation.NetOperating),
			RecommendedAction: "Open the station's profitability report to find the driver (pricing, COGS or expenses).",
		})
	}

	// Recurring/aggregate loss + open alerts escalate to a warning with an action.
	if lossLitresPositive(in.LossLitres) && (in.OpenAlerts > 0 || in.OpenInvestigations > 0) {
		msg := fmt.Sprintf("The network recorded %s L of fuel loss with %s open.",
			strOrDash(in.LossLitres), pluralCount(in.OpenAlerts, "risk alert", "risk alerts"))
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityWarning,
			Message:           msg,
			RecommendedAction: "Drill into the Risk & Loss report for the highest-risk station.",
		})
	}

	// Overdue/credit exposure (only when the actor may see it).
	if in.ExposureShown {
		if v, ok := parseDec(in.CreditExposure); ok && v > 0 {
			rep.Insights = append(rep.Insights, Insight{
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("Outstanding credit exposure across the network is %s TZS.", in.CreditExposure),
			})
		}
	}

	// Data-quality: the rollup is only as final as its inputs.
	if in.UnlockedDays > 0 {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: fmt.Sprintf("%d revenue day(s) in this period are not yet locked — the network figures are provisional.", in.UnlockedDays),
		})
	}
	if in.StationsNoData > 0 {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: fmt.Sprintf("%d station(s) in scope have no recognized activity in the period — they contribute nothing to the rollup.", in.StationsNoData),
		})
	}
	if in.Scoped {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "This cockpit is limited to the stations you have access to — it is not the whole-network view.",
		})
	}
	if !in.MarginShown {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "Gross and net margin are hidden — they require the margin.view permission. Revenue, litres and counts are shown in full.",
		})
	}
	if !in.LossValueShown {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "Fuel-loss value is hidden — it requires the margin.view permission. Loss litres are shown in full.",
		})
	}
	if !in.ExposureShown {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "Credit exposure is hidden — it requires the customer credit permission.",
		})
	}
	return rep
}

// ---- small deterministic helpers (display-only math) ----

// pctChangeStr computes the period-over-period percent change from two decimal
// strings, returning ok=false when there is no comparable positive prior base
// (so the narrative omits the clause rather than printing a misleading figure).
func pctChangeStr(cur, prev string) (float64, bool) {
	c, okC := parseDec(cur)
	p, okP := parseDec(prev)
	if !okC || !okP {
		return 0, false
	}
	return pctChange(c, p)
}

// riseFell renders a direction word for a signed percentage.
func riseFell(p float64) string {
	if p < 0 {
		return "fell"
	}
	return "rose"
}

// isNegative reports whether a decimal string parses to a negative number.
func isNegative(s string) bool {
	v, ok := parseDec(s)
	return ok && v < 0
}

// lossLitresPositive reports whether a loss-litres decimal string is > 0.
func lossLitresPositive(s string) bool {
	v, ok := parseDec(s)
	return ok && v > 0
}

// pluralCount renders "<n> <singular>" or "<n> <plural>" deterministically.
func pluralCount(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// joinWithAnd joins parts as "a", "a and b", or "a, b and c" (Oxford-free) so
// the attention sentence reads naturally and deterministically.
func joinWithAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}

// FastestGrowingProduct returns the product whose litres grew the most (by
// period-over-period percentage, then by absolute current litres for ties), over
// the supplied current/prior product litres. It is a pure, deterministic
// selection — no money is recomputed; litres are compared as display floats only.
// Returns ok=false when no product has a comparable positive prior base.
func FastestGrowingProduct(products []ExecProductGrowth) (ExecProductGrowth, bool) {
	type scored struct {
		p   ExecProductGrowth
		pct float64
		cur float64
	}
	cands := make([]scored, 0, len(products))
	for _, p := range products {
		if strings.TrimSpace(p.Name) == "" {
			continue
		}
		pct, ok := pctChangeStr(p.Current, p.Prior)
		if !ok {
			continue
		}
		cur, _ := parseDec(p.Current)
		cands = append(cands, scored{p: p, pct: pct, cur: cur})
	}
	if len(cands) == 0 {
		return ExecProductGrowth{}, false
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].pct != cands[j].pct {
			return cands[i].pct > cands[j].pct
		}
		if cands[i].cur != cands[j].cur {
			return cands[i].cur > cands[j].cur
		}
		return cands[i].p.Name < cands[j].p.Name
	})
	return cands[0].p, true
}
