package reporting

import (
	"fmt"
	"math"
	"sort"
)

// This file holds the deterministic Delivery & Procurement composer (blueprint
// §5.7) and the SUPPLIER SCORECARD scoring model. Like every other composer here
// it takes a small, already-computed input struct (built by the HTTP handler from
// the procurement/delivery repos) and returns transparent insights + data-quality
// notes — NO AI, NO engine, NO config. The scorecard score is a pure, weighted,
// fully-explained function of supplier facts.
//
// Money/litre figures arrive as exact decimal STRINGS. They are parsed to float64
// ONLY for the heuristic/score math below (variance ratios, weighted averages);
// the original strings are never mutated and remain the source of every figure
// the report displays. The scorecard's outputs (a 0-100 score + a band) are
// advisory annotations, not persisted money.

// ---- Delivery & Procurement report composer ----

// DeliveryInput is the already-computed slice of a station's delivery &
// procurement picture a handler passes in. Litre figures are decimal strings.
type DeliveryInput struct {
	OrderedLitres   string // total ordered litres across POs in window
	ReceivedLitres  string // total received litres across receipts in window
	VarianceLitres  string // signed: received − ordered, decimal string
	DeliveryCount   int    // receipts in the window
	UnapprovedCount int    // deliveries not yet linked/approved (legacy/unmatched)
	PendingInvoices int    // supplier invoices recorded/matched/discrepancy (not approved)
	OpenDiscreps    int    // open procurement discrepancies (blocking the payable)
	LateDeliveries  int    // receipts after the PO expected_delivery_date
	PeriodComplete  bool   // every PO in the window is received/closed/cancelled
}

// Delivery builds the Delivery & Procurement report annotations: the
// ordered-vs-received fulfilment signal, late-delivery and discrepancy flags, and
// the honest data-quality notes (unapproved deliveries, pending invoices). Every
// rule is a transparent threshold.
func Delivery(in DeliveryInput) Report {
	var rep Report

	ordered, okOrd := parseDec(in.OrderedLitres)
	received, okRec := parseDec(in.ReceivedLitres)
	if okOrd && okRec && ordered > 0 {
		// Ordered and received are PERIOD aggregates windowed on their own anchors
		// (ordered on the PO date, received on the delivery date); the variance is a
		// period order-vs-arrival position, not a per-PO fulfilment rate, so the
		// wording below stays at "this period" and reports magnitudes as positive
		// quantities (no doubled-negative from a signed variance string).
		shortfall := ordered - received
		if shortfall > 0 {
			// Net shortfall this period: less arrived than was ordered. Grade by the
			// missing share of the order (>=5% warns) — a procurement reliability signal.
			pct := shortfall / ordered * 100
			sev := SeverityInfo
			if pct >= 5 {
				sev = SeverityWarning
			}
			rep.Insights = append(rep.Insights, Insight{
				Severity: sev,
				Message: fmt.Sprintf(
					"Received %s L less than ordered this period (%s of the ordered volume).",
					fmtLitres(shortfall), fmtPctMagnitude(pct)),
				RecommendedAction: "Reconcile short deliveries with the supplier and confirm the goods-receipt dips before approving invoices.",
			})
		} else if received-ordered > ordered*0.02 {
			// Net over-delivery beyond 2% this period — also worth a flag (over-billing risk).
			overage := received - ordered
			pct := overage / ordered * 100
			rep.Insights = append(rep.Insights, Insight{
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("Received %s L more than ordered this period (%s of the ordered volume).", fmtLitres(overage), fmtPctMagnitude(pct)),
			})
		}
	}

	if in.LateDeliveries > 0 {
		rep.Insights = append(rep.Insights, Insight{
			Severity: SeverityWarning,
			Message: fmt.Sprintf(
				"%d delivery(ies) arrived after their expected date — supplier lead times slipped.",
				in.LateDeliveries),
			RecommendedAction: "Review the affected suppliers' lead times and adjust reorder timing.",
		})
	}

	if in.OpenDiscreps > 0 {
		rep.Insights = append(rep.Insights, Insight{
			Severity: SeverityWarning,
			Message: fmt.Sprintf(
				"%d open procurement discrepancy(ies) are blocking supplier-invoice approval.",
				in.OpenDiscreps),
			RecommendedAction: "Resolve the open quantity/price discrepancies so the matched invoices can be approved and paid.",
		})
	}

	// Data-quality: deliveries not yet attributed to a supplier/PO read as
	// provisional procurement figures; pending invoices mean the payable is not
	// final; an incomplete period means more deliveries may still land.
	if in.UnapprovedCount > 0 {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: fmt.Sprintf(
				"%d delivery(ies) are not yet matched to a purchase order — procurement figures are provisional.",
				in.UnapprovedCount),
		})
	}
	if in.PendingInvoices > 0 {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: fmt.Sprintf(
				"%d supplier invoice(s) are not yet approved — supplier cost and payables may still change.",
				in.PendingInvoices),
		})
	}
	if !in.PeriodComplete {
		rep.DataQuality = append(rep.DataQuality, DataQualityWarning{
			Message: "Some purchase orders in this period are still open — delivery totals are not yet final.",
		})
	}
	return rep
}

