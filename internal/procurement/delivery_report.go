package procurement

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// Delivery & Procurement report aggregations (blueprint §5.7).
//
// Every litre/money figure is summed in SQL and read as an exact decimal STRING
// (numeric::text) — NEVER a Go float. The report handler parses these to float
// only for chart geometry and display ratios, never to recompute a money figure.
//
// All reads are station-scoped (deliveries reach a station through their tank;
// purchase_orders / supplier_invoices carry station_id directly) and tenant-bound
// (RLS plus an explicit tenant_id predicate). The window is [from, to] on the
// delivery received_at / PO created_at, matching the Sales report's window
// semantics.

// DeliveryTotals is the report's KPI hero: ordered vs loaded vs received litres,
// the signed variance, delivery count, and the late/unmatched counts that temper
// the figures. Cost fields are OPTIONAL — they are populated only when the actor
// may read supplier cost (margin.view); a non-cost actor gets the empty string so
// the handler omits the cost KPI entirely.
type DeliveryTotals struct {
	OrderedLitres   string // SUM(po line ordered) for POs created in window
	ReceivedLitres  string // SUM(delivery volume) in window
	VarianceLitres  string // received − ordered (computed in SQL)
	DeliveryCount   int
	LateDeliveries  int    // received after the PO expected_delivery_date
	UnmatchedCount  int    // deliveries with no supplier/PO attribution (legacy)
	FuelCostTotal   string // SUM(landed_cost_total) — sensitive (cost)
	AvgCostPerLitre string // weighted mean landed cost / litre — sensitive (cost)
}

// DeliveryTotals returns the ordered/received/variance/cost KPIs for a station
// over [from, to]. ordered is summed from purchase_order_lines on POs created in
// the window; received and cost from deliveries received in the window. withCost
// gates the supplier-cost figures (left blank when false).
func (r *Repo) DeliveryTotals(ctx context.Context, tenantID, stationID uuid.UUID, from, to time.Time, withCost bool) (DeliveryTotals, error) {
	var t DeliveryTotals

	// Received litres + cost + counts, from deliveries reaching this station via
	// their tank. received_at in [from, to). landed_cost_total is nullable on
	// legacy rows, so COALESCE to 0.
	costSelect := "'' AS fuel_cost, '' AS avg_cost"
	if withCost {
		// Avg cost / litre is a weighted mean over COSTED deliveries only: a legacy
		// row with a NULL landed_cost_total contributes neither cost (numerator) nor
		// litres (denominator), so uncosted volume can't dilute the figure. The fuel
		// cost KPI still sums every priced row (NULLs coalesce to 0, i.e. add nothing).
		costSelect = `
			COALESCE(SUM(d.landed_cost_total), 0)::text AS fuel_cost,
			CASE WHEN COALESCE(SUM(d.volume_litres) FILTER (WHERE d.landed_cost_total IS NOT NULL), 0) > 0
			     THEN (SUM(d.landed_cost_total) FILTER (WHERE d.landed_cost_total IS NOT NULL)
			           / SUM(d.volume_litres) FILTER (WHERE d.landed_cost_total IS NOT NULL))::numeric(14,4)::text
			     ELSE '0' END AS avg_cost`
	}
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(d.volume_litres), 0)::text AS received,
			COUNT(*) AS delivery_count,
			COUNT(*) FILTER (
				WHERE d.purchase_order_id IS NOT NULL
				  AND po.expected_delivery_date IS NOT NULL
				  AND d.received_at::date > po.expected_delivery_date
			) AS late_count,
			COUNT(*) FILTER (WHERE d.supplier_id IS NULL OR d.purchase_order_id IS NULL) AS unmatched_count,
			`+costSelect+`
		FROM deliveries d
		JOIN tanks t ON t.id = d.tank_id AND t.tenant_id = d.tenant_id
		LEFT JOIN purchase_orders po
		       ON po.id = d.purchase_order_id AND po.tenant_id = d.tenant_id
		WHERE d.tenant_id = $1 AND t.station_id = $2
		  AND d.received_at >= $3 AND d.received_at < $4
	`, tenantID, stationID, from, to.AddDate(0, 0, 1)).Scan(
		&t.ReceivedLitres, &t.DeliveryCount, &t.LateDeliveries, &t.UnmatchedCount,
		&t.FuelCostTotal, &t.AvgCostPerLitre,
	)
	if err != nil {
		return t, err
	}

	// Ordered litres from PO lines for POs raised at this station in the window.
	err = r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(pol.ordered_litres), 0)::text
		FROM purchase_order_lines pol
		JOIN purchase_orders po
		  ON po.id = pol.purchase_order_id AND po.tenant_id = pol.tenant_id
		WHERE pol.tenant_id = $1 AND po.station_id = $2
		  AND po.created_at >= $3 AND po.created_at < $4
	`, tenantID, stationID, from, to.AddDate(0, 0, 1)).Scan(&t.OrderedLitres)
	if err != nil {
		return t, err
	}

	// Variance = received − ordered, computed once in SQL on the two decimal
	// strings so it stays exact (never recomputed from floats in Go).
	err = r.pool.QueryRow(ctx, `SELECT ($1::numeric - $2::numeric)::text`,
		t.ReceivedLitres, t.OrderedLitres).Scan(&t.VarianceLitres)
	if err != nil {
		return t, err
	}
	return t, nil
}

