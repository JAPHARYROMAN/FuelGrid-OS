package server

import (
	"reflect"
	"testing"

	"github.com/japharyroman/fuelgrid-os/internal/reporting"
	"github.com/japharyroman/fuelgrid-os/internal/reportrules"
)

// GOLDEN no-regression test (Reports Center Phase 15). The central guarantee:
// with the DEFAULT seeded system rules (mode='shadow'), folding the config-driven
// engine into a report envelope leaves the VISIBLE output — Insights, DataQuality
// and RecommendedActions — byte-identical to the pre-Phase-15 composer output.
// Only InsightRules (the preview/attribution surface) gains rows. This proves the
// engine is additive and cannot regress any existing insight.
//
// It exercises every seeded report_key against representative figures that DO
// trip the underlying composer rule, captures the composer-only envelope as the
// golden, then asserts the post-engine envelope's visible fields are identical.

// shadowSeedRules returns the seeded system rules in their DEFAULT mode (shadow)
// for a report key, mirroring the 0115 migration. Each is enabled + active so it
// is evaluated; shadow mode keeps it out of the visible envelope.
func shadowSeedRules() []reportrules.Rule {
	mk := func(code, reportKey, condition, tmpl string, sev reportrules.Severity, placement reportrules.Placement, cfg map[string]any) reportrules.Rule {
		if cfg == nil {
			cfg = map[string]any{}
		}
		return reportrules.Rule{
			ID: "sys-" + code, Code: code, ReportKey: reportKey, Condition: condition,
			Severity: sev, MessageTemplate: tmpl, Placement: placement,
			Mode: reportrules.ModeShadow, IsSystem: true, Enabled: true, Status: "active",
			ThresholdConfig: cfg,
		}
	}
	return []reportrules.Rule{
		mk("gross_swing", "sales", "period_over_period", "{metric} moved {direction} {pct}% vs the prior period.", reportrules.SeverityWarning, reportrules.PlacementInsight, map[string]any{"metric": "Gross revenue", "warn_pct": float64(25)}),
		mk("gross_variance", "sales", "variance_vs_average", "{metric} is {pct}% vs its recent average — an unusual reading.", reportrules.SeverityWarning, reportrules.PlacementInsight, map[string]any{"metric": "Gross revenue", "warn_pct": float64(20)}),
		mk("cash_variance", "cash-reconciliation", "cash_variance_over_tolerance", "Cash drawer is off by {variance} — beyond tolerance.", reportrules.SeverityWarning, reportrules.PlacementInsight, map[string]any{"critical_multiple": float64(2)}),
		mk("tank_over_tolerance", "inventory-reconciliation", "tank_over_tolerance", "{count} tank(s) exceeded their variance tolerance.", reportrules.SeverityWarning, reportrules.PlacementInsight, nil),
		mk("margin_health", "sales", "margin_health", "Latest margin is negative — sales are running below cost.", reportrules.SeverityCritical, reportrules.PlacementInsight, map[string]any{"contract_pct": float64(15)}),
		mk("overdue_receivables", "customer-credit", "overdue_share", "{overdue} of receivables is overdue ({pct}% of outstanding).", reportrules.SeverityWarning, reportrules.PlacementInsight, map[string]any{"critical_pct": float64(50)}),
		mk("delivery_shortfall", "delivery", "delivery_shortfall", "Received {shortfall} L less than ordered this period ({pct}% of the ordered volume).", reportrules.SeverityWarning, reportrules.PlacementInsight, map[string]any{"warn_pct": float64(5)}),
		mk("period_unlocked", "", "period_unlocked", "This period is not locked yet, so its totals are provisional.", reportrules.SeverityInfo, reportrules.PlacementDataQuality, nil),
	}
}

// visibleSnapshot captures only the visible envelope fields the golden guards.
type visibleSnapshot struct {
	Insights    []reporting.Insight
	DataQuality []dataQualityItem
	Actions     []string
}

func snapshot(e *ReportEnvelope) visibleSnapshot {
	return visibleSnapshot{
		Insights:    append([]reporting.Insight(nil), e.Insights...),
		DataQuality: append([]dataQualityItem(nil), e.DataQuality...),
		Actions:     append([]string(nil), e.RecommendedActions...),
	}
}

// goldenCase is one report's representative figures: the composer Report it
// produces and the engine Facts the handler would build.
type goldenCase struct {
	name      string
	reportKey string
	composer  reporting.Report
	facts     reportrules.Facts
}

