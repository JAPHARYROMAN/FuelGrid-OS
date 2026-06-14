package reporting

import (
	"fmt"
	"sort"
	"strings"
)

// This file holds the deterministic Risk & Loss intelligence composer (blueprint
// §5.11 / §20.4). Like every other composer in this package it takes a small,
// already-computed input struct (built by the HTTP handler from real joins over
// the tank_reconciliations variance history + the risk domain) and returns
// transparent insights + data-quality notes. There is NO AI and NO free-form
// expression engine: the pattern findings ("Pump 03 appeared in 68% of related
// events") are pure percentage-of-total arithmetic over the variance events the
// handler counted by dimension, with every figure traceable to its source rows.
//
// Money/litre figures arrive as exact decimal STRINGS and are NEVER recomputed
// here — the handler sums them in SQL numeric and passes the totals through. The
// only arithmetic this composer does is integer share math (event counts and the
// loss-value already computed upstream) to turn a tally into a "% of related
// events" finding; no float64 ever touches a money/litre value.

// LossDimensionTally is one bucket of variance events for a single dimension
// value — e.g. one pump (tank code), one product, one shift name, or one
// attendant. Count is the number of over-tolerance variance events attributed to
// this value within the window. The labels are display strings.
type LossDimensionTally struct {
	Key   string // stable identity (uuid string or the dimension value)
	Label string // display label (pump code, product name, attendant name, shift name)
	Count int    // over-tolerance variance events attributed to this value
}

// RiskLossInput is the already-computed slice of a station's (or scope's) risk &
// loss picture a handler passes in. TotalEvents is the number of distinct
// over-tolerance variance events in the window — the denominator every pattern
// percentage is computed against, so a finding is always "X of N events". Each
// dimension slice is the per-value tally for that dimension, the handler having
// run the join (events → station/product/pump/shift/attendant). LossValueShown
// gates the financial-impact wording: when the actor lacks margin.view the value
// is omitted (not zeroed) and the composer never mentions a money figure.
type RiskLossInput struct {
	StationLabel string // the report's station label (for the headline sentence)

	TotalEvents      int    // distinct over-tolerance variance events in the window
	WindowDays       int    // the look-back window the events were counted over
	RepeatedTanks    int    // tanks over tolerance on 2+ days (recurring loss)
	OpenAlerts       int    // open risk alerts in scope
	OpenInvestations int    // open investigation cases in scope
	LossLitres       string // total loss litres (decimal string, summed in SQL)
	LossValue        string // total loss value (decimal string); only set when shown
	LossValueShown   bool   // true when the actor may see the sensitive loss VALUE

	ByPump      []LossDimensionTally // events by pump (tank code)
	ByShift     []LossDimensionTally // events by shift name
	ByAttendant []LossDimensionTally // events by attendant on the event's operating day
	ByProduct   []LossDimensionTally // events by product

	// Data-quality signals the composer turns into honest banner notes.
	EventsMissingDip int  // over-tolerance events on a tank with no closing dip
	IncompleteRecons int  // reconciliations still draft/exception (not sealed) in window
	DisabledLossRule bool // the fuel-variance rule is disabled, so alerts may be stale
}

// DonutDatum is one slice of a root-cause / pattern distribution donut: a label
// and an integer event count rendered as a decimal STRING value (matching the
// @fuelgrid/ui DonutSlice contract). It carries no money — only event tallies —
// so it stays a plain count throughout.
type DonutDatum struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// PatternFinding is one deterministic, traceable line of the §5.11 pattern
// narrative — a single "<label> appeared in <pct>% of related events (<n> of
// <total>)" statement. Share is the integer percentage; the count + total make
// the figure auditable. Dimension names which axis it came from.
type PatternFinding struct {
	Dimension string `json:"dimension"` // pump | shift | attendant | product
	Label     string `json:"label"`
	Count     int    `json:"count"`
	Total     int    `json:"total"`
	SharePct  int    `json:"share_pct"`
}

