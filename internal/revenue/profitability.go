package revenue

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Profitability + station-comparison aggregates (Features 10.4 / 10.6).
//
// Every money/litre figure is summed in SQL as ::numeric and returned as an
// exact decimal STRING — no figure is recomputed in Go float. Revenue/COGS/
// margin come from the same recognized-sales facts the dashboards use (sales,
// net of approved sale voids — Feature 4.3); operating expenses come from the
// posted/approved expenses ledger; both are filtered to a business-date window
// resolved through operating_days. The per-product breakdown and the cross-
// station comparison share the same costing basis (cumulative weighted-average
// landed cost; see docs/costing-policy.md), so the report ties out to the close.

// ProfitTotals is a station's profit-and-loss over a business-date window.
// Net operating result = gross margin − operating expenses. Litres is the
// recognized litres sold (net of approved voids).
type ProfitTotals struct {
	Revenue      string // net revenue recognized (ex-tax sales, net of voids)
	GrossRevenue string // gross (tax-inclusive) sales, net of voids
	TaxTotal     string
	Cogs         string
	GrossMargin  string // revenue figure the P&L uses: net revenue − COGS
	Expenses     string // posted/approved operating expenses booked to the station
	NetOperating string // gross margin − operating expenses
	LitresSold   string
	SaleCount    int
	ExpenseCount int
}

// ProfitProductLine is one product's contribution to a station's P&L.
type ProfitProductLine struct {
	ProductID   uuid.UUID
	ProductName string
	LitresSold  string
	Revenue     string // net revenue
	Cogs        string
	GrossMargin string
}

// Profitability returns a station's P&L totals over [from, to] (inclusive
// business dates). All sums run in SQL ::numeric; expenses are the station's
// approved+posted operating expenses dated in the window. The whole report is
// computed in one round-trip-friendly pair of queries (totals + per-product).
func (r *Repo) Profitability(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) (ProfitTotals, error) {
	var t ProfitTotals
	// Sales facts (net of approved voids), summed once. Margin is net revenue −
	// COGS so it never double-counts tax.
	if err := q.QueryRow(ctx, `
		WITH sale_facts AS (
		    SELECT
		        COALESCE(SUM(s.net_amount),    0) + COALESCE(SUM(v.reversal_net),    0) AS net,
		        COALESCE(SUM(s.gross_amount),  0) + COALESCE(SUM(v.reversal_gross),  0) AS gross,
		        COALESCE(SUM(s.tax_amount),    0) + COALESCE(SUM(v.reversal_tax),    0) AS tax,
		        COALESCE(SUM(s.cogs_amount),   0) + COALESCE(SUM(v.reversal_cogs),   0) AS cogs,
		        COALESCE(SUM(s.litres),        0) + COALESCE(SUM(v.reversal_litres), 0) AS litres,
		        COUNT(*) FILTER (WHERE v.id IS NULL)                                    AS sale_count
		    FROM sales s
		    JOIN operating_days od
		        ON od.id = s.operating_day_id AND od.tenant_id = s.tenant_id
		    LEFT JOIN sale_voids v
		        ON v.tenant_id = s.tenant_id AND v.sale_id = s.id AND v.status = 'approved'
		    WHERE s.tenant_id = $1 AND s.station_id = $2
		      AND od.business_date BETWEEN $3 AND $4
		),
		expense_facts AS (
		    SELECT COALESCE(SUM(e.amount), 0) AS total, COUNT(*) AS cnt
		    FROM expenses e
		    WHERE e.tenant_id = $1 AND e.station_id = $2
		      AND e.status IN ('approved', 'posted')
		      AND e.expense_date BETWEEN $3 AND $4
		)
		SELECT
		    sf.net::text,
		    sf.gross::text,
		    sf.tax::text,
		    sf.cogs::text,
		    (sf.net - sf.cogs)::text,
		    ef.total::text,
		    (sf.net - sf.cogs - ef.total)::text,
		    sf.litres::text,
		    sf.sale_count,
		    ef.cnt
		FROM sale_facts sf, expense_facts ef
	`, tenantID, stationID, from, to).Scan(
		&t.Revenue, &t.GrossRevenue, &t.TaxTotal, &t.Cogs, &t.GrossMargin,
		&t.Expenses, &t.NetOperating, &t.LitresSold, &t.SaleCount, &t.ExpenseCount,
	); err != nil {
		return ProfitTotals{}, err
	}
	return t, nil
}