func goldenCases() []goldenCase {
	gross := func(vals ...string) []reporting.PeriodPoint {
		pts := make([]reporting.PeriodPoint, len(vals))
		for i, v := range vals {
			pts[i] = reporting.PeriodPoint{Label: "d", Value: v}
		}
		return pts
	}
	salesFacts := reportrules.NewFacts()
	salesFacts.Nums["gross_current"] = "150"
	salesFacts.Nums["gross_prior"] = "100"
	salesFacts.Nums["gross_avg"] = "100"
	salesFacts.Nums["margin_current"] = "-5"
	salesFacts.Flags["period_locked"] = false

	cashFacts := reportrules.NewFacts()
	cashFacts.Nums["cash_variance"] = "-250"
	cashFacts.Flags["period_locked"] = false

	reconFacts := reportrules.NewFacts()
	reconFacts.Ints["tanks_over_tolerance"] = 2
	reconFacts.Flags["period_locked"] = false

	creditFacts := reportrules.NewFacts()
	creditFacts.Nums["overdue"] = "600"
	creditFacts.Nums["outstanding"] = "1000"

	deliveryFacts := reportrules.NewFacts()
	deliveryFacts.Nums["ordered_litres"] = "1000"
	deliveryFacts.Nums["received_litres"] = "900"
	deliveryFacts.Flags["period_locked"] = false

	return []goldenCase{
		{
			name: "sales", reportKey: "sales",
			composer: reporting.SalesSummary(reporting.SalesInput{
				GrossSeries: gross("100", "100", "150"), MarginSeries: gross("-5"), PeriodLocked: false,
			}),
			facts: salesFacts,
		},
		{
			name: "cash-reconciliation", reportKey: "cash-reconciliation",
			composer: reporting.CashReconciliation(reporting.CashReconInput{
				Variance: "-250", Tolerance: "100", PeriodLocked: false,
			}),
			facts: cashFacts,
		},
		{
			name: "inventory-reconciliation", reportKey: "inventory-reconciliation",
			composer: reporting.StockReconciliation(reporting.StockReconInput{
				Tanks: []reporting.TankRecon{
					{TankLabel: "T1", VariancePercent: "5", TolerancePercent: "1", Status: "over_tolerance", HasPhysicalDip: true},
					{TankLabel: "T2", VariancePercent: "3", TolerancePercent: "1", Status: "over_tolerance", HasPhysicalDip: true},
				},
				AllShiftsClosed: false,
			}),
			facts: reconFacts,
		},
		{
			name: "customer-credit", reportKey: "customer-credit",
			composer: reporting.CustomerCredit(reporting.CustomerCreditInput{
				Outstanding: "1000", Overdue: "600", Days90Plus: "0", Days61To90: "0",
				CustomersWithBalance: 3, ExposureShown: true,
			}),
			facts: creditFacts,
		},
		{
			name: "delivery", reportKey: "delivery",
			composer: reporting.Delivery(reporting.DeliveryInput{
				OrderedLitres: "1000", ReceivedLitres: "900", VarianceLitres: "-100",
				DeliveryCount: 1, PeriodComplete: false,
			}),
			facts: deliveryFacts,
		},
	}
}

// TestGolden_ShadowRulesDoNotChangeVisibleOutput is the central no-regression
// proof: the seeded system rules in their default (shadow) mode never alter the
// visible Insights / DataQuality / RecommendedActions.
func TestGolden_ShadowRulesDoNotChangeVisibleOutput(t *testing.T) {
	rules := shadowSeedRules()
	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			// Golden = composer-only envelope.
			golden := newEnvelope(c.reportKey, c.name, "this-month", nil)
			golden.applyReport(c.composer)
			want := snapshot(&golden)

			// Post-engine envelope = same composer + the shadow rules folded.
			got := newEnvelope(c.reportKey, c.name, "this-month", nil)
			got.applyReport(c.composer)
			fired := got.applyReportRules(c.reportKey, rules, c.facts)
			gotSnap := snapshot(&got)

			if !reflect.DeepEqual(want, gotSnap) {
				t.Fatalf("shadow rules changed visible output for %s:\nwant %+v\n got %+v", c.name, want, gotSnap)
			}
			// The shadow rules ARE evaluated (recorded in InsightRules, folded=false),
			// proving the no-regression is because of shadow mode, not because the
			// rules silently failed to fire.
			if len(fired) == 0 {
				t.Fatalf("%s: expected at least one shadow rule to fire (for the preview surface)", c.name)
			}
			anyFolded := false
			anyHit := false
			for i := range got.InsightRules {
				anyHit = true
				if got.InsightRules[i].Folded {
					anyFolded = true
				}
			}
			if !anyHit {
				t.Fatalf("%s: expected InsightRules attribution rows", c.name)
			}
			if anyFolded {
				t.Fatalf("%s: shadow rules must never be folded into the visible envelope", c.name)
			}
		})
	}
}

