package reportrules

import (
	"reflect"
	"testing"
)

// baseRule returns an active, augment, enabled rule for the given condition with
// a simple template — the test then tweaks fields per case.
func baseRule(code, condition, tmpl string) Rule {
	return Rule{
		ID: "rule-" + code, Code: code, Condition: condition,
		Severity: SeverityWarning, MessageTemplate: tmpl,
		Placement: PlacementInsight, Mode: ModeAugment,
		Enabled: true, Status: "active", ThresholdConfig: map[string]any{},
	}
}

// ---- RenderTemplate: SAFE, injection-proof {token} substitution ----

func TestRenderTemplate_Substitution(t *testing.T) {
	got := RenderTemplate("{a} and {b}", map[string]string{"a": "X", "b": "Y"})
	if got != "X and Y" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderTemplate_UnknownTokenLeftIntact(t *testing.T) {
	got := RenderTemplate("{known} {unknown}", map[string]string{"known": "K"})
	if got != "K {unknown}" {
		t.Fatalf("unknown token should be left intact, got %q", got)
	}
}

func TestRenderTemplate_NoReexpansion_InjectionSafe(t *testing.T) {
	// A value that itself contains a "{token}" must NOT be re-expanded on a second
	// pass — otherwise a hostile figure could inject another rule's placeholder.
	vars := map[string]string{"v": "{secret}", "secret": "LEAKED"}
	got := RenderTemplate("value={v}", vars)
	if got != "value={secret}" {
		t.Fatalf("template value must be written verbatim (no re-expansion), got %q", got)
	}
}

func TestRenderTemplate_NoEvalNoBraceSoup(t *testing.T) {
	// Any "{x}" token is replaced by plain string substitution regardless of the
	// surrounding characters — there is no eval surface, no shell/format directive
	// interpretation. A literal "%" is inert (no printf), and a "$" prefix does not
	// shell-expand. So "${x}" becomes "$ok" (the {x} token substituted), never an
	// evaluated value.
	got := RenderTemplate("100% safe {x} ${x} #{y}", map[string]string{"x": "ok"})
	if got != "100% safe ok $ok #{y}" {
		t.Fatalf("got %q", got)
	}
}

// ---- Evaluate: applicability, mode, enable/disable, determinism ----

func TestEvaluate_AppliesByReportKey(t *testing.T) {
	rules := []Rule{
		func() Rule {
			r := baseRule("a", "period_over_period", "{metric} {direction} {pct}")
			r.ReportKey = "sales"
			return r
		}(),
		func() Rule { r := baseRule("b", "period_over_period", "x"); r.ReportKey = "delivery"; return r }(),
	}
	f := NewFacts()
	f.Nums["gross_current"] = "150"
	f.Nums["gross_prior"] = "100"
	got := Evaluate("sales", rules, f)
	if len(got) != 1 || got[0].RuleCode != "a" {
		t.Fatalf("only the sales rule should fire, got %+v", got)
	}
}

func TestEvaluate_BroadRuleAppliesEverywhere(t *testing.T) {
	r := baseRule("broad", "period_unlocked", "unlocked")
	r.Placement = PlacementDataQuality
	r.ReportKey = "" // broad
	f := NewFacts()
	f.Flags["period_locked"] = false
	if got := Evaluate("anything", []Rule{r}, f); len(got) != 1 {
		t.Fatalf("broad rule should apply to any report, got %d", len(got))
	}
}

func TestEvaluate_DisabledRuleDoesNotFire(t *testing.T) {
	r := baseRule("a", "period_over_period", "x")
	r.ReportKey = "sales"
	r.Enabled = false
	f := NewFacts()
	f.Nums["gross_current"] = "150"
	f.Nums["gross_prior"] = "100"
	if got := Evaluate("sales", []Rule{r}, f); len(got) != 0 {
		t.Fatalf("disabled rule must not fire, got %+v", got)
	}
}

func TestEvaluate_NonActiveStatusDoesNotFire(t *testing.T) {
	r := baseRule("a", "period_over_period", "x")
	r.Status = "paused"
	f := NewFacts()
	f.Nums["gross_current"] = "150"
	f.Nums["gross_prior"] = "100"
	if got := Evaluate("sales", []Rule{r}, f); len(got) != 0 {
		t.Fatalf("paused rule must not fire, got %+v", got)
	}
}

func TestEvaluate_UnknownConditionSkipped(t *testing.T) {
	r := baseRule("a", "no_such_evaluator", "x")
	if got := Evaluate("sales", []Rule{r}, NewFacts()); len(got) != 0 {
		t.Fatalf("unknown condition must be skipped, got %+v", got)
	}
}

func TestEvaluate_Deterministic_SameInputSameOutput(t *testing.T) {
	rules := []Rule{
		baseRule("z_swing", "period_over_period", "{metric} {direction} {pct}"),
		baseRule("a_margin", "margin_health", "neg"),
	}
	f := NewFacts()
	f.Nums["gross_current"] = "150"
	f.Nums["gross_prior"] = "100"
	f.Nums["margin_current"] = "-5"
	first := Evaluate("sales", rules, f)
	for i := 0; i < 50; i++ {
		again := Evaluate("sales", rules, f)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("non-deterministic output on run %d:\n%+v\nvs\n%+v", i, first, again)
		}
	}
	// And the order is by rule code (a_margin before z_swing).
	if len(first) != 2 || first[0].RuleCode != "a_margin" || first[1].RuleCode != "z_swing" {
		t.Fatalf("expected code-sorted output, got %+v", first)
	}
}

