package reportrules

import (
	"math"
	"strconv"
	"strings"
)

// The registered, deterministic evaluators. Each reads named figures from Facts
// and the rule's configured thresholds and returns the fired insight(s). They
// MIRROR the thresholds the internal/reporting composers already enforce, so a
// system rule seeded in mode "shadow" describes exactly what the composer does
// (and a tenant tuning the threshold gets the engine to fold a tuned line in).
// No figure is recomputed: floats are only the same display heuristics
// internal/reporting documents.
//
// Facts key conventions (filled by the handler from already-computed figures):
//   Nums["gross_current"], Nums["gross_prior"]      — period-over-period gross
//   Nums["gross_avg"]                                — recent-average gross
//   Nums["cash_variance"], Nums["cash_tolerance"]    — cash recon
//   Nums["overdue"], Nums["outstanding"]             — receivables
//   Nums["ordered_litres"], Nums["received_litres"]  — delivery
//   Nums["margin_current"], Nums["margin_prior"]     — margin
//   Ints["tanks_over_tolerance"]                     — stock recon
//   Flags["period_locked"]                           — period lock state

// evalPeriodOverPeriod fires when the latest metric moved past the configured
// warn percentage vs the prior period. Mirrors reporting.PeriodOverPeriod +
// severityForDeltaPct (default warn at 25%). Vars: {metric} {direction} {pct}.
func evalPeriodOverPeriod(rule Rule, f Facts) []Fired {
	cur, okC := f.num("gross_current")
	prev, okP := f.num("gross_prior")
	if !okC || !okP || prev <= 0 {
		return nil
	}
	pct := (cur - prev) / prev * 100
	warn := configFloat(rule.ThresholdConfig, "warn_pct", thresholdOr(rule, 25))
	if math.Abs(pct) < warn {
		return nil
	}
	dir := "up"
	if pct < 0 {
		dir = "down"
	}
	return []Fired{{
		Vars: map[string]string{
			"metric":    configStr(rule.ThresholdConfig, "metric", "Gross revenue"),
			"direction": dir,
			"pct":       fmtPct1(math.Abs(pct)),
		},
	}}
}

// evalVarianceVsAverage fires when the latest metric deviates from its recent
// average past the configured percentage. Mirrors reporting.VarianceVs30dAverage
// (default threshold 20%). Vars: {metric} {pct}.
func evalVarianceVsAverage(rule Rule, f Facts) []Fired {
	cur, okC := f.num("gross_current")
	avg, okA := f.num("gross_avg")
	if !okC || !okA || avg <= 0 {
		return nil
	}
	pct := (cur - avg) / avg * 100
	warn := configFloat(rule.ThresholdConfig, "warn_pct", thresholdOr(rule, 20))
	if math.Abs(pct) < warn {
		return nil
	}
	sign := "+"
	if pct < 0 {
		sign = ""
	}
	return []Fired{{
		Vars: map[string]string{
			"metric": configStr(rule.ThresholdConfig, "metric", "Gross revenue"),
			"pct":    sign + fmtPct1(pct),
		},
	}}
}

// evalCashVariance fires when the absolute cash variance exceeds the tolerance
// (the rule threshold overrides the fact tolerance when set). Mirrors
// reporting.cashVarianceInsight: any over-tolerance variance warns, beyond the
// configured critical multiple (default 2x) escalates to critical. Vars:
// {variance} {direction}.
func evalCashVariance(rule Rule, f Facts) []Fired {
	v, ok := f.num("cash_variance")
	if !ok || v == 0 {
		return nil
	}
	limit := 0.0
	if t, ok := parseDec(rule.Threshold); ok && t > 0 {
		limit = t
	} else if tol, ok := f.num("cash_tolerance"); ok && tol > 0 {
		limit = tol
	}
	abs := math.Abs(v)
	if abs <= limit {
		return nil
	}
	sev := SeverityWarning
	mult := configFloat(rule.ThresholdConfig, "critical_multiple", 2)
	if limit > 0 && mult > 0 && abs > limit*mult {
		sev = SeverityCritical
	}
	dir := "over"
	if v < 0 {
		dir = "short"
	}
	raw, _ := f.rawNum("cash_variance")
	return []Fired{{
		Severity: sev,
		Vars: map[string]string{
			"variance":  raw,
			"direction": dir,
		},
	}}
}

// evalTankOverTolerance fires when one or more tanks are over their variance
// tolerance (the count comes pre-computed from the composer's per-tank check, so
// the engine and composer agree exactly). Mirrors reporting.tankOverTolerance.
// Vars: {count}.
func evalTankOverTolerance(rule Rule, f Facts) []Fired {
	n, ok := f.intVal("tanks_over_tolerance")
	if !ok {
		return nil
	}
	floor := int(thresholdOr(rule, 1))
	if floor < 1 {
		floor = 1
	}
	if n < floor {
		return nil
	}
	return []Fired{{Vars: map[string]string{"count": itoa(n)}}}
}

