package reporting

import (
	"strings"
	"testing"
)

// TestExecutiveNarrative_Deterministic asserts the §5.1 management narrative is
// DETERMINISTIC for fixed data (two runs over the same input yield byte-identical
// prose) and that every sentence is filled with the computed figures (the
// blueprint example shape: litres + station count, revenue period-over-period,
// fastest-growing product, station leaders, recommended focus).
func TestExecutiveNarrative_Deterministic(t *testing.T) {
	in := ExecutiveInput{
		Period:             "this-month",
		PriorPeriod:        "last month",
		StationCount:       3,
		Revenue:            ExecMetricDelta{Current: "12000000.00", Prior: "10000000.00"},
		Litres:             ExecMetricDelta{Current: "150000.000", Prior: "140000.000"},
		NetMargin:          ExecMetricDelta{Current: "2400000.00", Prior: "2000000.00"},
		MarginShown:        true,
		LossLitres:         "450.000",
		LossValue:          "1327500.00",
		LossValueShown:     true,
		CashShortages:      "85000.00",
		OpenAlerts:         2,
		OpenInvestigations: 1,
		FastestProduct:     ExecProductGrowth{Name: "Diesel", Current: "80000.000", Prior: "60000.000"},
		TopStation:         &ExecStationLine{Name: "MIK-01", NetOperating: "900000.00"},
		WeakStation:        &ExecStationLine{Name: "MSA-01", NetOperating: "120000.00"},
	}

	a := ExecutiveNarrativeText(in)
	b := ExecutiveNarrativeText(in)

	// Determinism: identical input → identical output, every field.
	if strings.Join(a.Sentences, " ") != strings.Join(b.Sentences, " ") {
		t.Fatalf("narrative sentences are not deterministic:\n a=%q\n b=%q", a.Sentences, b.Sentences)
	}
	if a.Focus != b.Focus {
		t.Fatalf("focus is not deterministic: a=%q b=%q", a.Focus, b.Focus)
	}

	joined := strings.Join(a.Sentences, " ")
	// The §5.1 example clauses are present and filled with the computed figures.
	wantContains := []string{
		"sold 150000.000 L this period across 3 stations",
		"Revenue rose 20.0% vs the prior period",
		"Diesel grew fastest at +33.3%",
		"MIK-01 led on net operating result",
		"MSA-01 trailed",
	}
	for _, w := range wantContains {
		if !strings.Contains(joined, w) {
			t.Fatalf("narrative missing %q.\nGot: %s", w, joined)
		}
	}
	// The recommended focus follows the fixed precedence: open investigations first.
	if !strings.Contains(a.Focus, "open investigation") {
		t.Fatalf("focus should lead with the open investigation, got %q", a.Focus)
	}
}

// TestExecutiveNarrative_NoPriorPeriod asserts the growth sentences are OMITTED
// (an honest partial state, never a fabricated "+0%") when there is no comparable
// prior base, while the always-present volume sentence still renders.
func TestExecutiveNarrative_NoPriorPeriod(t *testing.T) {
	in := ExecutiveInput{
		StationCount: 1,
		Revenue:      ExecMetricDelta{Current: "5000.00", Prior: ""},
		Litres:       ExecMetricDelta{Current: "1000.000", Prior: ""},
		LossLitres:   "0.000",
	}
	a := ExecutiveNarrativeText(in)
	joined := strings.Join(a.Sentences, " ")
	if !strings.Contains(joined, "sold 1000.000 L this period across 1 station") {
		t.Fatalf("volume sentence missing: %s", joined)
	}
	if strings.Contains(joined, "vs the prior period") {
		t.Fatalf("a no-prior-period narrative must not print a period-over-period clause: %s", joined)
	}
}