// ---- Tuning a threshold deterministically changes output ----

func TestEvaluate_TunedThresholdChangesOutput(t *testing.T) {
	f := NewFacts()
	f.Nums["gross_current"] = "110" // +10% swing
	f.Nums["gross_prior"] = "100"

	// Default warn 25% -> a 10% swing does NOT fire.
	def := baseRule("swing", "period_over_period", "{pct}")
	def.ReportKey = "sales"
	if got := Evaluate("sales", []Rule{def}, f); len(got) != 0 {
		t.Fatalf("10%% swing should not fire at warn 25%%, got %+v", got)
	}

	// Tune warn down to 5% -> the same 10% swing now fires.
	tuned := def
	tuned.ThresholdConfig = map[string]any{"warn_pct": float64(5)}
	got := Evaluate("sales", []Rule{tuned}, f)
	if len(got) != 1 {
		t.Fatalf("tuned warn 5%% should fire on a 10%% swing, got %+v", got)
	}
	if got[0].Message != "10.0" {
		t.Fatalf("rendered pct mismatch: %q", got[0].Message)
	}
}

// ---- Per-evaluator boundary checks ----

func TestEvalCashVariance_EscalatesToCritical(t *testing.T) {
	r := baseRule("cash", "cash_variance_over_tolerance", "{variance} {direction}")
	r.Threshold = "100" // tolerance 100
	f := NewFacts()
	f.Nums["cash_variance"] = "-250" // short by 250, > 2x tolerance
	got := Evaluate("cash-reconciliation", []Rule{r}, f)
	if len(got) != 1 || got[0].Severity != SeverityCritical {
		t.Fatalf("variance beyond 2x tolerance should be critical, got %+v", got)
	}
	if got[0].Message != "-250 short" {
		t.Fatalf("message %q", got[0].Message)
	}
}

func TestEvalCashVariance_WithinToleranceSilent(t *testing.T) {
	r := baseRule("cash", "cash_variance_over_tolerance", "x")
	r.Threshold = "100"
	f := NewFacts()
	f.Nums["cash_variance"] = "-50" // within tolerance
	if got := Evaluate("cash-reconciliation", []Rule{r}, f); len(got) != 0 {
		t.Fatalf("within-tolerance variance must be silent, got %+v", got)
	}
}