// TestGolden_AugmentRuleAddsOneInsight proves the additive path: flipping a seed
// rule to augment folds EXACTLY its rendered line into the visible insights,
// leaving the composer's own lines intact.
func TestGolden_AugmentRuleAddsOneInsight(t *testing.T) {
	c := goldenCases()[0] // sales
	golden := newEnvelope(c.reportKey, c.name, "this-month", nil)
	golden.applyReport(c.composer)
	baseInsights := len(golden.Insights)

	// One augment rule: gross_swing (the +50% swing trips it at warn 25%).
	rule := reportrules.Rule{
		ID: "sys-gross_swing", Code: "gross_swing", ReportKey: "sales",
		Condition: "period_over_period", Severity: reportrules.SeverityWarning,
		MessageTemplate: "{metric} moved {direction} {pct}% vs the prior period.",
		Placement:       reportrules.PlacementInsight, Mode: reportrules.ModeAugment,
		Enabled: true, Status: "active",
		ThresholdConfig: map[string]any{"metric": "Gross revenue", "warn_pct": float64(25)},
	}
	got := newEnvelope(c.reportKey, c.name, "this-month", nil)
	got.applyReport(c.composer)
	got.applyReportRules(c.reportKey, []reportrules.Rule{rule}, c.facts)

	if len(got.Insights) != baseInsights+1 {
		t.Fatalf("augment rule should add exactly one insight: base=%d got=%d", baseInsights, len(got.Insights))
	}
	last := got.Insights[len(got.Insights)-1]
	if last.Message != "Gross revenue moved up 50.0% vs the prior period." {
		t.Fatalf("augment insight message mismatch: %q", last.Message)
	}
	// And the composer's original insights are untouched (prefix-identical).
	for i := 0; i < baseInsights; i++ {
		if got.Insights[i] != golden.Insights[i] {
			t.Fatalf("composer insight %d was altered by the engine", i)
		}
	}
}

// TestGolden_DisabledRuleRemovesItsInsight proves disabling an augment rule
// removes its line (the kill switch works end to end).
func TestGolden_DisabledRuleRemovesItsInsight(t *testing.T) {
	c := goldenCases()[0]
	rule := reportrules.Rule{
		ID: "x", Code: "gross_swing", ReportKey: "sales", Condition: "period_over_period",
		Severity: reportrules.SeverityWarning, MessageTemplate: "{pct}",
		Placement: reportrules.PlacementInsight, Mode: reportrules.ModeAugment,
		Enabled: false, Status: "active", ThresholdConfig: map[string]any{"warn_pct": float64(25)},
	}
	env := newEnvelope(c.reportKey, c.name, "this-month", nil)
	env.applyReport(c.composer)
	before := len(env.Insights)
	env.applyReportRules(c.reportKey, []reportrules.Rule{rule}, c.facts)
	if len(env.Insights) != before {
		t.Fatalf("a disabled rule must add no insight: before=%d after=%d", before, len(env.Insights))
	}
	if len(env.InsightRules) != 0 {
		t.Fatalf("a disabled rule must not even appear in InsightRules, got %d", len(env.InsightRules))
	}
}

// TestGolden_NoRulesIsExactNoOp proves the empty-rules path leaves the envelope
// untouched (a tenant with the table empty sees pure composer output).
func TestGolden_NoRulesIsExactNoOp(t *testing.T) {
	c := goldenCases()[0]
	env := newEnvelope(c.reportKey, c.name, "this-month", nil)
	env.applyReport(c.composer)
	want := snapshot(&env)
	if fired := env.applyReportRules(c.reportKey, nil, c.facts); fired != nil {
		t.Fatalf("nil rules should fire nothing, got %+v", fired)
	}
	if !reflect.DeepEqual(want, snapshot(&env)) {
		t.Fatalf("nil rules must be an exact no-op")
	}
	if len(env.InsightRules) != 0 {
		t.Fatalf("nil rules must record no InsightRules")
	}
}