// ---- Supplier scorecard (blueprint §5.7 "Supplier Scorecard Logic") ----

// SupplierFacts is one supplier's already-computed, period-windowed performance
// facts. Every litre/count figure is computed in SQL from the procurement tables;
// the scorecard turns them into a transparent 0-100 score. Price competitiveness
// is intentionally OPTIONAL (PriceKnown) — it is supplier-cost-derived and may be
// gated away from a non-cost actor, in which case its weight is redistributed so
// the score never silently penalises a supplier for a hidden dimension.
type SupplierFacts struct {
	SupplierID   string
	SupplierName string

	// On-time delivery: receipts on/before the PO expected date vs total dated
	// receipts. OnTimeTotal == 0 means we have no dated deliveries to judge.
	OnTimeCount int
	OnTimeTotal int

	// Quantity accuracy: |received − ordered| as a fraction of ordered, averaged
	// across the supplier's PO lines (a smaller mean variance scores higher).
	// QtyAccuracy is a fraction in [0,1] where 0 = perfect, already computed in SQL.
	QtyAccuracy    string // decimal string fraction (e.g. "0.015" = 1.5% mean variance)
	QtyAccuracyHas bool

	// Dispute frequency: open+resolved procurement discrepancies raised against
	// this supplier's invoices, vs the supplier's delivery count.
	DisputeCount int
	DeliveryNum  int

	// Document completeness: invoices approved (fully documented & matched) vs all
	// invoices recorded for the supplier.
	InvoicesApproved int
	InvoicesTotal    int

	// Delivery-variance history: count of deliveries whose dip_variance exceeded a
	// material threshold (computed in SQL), vs delivery count.
	DipVarianceBreaches int

	// Price competitiveness (OPTIONAL, supplier-cost-sensitive): the supplier's
	// mean landed cost per litre vs the tenant-wide mean for the same products.
	// PriceRatio < 1 = cheaper than peers (better). Omitted (PriceKnown=false)
	// when the actor cannot read cost — its weight is then redistributed.
	PriceRatio string // decimal string ratio (supplier mean / peer mean)
	PriceKnown bool
}

// SupplierScore is the scored result for one supplier: a 0-100 composite, a
// band (text + a RiskBadge-aligned severity), and the per-dimension sub-scores
// (each 0-100) so the UI can render an explainable scorecard, never a black box.
type SupplierScore struct {
	SupplierID   string `json:"supplier_id"`
	SupplierName string `json:"supplier_name"`

	Score  int    `json:"score"` // 0-100 composite
	Band   string `json:"band"`  // Excellent | Good | Fair | At risk
	Tone   string `json:"tone"`  // low | medium | high | critical (RiskBadge severity)
	Grade  string `json:"grade"` // A | B | C | D (compact display)
	Detail string `json:"detail,omitempty"`

	// Per-dimension sub-scores (0-100). PriceScore is nil when price is gated/unknown.
	OnTimeScore   int  `json:"on_time_score"`
	QuantityScore int  `json:"quantity_score"`
	DisputeScore  int  `json:"dispute_score"`
	DocumentScore int  `json:"document_score"`
	VarianceScore int  `json:"variance_score"`
	PriceScore    *int `json:"price_score,omitempty"`
	PriceIncluded bool `json:"price_included"`

	DeliveryCount int `json:"delivery_count"`
	DisputeCount  int `json:"dispute_count"`
}

// Scorecard weights (sum to 100 when price is INCLUDED). When price is excluded
// (gated/unknown), its weight is redistributed proportionally across the other
// five so the composite stays on a 0-100 scale and no supplier is penalised for a
// dimension that simply could not be measured.
const (
	wOnTime   = 25.0
	wQuantity = 25.0
	wDispute  = 15.0
	wDocument = 15.0
	wVariance = 10.0
	wPrice    = 10.0
)