// ProfitabilityByProduct returns the per-product P&L breakdown for a station
// over [from, to], ordered by net revenue (largest first). All sums run in SQL
// ::numeric; figures are net of approved voids.
func (r *Repo) ProfitabilityByProduct(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) ([]ProfitProductLine, error) {
	rows, err := q.Query(ctx, `
		SELECT
		    p.id,
		    p.name,
		    (COALESCE(SUM(s.litres),       0) + COALESCE(SUM(v.reversal_litres), 0))::text,
		    (COALESCE(SUM(s.net_amount),   0) + COALESCE(SUM(v.reversal_net),    0))::text,
		    (COALESCE(SUM(s.cogs_amount),  0) + COALESCE(SUM(v.reversal_cogs),   0))::text,
		    ((COALESCE(SUM(s.net_amount),  0) + COALESCE(SUM(v.reversal_net),    0))
		     - (COALESCE(SUM(s.cogs_amount), 0) + COALESCE(SUM(v.reversal_cogs), 0)))::text
		FROM sales s
		JOIN operating_days od
		    ON od.id = s.operating_day_id AND od.tenant_id = s.tenant_id
		JOIN products p ON p.id = s.product_id AND p.tenant_id = s.tenant_id
		LEFT JOIN sale_voids v
		    ON v.tenant_id = s.tenant_id AND v.sale_id = s.id AND v.status = 'approved'
		WHERE s.tenant_id = $1 AND s.station_id = $2
		  AND od.business_date BETWEEN $3 AND $4
		GROUP BY p.id, p.name
		ORDER BY (COALESCE(SUM(s.net_amount), 0) + COALESCE(SUM(v.reversal_net), 0)) DESC, p.name
	`, tenantID, stationID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProfitProductLine{}
	for rows.Next() {
		var l ProfitProductLine
		if err := rows.Scan(&l.ProductID, &l.ProductName, &l.LitresSold, &l.Revenue, &l.Cogs, &l.GrossMargin); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// StationComparisonRow is one station's line in the cross-station comparison:
// recognized revenue, litres, gross margin, expenses and net operating result,
// plus the stock-variance, open-risk-alert and outstanding-collections signals
// that surface a station's health. All money/litre figures are decimal strings.
type StationComparisonRow struct {
	StationID     uuid.UUID
	StationCode   string
	StationName   string
	Revenue       string // net revenue
	LitresSold    string
	GrossMargin   string
	Expenses      string
	NetOperating  string
	StockVariance string // sum of |variance litres| over reconciliations in window
	RiskAlerts    int    // open risk alerts attributed to the station
	Collections   string // outstanding credit (AR) attributed to the station's invoices
}

// StationComparison ranks the supplied stations over [from, to], summing every
// figure in SQL ::numeric. stationIDs scopes the result to exactly the stations
// the caller may read (the handler passes the actor's accessible set); an empty
// slice yields no rows. Ordered by net revenue, largest first.
//
// The per-station signals are read from the same facts the station reports use:
// stock variance from reconciliations, open risk alerts from risk_alerts, and
// outstanding collections from the credit (AR) ledger keyed to the station's
// invoices — so the comparison ties out to each station's own report.
func (r *Repo) StationComparison(ctx context.Context, tenantID uuid.UUID, stationIDs []uuid.UUID, from, to time.Time) ([]StationComparisonRow, error) {
	if len(stationIDs) == 0 {
		return []StationComparisonRow{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
		    st.id, st.code, st.name,
		    COALESCE(sf.net, 0)::text,
		    COALESCE(sf.litres, 0)::text,
		    COALESCE(sf.margin, 0)::text,
		    COALESCE(ef.total, 0)::text,
		    (COALESCE(sf.margin, 0) - COALESCE(ef.total, 0))::text,
		    COALESCE(rc.variance, 0)::text,
		    COALESCE(ra.alerts, 0),
		    COALESCE(ar.outstanding, 0)::text
		FROM stations st
		LEFT JOIN LATERAL (
		    SELECT
		        COALESCE(SUM(s.net_amount),    0) + COALESCE(SUM(v.reversal_net),    0) AS net,
		        COALESCE(SUM(s.litres),        0) + COALESCE(SUM(v.reversal_litres), 0) AS litres,
		        (COALESCE(SUM(s.net_amount),   0) + COALESCE(SUM(v.reversal_net),  0))
		          - (COALESCE(SUM(s.cogs_amount), 0) + COALESCE(SUM(v.reversal_cogs), 0)) AS margin
		    FROM sales s
		    JOIN operating_days od
		        ON od.id = s.operating_day_id AND od.tenant_id = s.tenant_id
		    LEFT JOIN sale_voids v
		        ON v.tenant_id = s.tenant_id AND v.sale_id = s.id AND v.status = 'approved'
		    WHERE s.tenant_id = st.tenant_id AND s.station_id = st.id
		      AND od.business_date BETWEEN $2 AND $3
		) sf ON true
		LEFT JOIN LATERAL (
		    SELECT COALESCE(SUM(e.amount), 0) AS total
		    FROM expenses e
		    WHERE e.tenant_id = st.tenant_id AND e.station_id = st.id
		      AND e.status IN ('approved', 'posted')
		      AND e.expense_date BETWEEN $2 AND $3
		) ef ON true
		LEFT JOIN LATERAL (
		    SELECT COALESCE(SUM(ABS(rec.variance_litres)), 0) AS variance
		    FROM tank_reconciliations rec
		    JOIN tanks tk
		        ON tk.id = rec.tank_id AND tk.tenant_id = rec.tenant_id
		    JOIN operating_days od
		        ON od.id = rec.operating_day_id AND od.tenant_id = rec.tenant_id
		    WHERE rec.tenant_id = st.tenant_id AND tk.station_id = st.id
		      AND od.business_date BETWEEN $2 AND $3
		) rc ON true
		LEFT JOIN LATERAL (
		    SELECT COUNT(*) AS alerts
		    FROM risk_alerts al
		    WHERE al.tenant_id = st.tenant_id AND al.station_id = st.id
		      AND al.status = 'open'
		) ra ON true
		LEFT JOIN LATERAL (
		    SELECT COALESCE(SUM(ci.outstanding_amount), 0) AS outstanding
		    FROM customer_invoices ci
		    WHERE ci.tenant_id = st.tenant_id AND ci.station_id = st.id
		      AND ci.status IN ('issued', 'partially_paid')
		) ar ON true
		WHERE st.tenant_id = $1 AND st.id = ANY($4)
		ORDER BY COALESCE(sf.net, 0) DESC, st.code
	`, tenantID, from, to, stationIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StationComparisonRow{}
	for rows.Next() {
		var c StationComparisonRow
		if err := rows.Scan(
			&c.StationID, &c.StationCode, &c.StationName,
			&c.Revenue, &c.LitresSold, &c.GrossMargin, &c.Expenses, &c.NetOperating,
			&c.StockVariance, &c.RiskAlerts, &c.Collections,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