// TestExecutiveNarrative_GatesSensitiveFigures asserts that with MarginShown and
// LossValueShown false, the narrative NEVER mentions a margin or loss-value money
// figure (omit, never zero) — only loss litres and counts appear.
func TestExecutiveNarrative_GatesSensitiveFigures(t *testing.T) {
	in := ExecutiveInput{
		StationCount:   2,
		Revenue:        ExecMetricDelta{Current: "8000.00", Prior: "7000.00"},
		Litres:         ExecMetricDelta{Current: "2000.000", Prior: "1800.000"},
		NetMargin:      ExecMetricDelta{Current: "1500.00", Prior: "1200.00"}, // present but gated off
		MarginShown:    false,
		LossLitres:     "120.000",
		LossValue:      "354000.00", // present but gated off
		LossValueShown: false,
	}
	a := ExecutiveNarrativeText(in)
	joined := strings.Join(a.Sentences, " ")
	if strings.Contains(joined, "Net margin") {
		t.Fatalf("margin must be omitted for a non-margin holder: %s", joined)
	}
	if strings.Contains(joined, "354000") || strings.Contains(joined, "TZS)") && strings.Contains(joined, "fuel loss (") {
		t.Fatalf("loss VALUE must be omitted for a non-margin holder: %s", joined)
	}
	// Loss litres are still shown in full.
	if !strings.Contains(joined, "120 L of fuel loss") {
		t.Fatalf("loss litres should be shown in full: %s", joined)
	}
}

// TestExecutive_DataQualityGatingNotes asserts the composer raises an honest
// data-quality note for each hidden sensitive figure (margin, loss value, credit
// exposure) and for a scoped view + provisional days.
func TestExecutive_DataQualityGatingNotes(t *testing.T) {
	in := ExecutiveInput{
		StationCount:   1,
		Revenue:        ExecMetricDelta{Current: "100.00", Prior: "90.00"},
		Litres:         ExecMetricDelta{Current: "10.000", Prior: "9.000"},
		LossLitres:     "0.000",
		MarginShown:    false,
		LossValueShown: false,
		ExposureShown:  false,
		Scoped:         true,
		UnlockedDays:   2,
	}
	rep := Executive(in)
	msgs := make([]string, 0, len(rep.DataQuality))
	for _, d := range rep.DataQuality {
		msgs = append(msgs, d.Message)
	}
	all := strings.Join(msgs, " | ")
	for _, want := range []string{"margin.view", "Fuel-loss value", "Credit exposure", "limited to the stations", "not yet locked"} {
		if !strings.Contains(all, want) {
			t.Fatalf("expected a data-quality note containing %q, got:\n%s", want, all)
		}
	}
}

// TestFastestGrowingProduct picks the product whose litres grew the most by
// period-over-period percentage (deterministic, ties broken by current litres
// then name), and returns ok=false when no product has a comparable prior base.
func TestFastestGrowingProduct(t *testing.T) {
	got, ok := FastestGrowingProduct([]ExecProductGrowth{
		{Name: "Petrol", Current: "110.000", Prior: "100.000"}, // +10%
		{Name: "Diesel", Current: "150.000", Prior: "100.000"}, // +50% — winner
		{Name: "Kerosene", Current: "0.000", Prior: "0.000"},   // no base, skipped
	})
	if !ok || got.Name != "Diesel" {
		t.Fatalf("fastest product = %+v ok=%v, want Diesel", got, ok)
	}

	// No comparable prior base anywhere → ok=false (honest partial state).
	if _, ok := FastestGrowingProduct([]ExecProductGrowth{
		{Name: "Petrol", Current: "10.000", Prior: "0.000"},
	}); ok {
		t.Fatalf("with no positive prior base, FastestGrowingProduct must return ok=false")
	}
}

// TestExecutive_RecommendedFocusPrecedence asserts the recommended focus follows
// a FIXED precedence (so it is deterministic): open investigations beat open
// alerts, which beat a loss-making station, which beats pending approvals.
func TestExecutive_RecommendedFocusPrecedence(t *testing.T) {
	base := ExecutiveInput{StationCount: 1, Litres: ExecMetricDelta{Current: "1"}, LossLitres: "0"}

	withInvest := base
	withInvest.OpenInvestigations = 1
	withInvest.OpenAlerts = 3
	if f := execRecommendedFocus(withInvest); !strings.Contains(f, "open investigation") {
		t.Fatalf("investigations must win precedence, got %q", f)
	}

	withAlerts := base
	withAlerts.OpenAlerts = 2
	withAlerts.WeakStation = &ExecStationLine{Name: "X", NetOperating: "-50"}
	if f := execRecommendedFocus(withAlerts); !strings.Contains(f, "open risk alert") {
		t.Fatalf("open alerts must beat a loss-making station, got %q", f)
	}

	withWeak := base
	withWeak.WeakStation = &ExecStationLine{Name: "MSA-01", NetOperating: "-100.00"}
	withWeak.PendingApprovals = 5
	if f := execRecommendedFocus(withWeak); !strings.Contains(f, "MSA-01") {
		t.Fatalf("a loss-making station must beat pending approvals, got %q", f)
	}
}