// sharePct is the integer percentage n/total, rounded to the nearest whole
// percent. Returns 0 when there is no denominator. Pure integer math — no float
// ever touches a money/litre value (this is an event-count ratio only).
func sharePct(n, total int) int {
	if total <= 0 {
		return 0
	}
	return (n*100 + total/2) / total
}

// topFinding returns the strongest finding for a dimension: the value with the
// most events, expressed as its share of TotalEvents. Returns ok=false when the
// dimension is empty or the leader has no events. The leader is chosen by count
// then label so the result is deterministic for ties.
func topFinding(dim string, tallies []LossDimensionTally, total int) (PatternFinding, bool) {
	if total <= 0 || len(tallies) == 0 {
		return PatternFinding{}, false
	}
	sorted := make([]LossDimensionTally, len(tallies))
	copy(sorted, tallies)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Count != sorted[j].Count {
			return sorted[i].Count > sorted[j].Count
		}
		return sorted[i].Label < sorted[j].Label
	})
	lead := sorted[0]
	if lead.Count <= 0 {
		return PatternFinding{}, false
	}
	return PatternFinding{
		Dimension: dim,
		Label:     lead.Label,
		Count:     lead.Count,
		Total:     total,
		SharePct:  sharePct(lead.Count, total),
	}, true
}

// RiskLossPatterns computes the deterministic §5.11 pattern findings: for each
// dimension (pump, shift, attendant, product) the single strongest concentration
// expressed as "<label> appeared in <pct>% of related events". Only findings
// whose share is MATERIAL (>= a transparent 40% concentration floor, AND the
// dimension has more than one observed value so a single-value scope does not
// trivially read 100%) are returned, newest-strongest first. Returning the raw
// findings (not prose) lets the handler render both the §5.11 narrative card and
// the structured table while keeping every number auditable.
//
// The 40% floor is the same idea as the blueprint's "68% of events" example: a
// concentration well above an even split is the signal worth surfacing. With
// fewer than the configurable minimum events the set is empty (too little data to
// call a pattern) — an honest partial state rather than a fabricated one.
func RiskLossPatterns(in RiskLossInput) []PatternFinding {
	const concentrationFloor = 40 // % share that makes a concentration noteworthy
	const minEvents = 3           // below this we don't call a "pattern"
	if in.TotalEvents < minEvents {
		return []PatternFinding{}
	}
	out := []PatternFinding{}
	add := func(dim string, tallies []LossDimensionTally) {
		f, ok := topFinding(dim, tallies, in.TotalEvents)
		if !ok {
			return
		}
		// A single observed value in a dimension trivially reads 100% and says
		// nothing about concentration — suppress it so a one-pump station doesn't
		// fabricate a "pump pattern".
		distinct := 0
		for i := range tallies {
			if tallies[i].Count > 0 {
				distinct++
			}
		}
		if distinct <= 1 {
			return
		}
		if f.SharePct >= concentrationFloor {
			out = append(out, f)
		}
	}
	add("pump", in.ByPump)
	add("shift", in.ByShift)
	add("attendant", in.ByAttendant)
	add("product", in.ByProduct)
	// Strongest concentration first so the narrative leads with the clearest signal.
	sort.SliceStable(out, func(i, j int) bool { return out[i].SharePct > out[j].SharePct })
	return out
}

