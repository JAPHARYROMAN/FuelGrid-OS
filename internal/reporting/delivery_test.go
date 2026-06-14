package reporting

import (
	"strings"
	"testing"
)

func TestDeliveryShortfallInsight(t *testing.T) {
	// Ordered 1000, received 900 → 10% short (>=5% warns).
	rep := Delivery(DeliveryInput{
		OrderedLitres:  "1000",
		ReceivedLitres: "900",
		VarianceLitres: "-100",
		DeliveryCount:  3,
		PeriodComplete: true,
	})
	if len(rep.Insights) != 1 {
		t.Fatalf("expected one shortfall insight, got %d: %+v", len(rep.Insights), rep.Insights)
	}
	if rep.Insights[0].Severity != SeverityWarning {
		t.Fatalf("10%% short must warn, got %s", rep.Insights[0].Severity)
	}
	if !strings.Contains(rep.Insights[0].Message, "less than ordered") {
		t.Fatalf("unexpected message: %q", rep.Insights[0].Message)
	}
}

func TestDeliverySmallShortfallIsInfo(t *testing.T) {
	// Ordered 1000, received 980 → 2% short (<5% → info, not warning).
	rep := Delivery(DeliveryInput{
		OrderedLitres: "1000", ReceivedLitres: "980", VarianceLitres: "-20",
		DeliveryCount: 1, PeriodComplete: true,
	})
	if len(rep.Insights) != 1 || rep.Insights[0].Severity != SeverityInfo {
		t.Fatalf("2%% short must be info, got %+v", rep.Insights)
	}
}

func TestDeliveryDataQuality(t *testing.T) {
	rep := Delivery(DeliveryInput{
		OrderedLitres: "1000", ReceivedLitres: "1000", VarianceLitres: "0",
		DeliveryCount: 2, UnapprovedCount: 1, PendingInvoices: 2,
		OpenDiscreps: 1, LateDeliveries: 1, PeriodComplete: false,
	})
	// unmatched + pending invoices + incomplete period = 3 DQ notes.
	if len(rep.DataQuality) != 3 {
		t.Fatalf("expected 3 data-quality notes, got %d: %+v", len(rep.DataQuality), rep.DataQuality)
	}
	// late + open discrepancy = 2 insights (no fulfilment shortfall: balanced).
	if len(rep.Insights) != 2 {
		t.Fatalf("expected 2 insights (late + discrepancy), got %d: %+v", len(rep.Insights), rep.Insights)
	}
}

func TestScoreSupplierPerfect(t *testing.T) {
	// A flawless supplier with cost included scores 100 / Excellent / A.
	sc := ScoreSupplier(SupplierFacts{
		SupplierID: "s1", SupplierName: "Acme",
		OnTimeCount: 10, OnTimeTotal: 10,
		QtyAccuracy: "0", QtyAccuracyHas: true,
		DisputeCount: 0, DeliveryNum: 10,
		InvoicesApproved: 5, InvoicesTotal: 5,
		DipVarianceBreaches: 0,
		PriceRatio:          "1.0", PriceKnown: true,
	})
	if sc.Score != 100 || sc.Band != "Excellent" || sc.Grade != "A" || sc.Tone != "low" {
		t.Fatalf("perfect supplier mis-scored: %+v", sc)
	}
	if sc.PriceScore == nil || *sc.PriceScore != 100 {
		t.Fatalf("price score should be 100, got %v", sc.PriceScore)
	}
	if !sc.PriceIncluded {
		t.Fatal("price should be included")
	}
}

func TestScoreSupplierAtRisk(t *testing.T) {
	// Half late, big quantity variance, disputes on every delivery, no approved
	// invoices, dip breaches everywhere → bottom band.
	sc := ScoreSupplier(SupplierFacts{
		SupplierID: "s2", SupplierName: "Risky",
		OnTimeCount: 5, OnTimeTotal: 10,
		QtyAccuracy: "0.2", QtyAccuracyHas: true, // 20% mean variance → quantity 0
		DisputeCount: 10, DeliveryNum: 10, // dispute rate 1.0 → 0
		InvoicesApproved: 0, InvoicesTotal: 4, // document 0
		DipVarianceBreaches: 10,                      // variance rate 1.0 → 25
		PriceRatio:          "1.5", PriceKnown: true, // 50% dearer → 50
	})
	if sc.Band != "At risk" || sc.Grade != "D" || sc.Tone != "critical" {
		t.Fatalf("risky supplier should be at risk, got %+v", sc)
	}
	if sc.OnTimeScore != 50 || sc.QuantityScore != 0 || sc.DisputeScore != 0 || sc.DocumentScore != 0 {
		t.Fatalf("sub-scores wrong: %+v", sc)
	}
}

func TestScoreSupplierPriceGatedRedistributes(t *testing.T) {
	// Same supplier with and without price. The composite must stay on a 0-100
	// scale and NOT be dragged down by an absent (gated) price dimension.
	base := SupplierFacts{
		SupplierID: "s3", SupplierName: "Mid",
		OnTimeCount: 8, OnTimeTotal: 10,
		QtyAccuracy: "0.04", QtyAccuracyHas: true,
		DisputeCount: 1, DeliveryNum: 10,
		InvoicesApproved: 4, InvoicesTotal: 5,
		DipVarianceBreaches: 1,
	}
	withPrice := base
	withPrice.PriceRatio = "1.0"
	withPrice.PriceKnown = true

	gated := ScoreSupplier(base)         // price excluded
	included := ScoreSupplier(withPrice) // price = 100 (peer-matched)
	if gated.PriceScore != nil || gated.PriceIncluded {
		t.Fatalf("gated score must omit price: %+v", gated)
	}
	if included.PriceScore == nil || !included.PriceIncluded {
		t.Fatalf("included score must carry price: %+v", included)
	}
	// Both must be valid 0-100 scores; neither should be 0 just because price is
	// missing (redistribution keeps the composite honest).
	if gated.Score <= 0 || gated.Score > 100 {
		t.Fatalf("gated composite out of range: %d", gated.Score)
	}
}

func TestRankSuppliersWorstFirst(t *testing.T) {
	ranked := RankSuppliers([]SupplierFacts{
		{SupplierID: "good", SupplierName: "Good", OnTimeCount: 10, OnTimeTotal: 10,
			QtyAccuracy: "0", QtyAccuracyHas: true, DeliveryNum: 5, InvoicesApproved: 5, InvoicesTotal: 5},
		{SupplierID: "bad", SupplierName: "Bad", OnTimeCount: 1, OnTimeTotal: 10,
			QtyAccuracy: "0.3", QtyAccuracyHas: true, DisputeCount: 10, DeliveryNum: 10,
			InvoicesApproved: 0, InvoicesTotal: 5, DipVarianceBreaches: 10},
	})
	if len(ranked) != 2 {
		t.Fatalf("expected 2 scorecards, got %d", len(ranked))
	}
	if ranked[0].SupplierID != "bad" {
		t.Fatalf("worst supplier must rank first, got %q", ranked[0].SupplierID)
	}
	if ranked[0].Score >= ranked[1].Score {
		t.Fatalf("worst (%d) should score below best (%d)", ranked[0].Score, ranked[1].Score)
	}
}