func TestEvalOverdueShare_CriticalAtThreshold(t *testing.T) {
	r := baseRule("od", "overdue_share", "{overdue} {pct}")
	f := NewFacts()
	f.Nums["overdue"] = "600"
	f.Nums["outstanding"] = "1000" // 60% overdue >= 50 -> critical
	got := Evaluate("customer-credit", []Rule{r}, f)
	if len(got) != 1 || got[0].Severity != SeverityCritical {
		t.Fatalf("60%% overdue should be critical, got %+v", got)
	}
	if got[0].Message != "600 60" {
		t.Fatalf("message %q", got[0].Message)
	}
}

func TestEvalMarginHealth_NegativeIsCritical(t *testing.T) {
	r := baseRule("m", "margin_health", "neg margin")
	f := NewFacts()
	f.Nums["margin_current"] = "-1"
	got := Evaluate("sales", []Rule{r}, f)
	if len(got) != 1 || got[0].Severity != SeverityCritical {
		t.Fatalf("negative margin should be critical, got %+v", got)
	}
}

func TestEvalTankOverTolerance_FiresOnCount(t *testing.T) {
	r := baseRule("t", "tank_over_tolerance", "{count} over")
	f := NewFacts()
	f.Ints["tanks_over_tolerance"] = 3
	got := Evaluate("inventory-reconciliation", []Rule{r}, f)
	if len(got) != 1 || got[0].Message != "3 over" {
		t.Fatalf("got %+v", got)
	}
	// Zero tanks -> silent.
	f.Ints["tanks_over_tolerance"] = 0
	if got := Evaluate("inventory-reconciliation", []Rule{r}, f); len(got) != 0 {
		t.Fatalf("zero over-tolerance tanks must be silent, got %+v", got)
	}
}

func TestEvalDeliveryShortfall_WarnPctFloor(t *testing.T) {
	r := baseRule("d", "delivery_shortfall", "{shortfall} {pct}")
	f := NewFacts()
	f.Nums["ordered_litres"] = "1000"
	f.Nums["received_litres"] = "980" // 2% short, below 5% floor
	if got := Evaluate("delivery", []Rule{r}, f); len(got) != 0 {
		t.Fatalf("2%% short below 5%% floor must be silent, got %+v", got)
	}
	f.Nums["received_litres"] = "900" // 10% short
	got := Evaluate("delivery", []Rule{r}, f)
	if len(got) != 1 || got[0].Message != "100 10.0" {
		t.Fatalf("got %+v", got)
	}
}

func TestEvalPeriodUnlocked_AbsentFlagSilent(t *testing.T) {
	r := baseRule("p", "period_unlocked", "unlocked")
	r.Placement = PlacementDataQuality
	// No flag set -> silent (reports without a lock state stay quiet).
	if got := Evaluate("any", []Rule{r}, NewFacts()); len(got) != 0 {
		t.Fatalf("absent period_locked flag must be silent, got %+v", got)
	}
	// Locked -> silent.
	f := NewFacts()
	f.Flags["period_locked"] = true
	if got := Evaluate("any", []Rule{r}, f); len(got) != 0 {
		t.Fatalf("locked period must be silent, got %+v", got)
	}
	// Unlocked -> fires.
	f.Flags["period_locked"] = false
	if got := Evaluate("any", []Rule{r}, f); len(got) != 1 {
		t.Fatalf("unlocked period must fire, got %+v", got)
	}
}

// ---- Registry lock ----

func TestRegisteredConditions_Stable(t *testing.T) {
	want := []string{
		"cash_variance_over_tolerance", "delivery_shortfall", "margin_health",
		"overdue_share", "period_over_period", "period_unlocked",
		"tank_over_tolerance", "variance_vs_average",
	}
	if got := RegisteredConditions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("registry drift:\n got %v\nwant %v", got, want)
	}
}