// RiskLoss builds the Risk & Loss report annotations: the §5.11 pattern
// narrative as insights, the recurring-loss and open-alert/investigation signals,
// and the honest data-quality notes (missing dips, incomplete reconciliations,
// a disabled loss rule). Every rule is a transparent threshold; no money is
// recomputed.
func RiskLoss(in RiskLossInput) Report {
	var rep Report
	findings := RiskLossPatterns(in)

	// The headline §5.11 sentence: a deterministic loss summary for the scope.
	// The value clause is only added when the actor may see the sensitive figure.
	if in.TotalEvents > 0 {
		station := strings.TrimSpace(in.StationLabel)
		if station == "" {
			station = "This station"
		}
		head := fmt.Sprintf("%s recorded %d over-tolerance variance event(s) over the last %d day(s), totalling %s L of loss",
			station, in.TotalEvents, windowOr(in.WindowDays, 30), strOrDash(in.LossLitres))
		if in.LossValueShown && strings.TrimSpace(in.LossValue) != "" {
			head += fmt.Sprintf(", valued at approximately %s TZS", in.LossValue)
		}
		head += "."
		sev := SeverityWarning
		if in.RepeatedTanks > 0 {
			sev = SeverityCritical
		}
		rep.Insights = append(rep.Insights, Insight{Severity: sev, Message: head})
	}

	// The pattern findings become individual, traceable insight lines — exactly
	// the blueprint example shape ("Pump 03 appeared in 68% of related events").
	for _, f := range findings {
		rep.Insights = append(rep.Insights, Insight{
			Severity: SeverityWarning,
			Message: fmt.Sprintf("%s %q appeared in %d%% of related variance events (%d of %d).",
				dimensionNoun(f.Dimension), f.Label, f.SharePct, f.Count, f.Total),
		})
	}

	// A recurring loss (a tank over tolerance on multiple days) is the strongest
	// fraud/leakage signal — it gets a concrete recommended action.
	if in.RepeatedTanks > 0 {
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityCritical,
			Message:           fmt.Sprintf("%d tank(s) breached tolerance on 2 or more days — a recurring loss pattern.", in.RepeatedTanks),
			RecommendedAction: recommendFromFindings(findings, "Open a loss investigation for the repeating tank(s) and verify meter calibration."),
		})
	} else if len(findings) > 0 {
		// Isolated but concentrated: still worth an audit of the leading factor.
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityWarning,
			Message:           "Variance events are concentrated rather than evenly spread — a targeted audit is warranted.",
			RecommendedAction: recommendFromFindings(findings, "Audit the leading pump/shift/attendant in the pattern above and re-verify the closing readings."),
		})
	}

	if in.OpenInvestations > 0 {
		rep.Insights = append(rep.Insights, Insight{
			Severity:          SeverityInfo,
			Message:           fmt.Sprintf("%d open investigation case(s) cover loss/variance in this scope.", in.OpenInvestations),
			RecommendedAction: "Progress the open investigation(s) to a disposition.",
		})
	}

	// Data-quality: the loss figures are only as trustworthy as their inputs.
	if in.EventsMissingDip > 0 {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: fmt.Sprintf("%d over-tolerance event(s) sit on a tank with no closing dip — those variances are book-only and may be measurement gaps, not real loss.", in.EventsMissingDip),
		})
	}
	if in.IncompleteRecons > 0 {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: fmt.Sprintf("%d reconciliation(s) in this window are not yet sealed — the loss total may change once they are finalised.", in.IncompleteRecons),
		})
	}
	if in.DisabledLossRule {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "The fuel-variance risk rule is disabled — open alerts may not reflect the latest variance events. Re-enable or tune it from the rules page.",
		})
	}
	if !in.LossValueShown {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "Loss value is hidden — it requires the margin.view permission. Loss litres and counts are shown in full.",
		})
	}
	return rep
}

// dimensionNoun renders a dimension key as the sentence-leading noun.
func dimensionNoun(dim string) string {
	switch dim {
	case "pump":
		return "Pump"
	case "shift":
		return "Shift"
	case "attendant":
		return "Attendant"
	case "product":
		return "Product"
	default:
		return strings.Title(dim) //nolint:staticcheck // simple display title, ASCII labels
	}
}

// recommendFromFindings builds a §5.11-style recommended action naming the
// leading factors, falling back to a generic action when there is no pattern.
func recommendFromFindings(findings []PatternFinding, fallback string) string {
	if len(findings) == 0 {
		return fallback
	}
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, fmt.Sprintf("%s %s", strings.ToLower(dimensionNoun(f.Dimension)), f.Label))
	}
	return "Audit " + strings.Join(parts, ", ") + "; verify the closing readings and review the related cash submissions."
}

// windowOr returns the window days, or a default when unset.
func windowOr(days, def int) int {
	if days <= 0 {
		return def
	}
	return days
}

// strOrDash returns the trimmed decimal string, or "0" when blank.
func strOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "0"
	}
	return s
}