// evalMarginHealth fires when the latest margin is negative (critical) or has
// contracted past the configured percentage vs the prior period (warning).
// Mirrors reporting.marginInsight (default contraction 15%): the negative branch
// uses the rule's seeded "Latest margin is negative…" template; the CONTRACTION
// branch supplies its own template ("Margin contracted {pct}% vs the prior
// period.") so the folded line is byte-identical to the composer's contraction
// prose and never asserts a positive-but-shrinking margin is "negative".
func evalMarginHealth(rule Rule, f Facts) []Fired {
	cur, ok := f.num("margin_current")
	if !ok {
		return nil
	}
	if cur < 0 {
		// Negative-margin branch: use the rule's configured (critical) template.
		return []Fired{{
			Severity: SeverityCritical,
			Vars:     map[string]string{},
		}}
	}
	prev, okP := f.num("margin_prior")
	if !okP || prev <= 0 {
		return nil
	}
	pct := (cur - prev) / prev * 100
	contract := configFloat(rule.ThresholdConfig, "contract_pct", thresholdOr(rule, 15))
	if pct > -contract {
		return nil
	}
	// Contraction past the floor (margin still POSITIVE): warn with the contraction
	// magnitude and a contraction-specific template, mirroring the composer's
	// "Margin contracted %s vs the prior period." (fmtPct renders the signed pct,
	// e.g. "-20.0%"). {pct} carries the signed magnitude, so the rendered line is
	// "Margin contracted -20.0% vs the prior period." — identical to the composer.
	return []Fired{{
		Severity: SeverityWarning,
		Action:   "Review pump pricing and COGS for the contraction.",
		Template: "Margin contracted {pct}% vs the prior period.",
		Vars: map[string]string{
			"pct": "-" + fmtPct1(math.Abs(pct)),
		},
	}}
}

// evalOverdueShare fires when overdue receivables exceed zero; the severity
// escalates to critical when the overdue share of outstanding crosses the
// configured percentage (default 50%). Mirrors reporting.CustomerCredit's overdue
// branch. Vars: {overdue} {pct}.
func evalOverdueShare(rule Rule, f Facts) []Fired {
	overdue, ok := f.num("overdue")
	if !ok || overdue <= 0 {
		return nil
	}
	outstanding, okO := f.num("outstanding")
	share := 0.0
	if okO && outstanding > 0 {
		share = overdue / outstanding * 100
	}
	sev := SeverityWarning
	critPct := configFloat(rule.ThresholdConfig, "critical_pct", thresholdOr(rule, 50))
	if share >= critPct {
		sev = SeverityCritical
	}
	raw, _ := f.rawNum("overdue")
	return []Fired{{
		Severity: sev,
		Vars: map[string]string{
			"overdue": raw,
			"pct":     fmtPct0(share),
		},
	}}
}

// evalDeliveryShortfall fires when received litres fall short of ordered by more
// than the configured percentage of the order (default warn at 5%). Mirrors
// reporting.Delivery's shortfall branch. Vars: {shortfall} {pct}.
func evalDeliveryShortfall(rule Rule, f Facts) []Fired {
	ordered, okO := f.num("ordered_litres")
	received, okR := f.num("received_litres")
	if !okO || !okR || ordered <= 0 {
		return nil
	}
	shortfall := ordered - received
	if shortfall <= 0 {
		return nil
	}
	pct := shortfall / ordered * 100
	warn := configFloat(rule.ThresholdConfig, "warn_pct", thresholdOr(rule, 5))
	if pct < warn {
		return nil
	}
	return []Fired{{
		Vars: map[string]string{
			"shortfall": fmtLitres(shortfall),
			"pct":       fmtPct1(pct),
		},
	}}
}

// evalPeriodUnlocked fires when the report's period is NOT locked — a broad,
// cross-report data-quality note. Mirrors the shared "period is not locked"
// composer note. Reads Flags["period_locked"]; fires only when the flag is
// explicitly present and false (an absent flag does not fire, so reports that do
// not carry a lock state stay silent). No vars.
func evalPeriodUnlocked(rule Rule, f Facts) []Fired {
	v, present := f.Flags["period_locked"]
	if !present || v {
		return nil
	}
	return []Fired{{Vars: map[string]string{}}}
}

// ---- tiny render helpers (no fmt on the hot path; ASCII only) ----

// itoa renders a non-negative int as a decimal string.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// fmtLitres renders a litre magnitude with up to three decimals, trimming
// trailing zeros — mirrors reporting.fmtLitres so the prose matches the composer.
func fmtLitres(v float64) string {
	s := strconv.FormatFloat(math.Abs(v), 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		s = "0"
	}
	return s
}