// ScoreSupplier computes one supplier's deterministic scorecard from its facts.
// Each sub-score is a transparent function of a single dimension; the composite
// is their weighted mean (weights above), with the price weight redistributed
// when price is unavailable. The mapping from score → band/tone/grade is fixed.
func ScoreSupplier(f SupplierFacts) SupplierScore {
	out := SupplierScore{
		SupplierID:    f.SupplierID,
		SupplierName:  f.SupplierName,
		DeliveryCount: f.DeliveryNum,
		DisputeCount:  f.DisputeCount,
		PriceIncluded: f.PriceKnown,
	}

	// (1) On-time delivery: share of dated receipts on/before the expected date.
	// No dated deliveries → neutral 100 (we cannot fault punctuality we can't see).
	out.OnTimeScore = 100
	if f.OnTimeTotal > 0 {
		out.OnTimeScore = clampScore(float64(f.OnTimeCount) / float64(f.OnTimeTotal) * 100)
	}

	// (2) Quantity accuracy: 100 at zero mean variance, falling 5 points per 1%
	// mean variance (so 20% mean variance → 0). A supplier delivering exactly what
	// was ordered scores 100.
	out.QuantityScore = 100
	if f.QtyAccuracyHas {
		if frac, ok := parseDec(f.QtyAccuracy); ok {
			out.QuantityScore = clampScore(100 - frac*100*5)
		}
	}

	// (3) Dispute frequency: 100 at zero disputes, falling 20 points per dispute
	// PER delivery (normalised so a high-volume supplier is judged on rate, not
	// raw count). With no deliveries, fall back to raw count.
	out.DisputeScore = 100
	if f.DisputeCount > 0 {
		base := float64(f.DeliveryNum)
		if base < 1 {
			base = 1
		}
		rate := float64(f.DisputeCount) / base
		out.DisputeScore = clampScore(100 - rate*100)
	}

	// (4) Document completeness: share of invoices fully approved (matched +
	// documented). No invoices → neutral 100.
	out.DocumentScore = 100
	if f.InvoicesTotal > 0 {
		out.DocumentScore = clampScore(float64(f.InvoicesApproved) / float64(f.InvoicesTotal) * 100)
	}

	// (5) Delivery-variance history: 100 at zero dip-variance breaches, falling
	// 15 points per breach per delivery (rate-normalised like disputes).
	out.VarianceScore = 100
	if f.DipVarianceBreaches > 0 {
		base := float64(f.DeliveryNum)
		if base < 1 {
			base = 1
		}
		rate := float64(f.DipVarianceBreaches) / base
		out.VarianceScore = clampScore(100 - rate*100*0.75)
	}

	// (6) Price competitiveness (optional): 100 when the supplier's mean landed
	// cost matches peers (ratio 1.0); cheaper than peers scores above, dearer
	// below, ±10 points per 10% deviation. Only included when cost is readable.
	priceScore := 0
	if f.PriceKnown {
		priceScore = 100
		if ratio, ok := parseDec(f.PriceRatio); ok && ratio > 0 {
			priceScore = clampScore(100 - (ratio-1)*100)
		}
		ps := priceScore
		out.PriceScore = &ps
	}

	// Composite: weighted mean. Redistribute the price weight when excluded.
	type dim struct {
		score  int
		weight float64
	}
	dims := []dim{
		{out.OnTimeScore, wOnTime},
		{out.QuantityScore, wQuantity},
		{out.DisputeScore, wDispute},
		{out.DocumentScore, wDocument},
		{out.VarianceScore, wVariance},
	}
	if f.PriceKnown {
		dims = append(dims, dim{priceScore, wPrice})
	}
	var totalWeight, weighted float64
	for _, d := range dims {
		totalWeight += d.weight
		weighted += float64(d.score) * d.weight
	}
	if totalWeight > 0 {
		out.Score = clampScore(weighted / totalWeight)
	} else {
		out.Score = 100
	}

	out.Band, out.Tone, out.Grade = bandForScore(out.Score)
	return out
}

// bandForScore maps a 0-100 score onto a fixed band, a RiskBadge-aligned tone
// (low/medium/high/critical), and a compact letter grade — text + colour, never
// colour alone, so the scorecard is accessible.
func bandForScore(score int) (band, tone, grade string) {
	switch {
	case score >= 85:
		return "Excellent", "low", "A"
	case score >= 70:
		return "Good", "medium", "B"
	case score >= 50:
		return "Fair", "high", "C"
	default:
		return "At risk", "critical", "D"
	}
}

// clampScore rounds a float score into the inclusive 0-100 integer range.
func clampScore(v float64) int {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	r := int(math.Round(v))
	if r < 0 {
		return 0
	}
	if r > 100 {
		return 100
	}
	return r
}

// RankSuppliers scores a slice of supplier facts and returns the scorecards
// ordered worst-first (lowest score), so the report surfaces the suppliers that
// need attention at the top. Ties break by name for a stable order.
func RankSuppliers(facts []SupplierFacts) []SupplierScore {
	out := make([]SupplierScore, 0, len(facts))
	for i := range facts {
		out = append(out, ScoreSupplier(facts[i]))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score < out[j].Score
		}
		return out[i].SupplierName < out[j].SupplierName
	})
	return out
}