// DeliveryComparisonRow is one product's ordered/loaded/received comparison for
// the §5.7 bar chart. "Loaded" is the depot-loaded volume; the schema records
// received volume and ordered volume but no separate loaded leg, so loaded is the
// PO-confirmed (received_litres-on-PO) figure where present, else equals ordered
// — a documented, honest stand-in (no loaded-at-depot field exists yet).
type DeliveryComparisonRow struct {
	ProductID    uuid.UUID
	ProductName  string
	ProductColor string
	Ordered      string
	Loaded       string
	Received     string
}

// DeliveryComparison returns the per-product ordered/loaded/received litres for
// the window. Ordered comes from PO lines; loaded from PO confirmed/received
// litres; received from deliveries. All decimal strings.
func (r *Repo) DeliveryComparison(ctx context.Context, tenantID, stationID uuid.UUID, from, to time.Time) ([]DeliveryComparisonRow, error) {
	rows, err := r.pool.Query(ctx, `
		WITH ordered AS (
			SELECT pol.product_id,
			       SUM(pol.ordered_litres)  AS ordered_litres,
			       SUM(pol.received_litres) AS loaded_litres
			FROM purchase_order_lines pol
			JOIN purchase_orders po
			  ON po.id = pol.purchase_order_id AND po.tenant_id = pol.tenant_id
			WHERE pol.tenant_id = $1 AND po.station_id = $2
			  AND po.created_at >= $3 AND po.created_at < $4
			GROUP BY pol.product_id
		),
		received AS (
			SELECT t.product_id, SUM(d.volume_litres) AS received_litres
			FROM deliveries d
			JOIN tanks t ON t.id = d.tank_id AND t.tenant_id = d.tenant_id
			WHERE d.tenant_id = $1 AND t.station_id = $2
			  AND d.received_at >= $3 AND d.received_at < $4
			GROUP BY t.product_id
		)
		SELECT p.id, p.name, COALESCE(p.color, ''),
		       COALESCE(o.ordered_litres, 0)::text,
		       COALESCE(o.loaded_litres, 0)::text,
		       COALESCE(rc.received_litres, 0)::text
		FROM products p
		LEFT JOIN ordered  o  ON o.product_id  = p.id
		LEFT JOIN received rc ON rc.product_id = p.id
		WHERE p.tenant_id = $1
		  AND (o.product_id IS NOT NULL OR rc.product_id IS NOT NULL)
		ORDER BY p.name
	`, tenantID, stationID, from, to.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeliveryComparisonRow{}
	for rows.Next() {
		var row DeliveryComparisonRow
		if err := rows.Scan(
			&row.ProductID, &row.ProductName, &row.ProductColor,
			&row.Ordered, &row.Loaded, &row.Received,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// DeliveryLine is one delivery receipt for the drillable table + variance chart:
// when, which supplier/product, the declared volume, the dip variance, the match
// status, and (gated) the landed cost. Cost is the empty string for a non-cost
// actor so it never leaks.
type DeliveryLine struct {
	DeliveryID   uuid.UUID
	ReceivedAt   time.Time
	SupplierName string
	ProductName  string
	VolumeLitres string
	DipVariance  string // dip_variance_litres (declared − measured), decimal string
	MatchStatus  string
	Late         bool
	LandedCost   string // landed_cost_total — sensitive (cost), "" when gated
}

// DeliveryLines returns the individual delivery receipts for the window (newest
// first), with the dip variance, match status, late flag and (optionally) the
// landed cost. withCost gates the cost column.
func (r *Repo) DeliveryLines(ctx context.Context, tenantID, stationID uuid.UUID, from, to time.Time, withCost bool) ([]DeliveryLine, error) {
	costSelect := "'' AS landed_cost"
	if withCost {
		costSelect = "COALESCE(d.landed_cost_total, 0)::text AS landed_cost"
	}
	rows, err := r.pool.Query(ctx, `
		SELECT d.id, d.received_at,
		       COALESCE(s.name, 'Unattributed'),
		       COALESCE(p.name, '—'),
		       d.volume_litres::text,
		       COALESCE(d.dip_variance_litres, 0)::text,
		       d.match_status,
		       (d.purchase_order_id IS NOT NULL
		        AND po.expected_delivery_date IS NOT NULL
		        AND d.received_at::date > po.expected_delivery_date) AS late,
		       `+costSelect+`
		FROM deliveries d
		JOIN tanks t ON t.id = d.tank_id AND t.tenant_id = d.tenant_id
		LEFT JOIN products p ON p.id = t.product_id AND p.tenant_id = t.tenant_id
		LEFT JOIN suppliers s ON s.id = d.supplier_id AND s.tenant_id = d.tenant_id
		LEFT JOIN purchase_orders po
		       ON po.id = d.purchase_order_id AND po.tenant_id = d.tenant_id
		WHERE d.tenant_id = $1 AND t.station_id = $2
		  AND d.received_at >= $3 AND d.received_at < $4
		ORDER BY d.received_at DESC
		LIMIT 500
	`, tenantID, stationID, from, to.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeliveryLine{}
	for rows.Next() {
		var l DeliveryLine
		if err := rows.Scan(
			&l.DeliveryID, &l.ReceivedAt, &l.SupplierName, &l.ProductName,
			&l.VolumeLitres, &l.DipVariance, &l.MatchStatus, &l.Late, &l.LandedCost,
		); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// PurchaseOrderStatusRow is one PO-status bucket for the procurement pipeline.
type PurchaseOrderStatusRow struct {
	Status string
	Count  int
}

// PurchaseOrderPipeline returns the count of purchase orders by status for the
// station in the window — the procurement pipeline funnel.
func (r *Repo) PurchaseOrderPipeline(ctx context.Context, tenantID, stationID uuid.UUID, from, to time.Time) ([]PurchaseOrderStatusRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT status, COUNT(*)
		FROM purchase_orders
		WHERE tenant_id = $1 AND station_id = $2
		  AND created_at >= $3 AND created_at < $4
		GROUP BY status
		ORDER BY status
	`, tenantID, stationID, from, to.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PurchaseOrderStatusRow{}
	for rows.Next() {
		var row PurchaseOrderStatusRow
		if err := rows.Scan(&row.Status, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// DeliveryQuality is the report's data-quality picture: deliveries not matched to
// a PO, supplier invoices not yet approved, open procurement discrepancies, and
// whether every PO in the window is in a terminal state.
type DeliveryQuality struct {
	UnmatchedDeliveries int
	PendingInvoices     int
	OpenDiscrepancies   int
	OpenPurchaseOrders  int
	LateDeliveries      int
}

// DeliveryWindowQuality computes the data-quality counts for the window.
func (r *Repo) DeliveryWindowQuality(ctx context.Context, tenantID, stationID uuid.UUID, from, to time.Time) (DeliveryQuality, error) {
	var q DeliveryQuality
	hi := to.AddDate(0, 0, 1)

	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE d.supplier_id IS NULL OR d.purchase_order_id IS NULL),
			COUNT(*) FILTER (
				WHERE d.purchase_order_id IS NOT NULL AND po.expected_delivery_date IS NOT NULL
				  AND d.received_at::date > po.expected_delivery_date)
		FROM deliveries d
		JOIN tanks t ON t.id = d.tank_id AND t.tenant_id = d.tenant_id
		LEFT JOIN purchase_orders po ON po.id = d.purchase_order_id AND po.tenant_id = d.tenant_id
		WHERE d.tenant_id = $1 AND t.station_id = $2
		  AND d.received_at >= $3 AND d.received_at < $4
	`, tenantID, stationID, from, hi).Scan(&q.UnmatchedDeliveries, &q.LateDeliveries); err != nil {
		return q, err
	}

	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM supplier_invoices
		WHERE tenant_id = $1 AND station_id = $2
		  AND status <> 'approved'
		  AND received_at >= $3 AND received_at < $4
	`, tenantID, stationID, from, hi).Scan(&q.PendingInvoices); err != nil {
		return q, err
	}

	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM procurement_discrepancies pd
		JOIN supplier_invoices inv ON inv.id = pd.supplier_invoice_id AND inv.tenant_id = pd.tenant_id
		WHERE pd.tenant_id = $1 AND inv.station_id = $2 AND pd.status = 'open'
	`, tenantID, stationID).Scan(&q.OpenDiscrepancies); err != nil {
		return q, err
	}

	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM purchase_orders
		WHERE tenant_id = $1 AND station_id = $2
		  AND status NOT IN ('received', 'closed', 'cancelled')
		  AND created_at >= $3 AND created_at < $4
	`, tenantID, stationID, from, hi).Scan(&q.OpenPurchaseOrders); err != nil {
		return q, err
	}
	return q, nil
}

// SupplierFactsRow is one supplier's raw, SQL-computed performance facts for the
// scorecard (the reporting.ScoreSupplier input is built from these). Litre/ratio
// figures are decimal strings; the price ratio is populated only when withCost.
type SupplierFactsRow struct {
	SupplierID   uuid.UUID
	SupplierName string

	OnTimeCount int
	OnTimeTotal int

	QtyAccuracy    string // mean |received − ordered| / ordered across PO lines
	QtyAccuracyHas bool

	DisputeCount  int
	DeliveryCount int

	InvoicesApproved int
	InvoicesTotal    int

	DipVarianceBreaches int

	PriceRatio string // supplier mean landed-cost-per-litre / peer mean
	PriceKnown bool
}

// SupplierScorecardFacts computes per-supplier performance facts for every
// supplier that delivered to the station (or had a PO/invoice there) in the
// window. The figures are deterministic SQL aggregates; reporting.ScoreSupplier
// turns them into the explainable scorecard. withCost gates the price-ratio
// dimension (left absent when the actor cannot read supplier cost).
func (r *Repo) SupplierScorecardFacts(ctx context.Context, tenantID, stationID uuid.UUID, from, to time.Time, withCost bool) ([]SupplierFactsRow, error) {
	hi := to.AddDate(0, 0, 1)

	// Materialise the dip-variance threshold (litres) once. A breach is a delivery
	// whose |dip_variance_litres| exceeds 0.5% of its declared volume.
	rows, err := r.pool.Query(ctx, `
		WITH del AS (
			SELECT d.supplier_id,
			       COUNT(*) AS delivery_count,
			       COUNT(*) FILTER (
			         WHERE d.dip_variance_litres IS NOT NULL
			           AND abs(d.dip_variance_litres) > d.volume_litres * 0.005
			       ) AS dip_breaches,
			       COUNT(*) FILTER (
			         WHERE d.purchase_order_id IS NOT NULL AND po.expected_delivery_date IS NOT NULL
			       ) AS dated_total,
			       COUNT(*) FILTER (
			         WHERE d.purchase_order_id IS NOT NULL AND po.expected_delivery_date IS NOT NULL
			           AND d.received_at::date <= po.expected_delivery_date
			       ) AS on_time
			FROM deliveries d
			JOIN tanks t ON t.id = d.tank_id AND t.tenant_id = d.tenant_id
			LEFT JOIN purchase_orders po ON po.id = d.purchase_order_id AND po.tenant_id = d.tenant_id
			WHERE d.tenant_id = $1 AND t.station_id = $2 AND d.supplier_id IS NOT NULL
			  AND d.received_at >= $3 AND d.received_at < $4
			GROUP BY d.supplier_id
		),
		qty AS (
			SELECT po.supplier_id,
			       AVG(CASE WHEN pol.ordered_litres > 0
			                THEN abs(pol.received_litres - pol.ordered_litres) / pol.ordered_litres
			                ELSE 0 END) AS mean_variance,
			       COUNT(*) AS line_count
			FROM purchase_order_lines pol
			JOIN purchase_orders po ON po.id = pol.purchase_order_id AND po.tenant_id = pol.tenant_id
			WHERE pol.tenant_id = $1 AND po.station_id = $2
			  AND po.status IN ('partially_received', 'received', 'closed')
			  AND po.created_at >= $3 AND po.created_at < $4
			GROUP BY po.supplier_id
		),
		inv AS (
			SELECT supplier_id,
			       COUNT(*) AS invoices_total,
			       COUNT(*) FILTER (WHERE status = 'approved') AS invoices_approved
			FROM supplier_invoices
			WHERE tenant_id = $1 AND station_id = $2
			  AND received_at >= $3 AND received_at < $4
			GROUP BY supplier_id
		),
		disp AS (
			SELECT inv.supplier_id, COUNT(*) AS dispute_count
			FROM procurement_discrepancies pd
			JOIN supplier_invoices inv ON inv.id = pd.supplier_invoice_id AND inv.tenant_id = pd.tenant_id
			WHERE pd.tenant_id = $1 AND inv.station_id = $2
			  AND inv.received_at >= $3 AND inv.received_at < $4
			GROUP BY inv.supplier_id
		),
		price AS (
			SELECT d.supplier_id, AVG(d.landed_cost_per_litre) AS supplier_mean
			FROM deliveries d
			JOIN tanks t ON t.id = d.tank_id AND t.tenant_id = d.tenant_id
			WHERE d.tenant_id = $1 AND t.station_id = $2
			  AND d.landed_cost_per_litre IS NOT NULL
			  AND d.received_at >= $3 AND d.received_at < $4
			GROUP BY d.supplier_id
		),
		peer AS (
			SELECT AVG(d.landed_cost_per_litre) AS peer_mean
			FROM deliveries d
			JOIN tanks t ON t.id = d.tank_id AND t.tenant_id = d.tenant_id
			WHERE d.tenant_id = $1 AND t.station_id = $2
			  AND d.landed_cost_per_litre IS NOT NULL
			  AND d.received_at >= $3 AND d.received_at < $4
		)
		SELECT s.id, s.name,
		       COALESCE(del.on_time, 0), COALESCE(del.dated_total, 0),
		       qty.mean_variance,
		       COALESCE(disp.dispute_count, 0), COALESCE(del.delivery_count, 0),
		       COALESCE(inv.invoices_approved, 0), COALESCE(inv.invoices_total, 0),
		       COALESCE(del.dip_breaches, 0),
		       CASE WHEN $5 AND price.supplier_mean IS NOT NULL AND peer.peer_mean IS NOT NULL AND peer.peer_mean > 0
		            THEN (price.supplier_mean / peer.peer_mean)::numeric(14,4)::text
		            ELSE NULL END AS price_ratio
		FROM suppliers s
		LEFT JOIN del  ON del.supplier_id  = s.id
		LEFT JOIN qty  ON qty.supplier_id  = s.id
		LEFT JOIN inv  ON inv.supplier_id  = s.id
		LEFT JOIN disp ON disp.supplier_id = s.id
		LEFT JOIN price ON price.supplier_id = s.id
		CROSS JOIN peer
		WHERE s.tenant_id = $1
		  AND (del.supplier_id IS NOT NULL OR qty.supplier_id IS NOT NULL OR inv.supplier_id IS NOT NULL)
		ORDER BY s.name
	`, tenantID, stationID, from, hi, withCost)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierFactsRow{}
	for rows.Next() {
		var f SupplierFactsRow
		var meanVar *float64
		var priceRatio *string
		if err := rows.Scan(
			&f.SupplierID, &f.SupplierName,
			&f.OnTimeCount, &f.OnTimeTotal,
			&meanVar,
			&f.DisputeCount, &f.DeliveryCount,
			&f.InvoicesApproved, &f.InvoicesTotal,
			&f.DipVarianceBreaches,
			&priceRatio,
		); err != nil {
			return nil, err
		}
		if meanVar != nil {
			f.QtyAccuracy = formatFraction(*meanVar)
			f.QtyAccuracyHas = true
		}
		if priceRatio != nil {
			f.PriceRatio = *priceRatio
			f.PriceKnown = true
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// formatFraction renders a float fraction with 6 decimals for the scorecard
// input string (parsed back to float only inside the deterministic score math).
func formatFraction(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}
