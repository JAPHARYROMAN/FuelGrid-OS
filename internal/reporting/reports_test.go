package reporting

import (
	"strings"
	"testing"
)

func series(vals ...string) []PeriodPoint {
	pts := make([]PeriodPoint, len(vals))
	for i, v := range vals {
		pts[i] = PeriodPoint{Label: v, Value: v}
	}
	return pts
}

func TestPeriodOverPeriod(t *testing.T) {
	ins, ok := PeriodOverPeriod("Gross revenue", series("100", "114"))
	if !ok {
		t.Fatal("expected an insight")
	}
	if !strings.Contains(ins.Message, "up") || !strings.Contains(ins.Message, "+14.0%") {
		t.Fatalf("unexpected message: %q", ins.Message)
	}
	if _, ok := PeriodOverPeriod("X", series("100")); ok {
		t.Fatal("single point must not yield an insight")
	}
}

func TestVarianceVs30dAverage(t *testing.T) {
	// avg of 100,100,100 = 100; latest 150 = +50% > 20% threshold.
	ins, ok := VarianceVs30dAverage("Gross revenue", series("100", "100", "100", "150"), 20)
	if !ok || ins.Severity != SeverityWarning {
		t.Fatalf("expected a warning, got ok=%v ins=%+v", ok, ins)
	}
	if _, ok := VarianceVs30dAverage("X", series("100", "100", "105"), 20); ok {
		t.Fatal("within threshold must not warn")
	}
}

func TestDailyCloseCashVariance(t *testing.T) {
	rep := DailyClose(DailyCloseInput{
		GrossSeries:     series("100", "110"),
		CashVariance:    "500",
		CashVarianceTol: "100",
		DayLocked:       true,
	})
	var found bool
	for _, ins := range rep.Insights {
		if strings.Contains(ins.Message, "Cash drawer") {
			found = true
			if ins.Severity != SeverityCritical {
				t.Fatalf("500 vs tol 100 should be critical, got %s", ins.Severity)
			}
		}
	}
	if !found {
		t.Fatal("expected a cash-variance insight")
	}
}

func TestDailyCloseUnclosedShiftsAndLock(t *testing.T) {
	rep := DailyClose(DailyCloseInput{UnclosedShiftCount: 2, DayLocked: false})
	if len(rep.DataQuality) != 2 {
		t.Fatalf("expected 2 data-quality warnings, got %d", len(rep.DataQuality))
	}
}

func TestStockReconciliationOverTolerance(t *testing.T) {
	rep := StockReconciliation(StockReconInput{
		Tanks: []TankRecon{
			{TankLabel: "T1", VariancePercent: "3.5", TolerancePercent: "1.0", Status: "over_tolerance", HasPhysicalDip: true},
			{TankLabel: "T2", VariancePercent: "0.1", TolerancePercent: "1.0", Status: "within_tolerance", HasPhysicalDip: false},
		},
		AllShiftsClosed: true,
	})
	if len(rep.Insights) != 1 {
		t.Fatalf("expected 1 over-tolerance insight, got %d", len(rep.Insights))
	}
	if len(rep.DataQuality) != 1 || !strings.Contains(rep.DataQuality[0].Message, "book-only") {
		t.Fatalf("expected a missing-dip data-quality warning, got %+v", rep.DataQuality)
	}
}

func TestCustomerAgingConcentration(t *testing.T) {
	rep := CustomerAging(CustomerAgingInput{Customers: []AgingCustomer{
		{Name: "Acme", Balance: "9000"},
		{Name: "Beta", Balance: "1000"},
	}})
	var concentrated bool
	for _, ins := range rep.Insights {
		if strings.Contains(ins.Message, "concentrated credit risk") {
			concentrated = true
		}
	}
	if !concentrated {
		t.Fatal("expected a concentration warning when one customer holds 90%")
	}
}

func TestSalesNegativeMargin(t *testing.T) {
	rep := SalesSummary(SalesInput{
		GrossSeries:  series("100", "110"),
		MarginSeries: series("10", "-5"),
		PeriodLocked: true,
	})
	var neg bool
	for _, ins := range rep.Insights {
		if ins.Severity == SeverityCritical && strings.Contains(ins.Message, "negative") {
			neg = true
		}
	}
	if !neg {
		t.Fatal("expected a critical negative-margin insight")
	}
}
