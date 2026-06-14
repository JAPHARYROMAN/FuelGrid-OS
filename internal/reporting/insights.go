// Package reporting holds the deterministic, hardcoded insight + data-quality
// functions that power the reporting hub's "insight cards" and "data-quality
// banners". These are intentionally NOT a rules engine: each function is a pure
// Go function that takes already-computed report data (the same decimal-string
// figures the dashboards show) and returns zero or more Insight / DataQuality
// values. No I/O, no config, no DSL — just transparent, testable heuristics.
//
// Money/litre figures arrive as exact decimal STRINGS. The helpers here parse
// them to float64 ONLY to compute display heuristics (period-over-period %,
// variance ratios). The original strings are never mutated and are not the
// source of any persisted figure — these outputs are advisory annotations.
package reporting

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Severity grades an insight for UI tone (info < warning < critical).
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Insight is a single deterministic observation about a report's data.
type Insight struct {
	Severity          Severity `json:"severity"`
	Message           string   `json:"message"`
	RecommendedAction string   `json:"recommended_action,omitempty"`
}

// DataQualityWarning flags a reason the figures on a report may be incomplete
// or subject to change (e.g. unclosed shifts, missing dips).
type DataQualityWarning struct {
	Message string `json:"message"`
}

// Report bundles the insights and data-quality warnings for one report view.
type Report struct {
	Insights    []Insight            `json:"insights"`
	DataQuality []DataQualityWarning `json:"data_quality"`
}

// parseDec parses a decimal string into a float64 for heuristic math only.
// Blank / unparseable values become 0 with ok=false so callers can skip a
// comparison rather than emit a misleading "−100%".
func parseDec(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// pctChange returns the percentage change from prev to cur, and whether it is
// meaningful (prev must be a non-trivial positive base).
func pctChange(cur, prev float64) (float64, bool) {
	if prev <= 0 {
		return 0, false
	}
	return (cur - prev) / prev * 100, true
}

// fmtPct renders a signed percentage with one decimal (e.g. "+14.0%").
func fmtPct(p float64) string {
	sign := "+"
	if p < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.1f%%", sign, p)
}

// fmtPctMagnitude renders an UNSIGNED percentage magnitude with one decimal
// (e.g. "5.0%") — for phrases that already state direction in words ("less than",
// "more than"), so a positive magnitude reads naturally without a leading sign.
func fmtPctMagnitude(p float64) string {
	return fmt.Sprintf("%.1f%%", math.Abs(p))
}

// fmtLitres renders a litre magnitude with up to three decimals, trimming
// trailing zeros (e.g. 400 → "400", 23.5 → "23.5"). Used for insight prose; the
// displayed report figures themselves always come from the exact decimal strings.
func fmtLitres(v float64) string {
	s := strconv.FormatFloat(math.Abs(v), 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		s = "0"
	}
	return s
}

// severityForDeltaPct grades a period-over-period swing: large moves warn.
func severityForDeltaPct(p float64) Severity {
	a := math.Abs(p)
	switch {
	case a >= 25:
		return SeverityWarning
	default:
		return SeverityInfo
	}
}

// ---- Generic series helpers (reused across report kinds) ----

// PeriodPoint is one labelled value in a trend (e.g. a revenue day's gross).
type PeriodPoint struct {
	Label string // e.g. business date
	Value string // decimal string
}

// PeriodOverPeriod compares the latest point to the immediately prior point in
// a chronological (oldest→newest) series and returns a delta insight. label is
// the metric's display name (e.g. "Gross revenue"). Returns false when there is
// no comparable prior point or the base is non-positive.
func PeriodOverPeriod(metric string, series []PeriodPoint) (Insight, bool) {
	n := len(series)
	if n < 2 {
		return Insight{}, false
	}
	cur, okC := parseDec(series[n-1].Value)
	prev, okP := parseDec(series[n-2].Value)
	if !okC || !okP {
		return Insight{}, false
	}
	p, ok := pctChange(cur, prev)
	if !ok {
		return Insight{}, false
	}
	dir := "up"
	if p < 0 {
		dir = "down"
	}
	return Insight{
		Severity: severityForDeltaPct(p),
		Message:  fmt.Sprintf("%s is %s %s vs the prior period.", metric, dir, fmtPct(p)),
	}, true
}

// VarianceVs30dAverage compares the latest value to the average of all earlier
// points in a chronological series, flagging an out-of-band reading. threshold
// is the absolute percent deviation above which a warning is raised.
func VarianceVs30dAverage(metric string, series []PeriodPoint, threshold float64) (Insight, bool) {
	n := len(series)
	if n < 3 {
		return Insight{}, false
	}
	var sum float64
	var count int
	for i := 0; i < n-1; i++ {
		if v, ok := parseDec(series[i].Value); ok {
			sum += v
			count++
		}
	}
	if count == 0 {
		return Insight{}, false
	}
	avg := sum / float64(count)
	cur, ok := parseDec(series[n-1].Value)
	if !ok {
		return Insight{}, false
	}
	p, ok := pctChange(cur, avg)
	if !ok || math.Abs(p) < threshold {
		return Insight{}, false
	}
	return Insight{
		Severity:          SeverityWarning,
		Message:           fmt.Sprintf("%s is %s vs its recent average — an unusual reading.", metric, fmtPct(p)),
		RecommendedAction: "Confirm the underlying transactions before relying on this figure.",
	}, true
}
