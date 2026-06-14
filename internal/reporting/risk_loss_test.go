package reporting

import (
	"strings"
	"testing"
)

// TestRiskLossPatterns_ConcentrationFinding asserts the deterministic §5.11
// pattern math: with 25 over-tolerance events, 17 of them on pump "03", the
// composer surfaces a "Pump 03 appeared in 68% of related events (17 of 25)"
// finding — the exact blueprint example shape, computed as integer share math.
func TestRiskLossPatterns_ConcentrationFinding(t *testing.T) {
	in := RiskLossInput{
		TotalEvents: 25,
		WindowDays:  7,
		ByPump: []LossDimensionTally{
			{Key: "p3", Label: "03", Count: 17},
			{Key: "p1", Label: "01", Count: 5},
			{Key: "p2", Label: "02", Count: 3},
		},
	}
	findings := RiskLossPatterns(in)
	if len(findings) != 1 {
		t.Fatalf("expected one pump finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Dimension != "pump" || f.Label != "03" {
		t.Fatalf("leader = %s %q, want pump 03", f.Dimension, f.Label)
	}
	if f.Count != 17 || f.Total != 25 {
		t.Fatalf("count/total = %d/%d, want 17/25", f.Count, f.Total)
	}
	if f.SharePct != 68 {
		t.Fatalf("share = %d%%, want 68%% (17/25)", f.SharePct)
	}
}

// TestRiskLossPatterns_BelowFloorSuppressed asserts an even spread (no value
// reaching the 40% concentration floor) yields no pattern — the report must not
// fabricate a "pattern" where the data does not support one.
func TestRiskLossPatterns_BelowFloorSuppressed(t *testing.T) {
	in := RiskLossInput{
		TotalEvents: 10,
		ByPump: []LossDimensionTally{
			{Key: "a", Label: "01", Count: 3},
			{Key: "b", Label: "02", Count: 3},
			{Key: "c", Label: "03", Count: 2},
			{Key: "d", Label: "04", Count: 2},
		},
	}
	if findings := RiskLossPatterns(in); len(findings) != 0 {
		t.Fatalf("even spread must yield no findings, got %+v", findings)
	}
}

// TestRiskLossPatterns_SingleValueSuppressed asserts a dimension with only one
// observed value (which would trivially read 100%) is suppressed — a one-pump
// station must not manufacture a "pump pattern".
func TestRiskLossPatterns_SingleValueSuppressed(t *testing.T) {
	in := RiskLossInput{
		TotalEvents: 6,
		ByPump:      []LossDimensionTally{{Key: "only", Label: "01", Count: 6}},
		ByShift: []LossDimensionTally{
			{Key: "ev", Label: "Evening", Count: 5},
			{Key: "mo", Label: "Morning", Count: 1},
		},
	}
	findings := RiskLossPatterns(in)
	// Only the multi-value shift dimension may surface; the single pump is dropped.
	if len(findings) != 1 || findings[0].Dimension != "shift" {
		t.Fatalf("expected only the shift finding, got %+v", findings)
	}
}

// TestRiskLossPatterns_TooFewEvents asserts that below the minimum-events floor
// the composer declines to call any pattern (an honest partial state).
func TestRiskLossPatterns_TooFewEvents(t *testing.T) {
	in := RiskLossInput{
		TotalEvents: 2,
		ByPump:      []LossDimensionTally{{Key: "a", Label: "01", Count: 2}},
	}
	if findings := RiskLossPatterns(in); len(findings) != 0 {
		t.Fatalf("2 events is too few to call a pattern, got %+v", findings)
	}
}

// TestRiskLoss_ValueGating asserts the sensitive loss VALUE is only mentioned in
// the headline when LossValueShown is true, and that a data-quality note explains
// the omission to non-holders (omit-not-zero).
func TestRiskLoss_ValueGating(t *testing.T) {
	base := RiskLossInput{
		StationLabel: "Mikocheni",
		TotalEvents:  5,
		WindowDays:   7,
		LossLitres:   "1240.000",
		LossValue:    "3968000.00",
	}

	// Non-holder: value hidden — no money figure in any insight, plus a DQ note.
	hidden := base
	hidden.LossValueShown = false
	rep := RiskLoss(hidden)
	for _, ins := range rep.Insights {
		if strings.Contains(ins.Message, "3968000") || strings.Contains(ins.Message, "TZS") {
			t.Fatalf("loss value leaked to a non-holder: %q", ins.Message)
		}
	}
	if !hasDQ(rep, "margin.view") {
		t.Fatalf("expected a data-quality note explaining the hidden loss value: %+v", rep.DataQuality)
	}

	// Holder: the value appears in the headline sentence.
	shown := base
	shown.LossValueShown = true
	rep = RiskLoss(shown)
	if !hasInsight(rep, "3968000") {
		t.Fatalf("expected the loss value in the headline for a holder: %+v", rep.Insights)
	}
}

// TestRiskLoss_RepeatedTanksCritical asserts a recurring loss escalates to a
// critical insight with a concrete recommended action.
func TestRiskLoss_RepeatedTanksCritical(t *testing.T) {
	rep := RiskLoss(RiskLossInput{
		StationLabel:   "Mikocheni",
		TotalEvents:    8,
		WindowDays:     7,
		RepeatedTanks:  2,
		LossLitres:     "900.000",
		LossValueShown: true,
		ByPump: []LossDimensionTally{
			{Key: "p3", Label: "03", Count: 6},
			{Key: "p1", Label: "01", Count: 2},
		},
	})
	var crit bool
	var action string
	for _, ins := range rep.Insights {
		if ins.Severity == SeverityCritical && strings.Contains(ins.Message, "recurring loss") {
			crit = true
			action = ins.RecommendedAction
		}
	}
	if !crit {
		t.Fatalf("recurring loss must produce a critical insight: %+v", rep.Insights)
	}
	if !strings.Contains(action, "pump 03") {
		t.Fatalf("recommended action should name the leading pump: %q", action)
	}
}

func hasDQ(rep Report, substr string) bool {
	for _, d := range rep.DataQuality {
		if strings.Contains(d.Message, substr) {
			return true
		}
	}
	return false
}

func hasInsight(rep Report, substr string) bool {
	for _, ins := range rep.Insights {
		if strings.Contains(ins.Message, substr) {
			return true
		}
	}
	return false
}
