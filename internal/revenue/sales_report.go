package revenue

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Sales report aggregations (Reports Center §5.2).
//
// Every money/litre figure is summed in SQL as ::numeric and returned as an
// exact decimal STRING — no figure is recomputed in Go float. All sums are NET
// of approved sale voids (Feature 4.3): an approved void carries the sale's
// amounts negated (reversal_*), so adding them reverses the voided sale without
// mutating the append-only sale row. The grain is one recognized sale per
// (shift, nozzle) (migration 0033's uq_sales_shift_nozzle), so the attendant /
// nozzle / hour breakdowns join shift_nozzle_assignments and the sale timestamp
// additively — they are not pre-aggregated anywhere else.
//
// COST and MARGIN are SENSITIVE (supplier-cost / margin gating, blueprint §14):
// the handler only surfaces them to actors holding margin.view. The aggregates
// below always compute margin so the report is honest; the handler decides
// whether to attach it.

// SalesTotals is a station's recognized-sales rollup over a business-date
// window, every money/litre figure an exact decimal string. AvgSellingPrice is
// gross ÷ litres (a display ratio, computed in SQL numeric). TxnCount is the
// number of recognized sales NOT currently reversed by an approved void.
type SalesTotals struct {
	LitresSold      string
	GrossRevenue    string
	NetRevenue      string
	TaxTotal        string
	Cogs            string
	Margin          string
	AvgSellingPrice string // gross / litres
	TxnCount        int
}

// salesFactsBase is the shared sales-facts sub-select used by the headline
// aggregations: recognized sales joined to their operating day (for the
// business-date window) and left-joined to approved voids so all sums are net of
// voids. Callers append their own GROUP BY. Bind args: $1 tenant, $2 station,
// $3 from, $4 to.
const salesFactsBase = `
    FROM sales s
    JOIN operating_days od
        ON od.id = s.operating_day_id AND od.tenant_id = s.tenant_id
    LEFT JOIN sale_voids v
        ON v.tenant_id = s.tenant_id AND v.sale_id = s.id AND v.status = 'approved'
    WHERE s.tenant_id = $1 AND s.station_id = $2
      AND od.business_date BETWEEN $3 AND $4
`

// netSum builds the net-of-voids sum expression for a sale column and its
// matching reversal column, e.g. netSum("litres", "reversal_litres").
func netSum(col, reversal string) string {
	return "(COALESCE(SUM(s." + col + "), 0) + COALESCE(SUM(v." + reversal + "), 0))"
}

// SalesSummaryTotals returns a station's headline sales figures over [from, to]
// (inclusive business dates). Average selling price is gross ÷ litres, computed
// in SQL numeric (NULLIF guards a zero-litre window). TxnCount counts sales not
// reversed by an approved void.
func (r *Repo) SalesSummaryTotals(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) (SalesTotals, error) {
	var t SalesTotals
	err := q.QueryRow(ctx, `
		SELECT
		    `+netSum("litres", "reversal_litres")+`::text,
		    `+netSum("gross_amount", "reversal_gross")+`::text,
		    `+netSum("net_amount", "reversal_net")+`::text,
		    `+netSum("tax_amount", "reversal_tax")+`::text,
		    `+netSum("cogs_amount", "reversal_cogs")+`::text,
		    `+netSum("margin_amount", "reversal_margin")+`::text,
		    COALESCE(
		        `+netSum("gross_amount", "reversal_gross")+`
		        / NULLIF(`+netSum("litres", "reversal_litres")+`, 0), 0)::text,
		    COUNT(*) FILTER (WHERE v.id IS NULL)
		`+salesFactsBase, tenantID, stationID, from, to).Scan(
		&t.LitresSold, &t.GrossRevenue, &t.NetRevenue, &t.TaxTotal,
		&t.Cogs, &t.Margin, &t.AvgSellingPrice, &t.TxnCount,
	)
	return t, err
}

// SalesGrossForWindow returns the net-of-voids GROSS revenue for [from, to] as a
// decimal string — used to build the previous-period comparison for the growth
// KPI without re-running the whole summary.
func (r *Repo) SalesGrossForWindow(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) (string, error) {
	var gross string
	err := q.QueryRow(ctx, `
		SELECT `+netSum("gross_amount", "reversal_gross")+`::text
		`+salesFactsBase, tenantID, stationID, from, to).Scan(&gross)
	return gross, err
}

// SalesDayPoint is one business-day's net-of-voids gross/litres/margin, used for
// the revenue-trend line and the period-over-period / variance insights.
type SalesDayPoint struct {
	BusinessDate string
	Gross        string
	Litres       string
	Margin       string
}

// SalesByDay returns the per-business-day gross/litres/margin trend over
// [from, to], chronological (oldest first) so it feeds both the trend chart and
// the reporting.PeriodPoint series directly.
func (r *Repo) SalesByDay(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) ([]SalesDayPoint, error) {
	rows, err := q.Query(ctx, `
		SELECT od.business_date::text,
		    `+netSum("gross_amount", "reversal_gross")+`::text,
		    `+netSum("litres", "reversal_litres")+`::text,
		    `+netSum("margin_amount", "reversal_margin")+`::text
		`+salesFactsBase+`
		GROUP BY od.business_date
		ORDER BY od.business_date
	`, tenantID, stationID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SalesDayPoint{}
	for rows.Next() {
		var p SalesDayPoint
		if err := rows.Scan(&p.BusinessDate, &p.Gross, &p.Litres, &p.Margin); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SalesDimensionRow is one row of a sales breakdown by an arbitrary dimension
// (station, product, shift, attendant, nozzle, region). Every money/litre figure
// is an exact decimal string; Margin is sensitive (margin.view-gated).
type SalesDimensionRow struct {
	Key      string // stable identity (uuid or label) for drilldown
	Label    string // human label
	Litres   string
	Gross    string
	Net      string
	Margin   string
	TxnCount int
}

// scanDimensionRows is the shared scan loop for a dimension query that selects
// (key, label, litres, gross, net, margin, txn_count).
func scanDimensionRows(rows pgx.Rows) ([]SalesDimensionRow, error) {
	defer rows.Close()
	out := []SalesDimensionRow{}
	for rows.Next() {
		var d SalesDimensionRow
		if err := rows.Scan(&d.Key, &d.Label, &d.Litres, &d.Gross, &d.Net, &d.Margin, &d.TxnCount); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SalesByProduct breaks a station's sales down by product over [from, to],
// ordered by net revenue (largest first). The product color rides the Key so the
// stacked-bar / mix visuals can theme by product without a second lookup —
// callers split on '|' (uuid|#color).
func (r *Repo) SalesByProduct(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) ([]SalesDimensionRow, error) {
	rows, err := q.Query(ctx, `
		SELECT (p.id::text || '|' || p.color),
		    p.name,
		    `+netSum("litres", "reversal_litres")+`::text,
		    `+netSum("gross_amount", "reversal_gross")+`::text,
		    `+netSum("net_amount", "reversal_net")+`::text,
		    `+netSum("margin_amount", "reversal_margin")+`::text,
		    COUNT(*) FILTER (WHERE v.id IS NULL)
		FROM sales s
		JOIN operating_days od
		    ON od.id = s.operating_day_id AND od.tenant_id = s.tenant_id
		JOIN products p ON p.id = s.product_id AND p.tenant_id = s.tenant_id
		LEFT JOIN sale_voids v
		    ON v.tenant_id = s.tenant_id AND v.sale_id = s.id AND v.status = 'approved'
		WHERE s.tenant_id = $1 AND s.station_id = $2
		  AND od.business_date BETWEEN $3 AND $4
		GROUP BY p.id, p.name, p.color
		ORDER BY `+netSum("net_amount", "reversal_net")+` DESC, p.name
	`, tenantID, stationID, from, to)
	if err != nil {
		return nil, err
	}
	return scanDimensionRows(rows)
}

// SalesByShift breaks a station's sales down by shift over [from, to], ordered by
// net revenue. The label is the shift name + its business date so two shifts of
// the same name on different days read distinctly.
func (r *Repo) SalesByShift(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) ([]SalesDimensionRow, error) {
	rows, err := q.Query(ctx, `
		SELECT sh.id::text,
		    (sh.name || ' · ' || od.business_date::text),
		    `+netSum("litres", "reversal_litres")+`::text,
		    `+netSum("gross_amount", "reversal_gross")+`::text,
		    `+netSum("net_amount", "reversal_net")+`::text,
		    `+netSum("margin_amount", "reversal_margin")+`::text,
		    COUNT(*) FILTER (WHERE v.id IS NULL)
		FROM sales s
		JOIN operating_days od
		    ON od.id = s.operating_day_id AND od.tenant_id = s.tenant_id
		JOIN shifts sh ON sh.id = s.shift_id AND sh.tenant_id = s.tenant_id
		LEFT JOIN sale_voids v
		    ON v.tenant_id = s.tenant_id AND v.sale_id = s.id AND v.status = 'approved'
		WHERE s.tenant_id = $1 AND s.station_id = $2
		  AND od.business_date BETWEEN $3 AND $4
		GROUP BY sh.id, sh.name, od.business_date
		ORDER BY `+netSum("net_amount", "reversal_net")+` DESC, od.business_date DESC
	`, tenantID, stationID, from, to)
	if err != nil {
		return nil, err
	}
	return scanDimensionRows(rows)
}

// SalesByAttendant breaks a station's sales down by attendant over [from, to].
// The attendant is resolved through shift_nozzle_assignments keyed to the SAME
// (shift, nozzle) grain as the sale (migration 0033/0015), so each sale is
// attributed to whoever was on that nozzle that shift. A sale with no assignment
// rolls into an "Unassigned" bucket so litres always tie out to the totals.
func (r *Repo) SalesByAttendant(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) ([]SalesDimensionRow, error) {
	rows, err := q.Query(ctx, `
		SELECT COALESCE(u.id::text, 'unassigned'),
		    COALESCE(u.full_name, 'Unassigned'),
		    `+netSum("litres", "reversal_litres")+`::text,
		    `+netSum("gross_amount", "reversal_gross")+`::text,
		    `+netSum("net_amount", "reversal_net")+`::text,
		    `+netSum("margin_amount", "reversal_margin")+`::text,
		    COUNT(*) FILTER (WHERE v.id IS NULL)
		FROM sales s
		JOIN operating_days od
		    ON od.id = s.operating_day_id AND od.tenant_id = s.tenant_id
		LEFT JOIN shift_nozzle_assignments sna
		    ON sna.tenant_id = s.tenant_id AND sna.shift_id = s.shift_id AND sna.nozzle_id = s.nozzle_id
		LEFT JOIN users u ON u.id = sna.attendant_id AND u.tenant_id = s.tenant_id
		LEFT JOIN sale_voids v
		    ON v.tenant_id = s.tenant_id AND v.sale_id = s.id AND v.status = 'approved'
		WHERE s.tenant_id = $1 AND s.station_id = $2
		  AND od.business_date BETWEEN $3 AND $4
		GROUP BY u.id, u.full_name
		ORDER BY `+netSum("net_amount", "reversal_net")+` DESC, u.full_name
	`, tenantID, stationID, from, to)
	if err != nil {
		return nil, err
	}
	return scanDimensionRows(rows)
}

// SalesByNozzle breaks a station's sales down by nozzle over [from, to], labelled
// "Pump N · Nozzle M" so the line is readable without a lookup.
func (r *Repo) SalesByNozzle(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) ([]SalesDimensionRow, error) {
	rows, err := q.Query(ctx, `
		SELECT n.id::text,
		    ('Pump ' || pmp.number::text || ' · Nozzle ' || n.number::text),
		    `+netSum("litres", "reversal_litres")+`::text,
		    `+netSum("gross_amount", "reversal_gross")+`::text,
		    `+netSum("net_amount", "reversal_net")+`::text,
		    `+netSum("margin_amount", "reversal_margin")+`::text,
		    COUNT(*) FILTER (WHERE v.id IS NULL)
		FROM sales s
		JOIN operating_days od
		    ON od.id = s.operating_day_id AND od.tenant_id = s.tenant_id
		JOIN nozzles n ON n.id = s.nozzle_id AND n.tenant_id = s.tenant_id
		JOIN pumps pmp ON pmp.id = n.pump_id AND pmp.tenant_id = s.tenant_id
		LEFT JOIN sale_voids v
		    ON v.tenant_id = s.tenant_id AND v.sale_id = s.id AND v.status = 'approved'
		WHERE s.tenant_id = $1 AND s.station_id = $2
		  AND od.business_date BETWEEN $3 AND $4
		GROUP BY n.id, n.number, pmp.number
		ORDER BY `+netSum("net_amount", "reversal_net")+` DESC, pmp.number, n.number
	`, tenantID, stationID, from, to)
	if err != nil {
		return nil, err
	}
	return scanDimensionRows(rows)
}

// SalesTenderBreakdown is a station's recorded tenders by type over [from, to]
// — the payment-method split for the donut. Read from payments (the same source
// the revenue_days rollup uses), scoped to the window via the shift's operating
// day, every figure a decimal string. Tenders are NOT recognized sales, so they
// are summed separately from the sale facts (a tender funds a sale but the two
// can drift while a day is open) — the donut shows how customers paid, the KPIs
// show what was sold.
type SalesTenderBreakdown struct {
	Cash        string
	MobileMoney string
	Card        string
	Credit      string
	Voucher     string
	Total       string
}

// SalesTenders sums a station's recorded tenders by type over the window.
func (r *Repo) SalesTenders(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) (SalesTenderBreakdown, error) {
	var t SalesTenderBreakdown
	err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount) FILTER (WHERE tender_type = 'cash'), 0)::text,
		       COALESCE(SUM(amount) FILTER (WHERE tender_type = 'mobile_money'), 0)::text,
		       COALESCE(SUM(amount) FILTER (WHERE tender_type = 'card'), 0)::text,
		       COALESCE(SUM(amount) FILTER (WHERE tender_type = 'credit'), 0)::text,
		       COALESCE(SUM(amount) FILTER (WHERE tender_type = 'voucher'), 0)::text,
		       COALESCE(SUM(amount), 0)::text
		FROM payments pay
		JOIN shifts sh ON sh.id = pay.shift_id AND sh.tenant_id = pay.tenant_id
		JOIN operating_days od ON od.id = sh.operating_day_id AND od.tenant_id = sh.tenant_id
		WHERE pay.tenant_id = $1 AND pay.station_id = $2 AND pay.status = 'recorded'
		  AND od.business_date BETWEEN $3 AND $4
	`, tenantID, stationID, from, to).Scan(
		&t.Cash, &t.MobileMoney, &t.Card, &t.Credit, &t.Voucher, &t.Total,
	)
	return t, err
}

// SalesHourCell is one hour-of-day bucket's net-of-voids gross + litres, derived
// from the sale's recorded_at timestamp (the sale is recognized at shift
// approval, so this is the recognition hour — the honest available signal absent
// a per-transaction timestamp). Hour is 0..23 in the station's stored timezone
// offset is NOT applied here; recorded_at is UTC, matching every other report.
type SalesHourCell struct {
	Hour   int
	Gross  string
	Litres string
	Txn    int
}

// SalesByHour buckets a station's sales by hour-of-day (0..23) over [from, to]
// for the peak-hours heatmap. Only hours with sales are returned; the handler
// fills the 24-hour grid. recorded_at is UTC (consistent with the rest of the
// reporting surface).
func (r *Repo) SalesByHour(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) ([]SalesHourCell, error) {
	rows, err := q.Query(ctx, `
		SELECT EXTRACT(HOUR FROM s.recorded_at)::int AS hr,
		    `+netSum("gross_amount", "reversal_gross")+`::text,
		    `+netSum("litres", "reversal_litres")+`::text,
		    COUNT(*) FILTER (WHERE v.id IS NULL)
		FROM sales s
		JOIN operating_days od
		    ON od.id = s.operating_day_id AND od.tenant_id = s.tenant_id
		LEFT JOIN sale_voids v
		    ON v.tenant_id = s.tenant_id AND v.sale_id = s.id AND v.status = 'approved'
		WHERE s.tenant_id = $1 AND s.station_id = $2
		  AND od.business_date BETWEEN $3 AND $4
		GROUP BY hr
		ORDER BY hr
	`, tenantID, stationID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SalesHourCell{}
	for rows.Next() {
		var c SalesHourCell
		if err := rows.Scan(&c.Hour, &c.Gross, &c.Litres, &c.Txn); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SalesDataQuality counts the window's shifts that are not yet approved and the
// revenue days that are not yet locked — the two signals that the sales figures
// may still change. Both are honest data-quality inputs (blueprint §5.2 / §18):
// an unapproved shift's sales are provisional, and an unlocked day's rollup can
// be recomputed.
type SalesDataQuality struct {
	Shifts           int
	UnapprovedShifts int
	RevenueDays      int
	UnlockedDays     int
}

// SalesWindowQuality returns the shift/day completeness signals for [from, to].
func (r *Repo) SalesWindowQuality(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) (SalesDataQuality, error) {
	var dq SalesDataQuality
	err := q.QueryRow(ctx, `
		SELECT
		    (SELECT COUNT(*) FROM shifts sh
		        JOIN operating_days od ON od.id = sh.operating_day_id AND od.tenant_id = sh.tenant_id
		        WHERE sh.tenant_id = $1 AND sh.station_id = $2
		          AND od.business_date BETWEEN $3 AND $4),
		    (SELECT COUNT(*) FROM shifts sh
		        JOIN operating_days od ON od.id = sh.operating_day_id AND od.tenant_id = sh.tenant_id
		        WHERE sh.tenant_id = $1 AND sh.station_id = $2 AND sh.status <> 'approved'
		          AND od.business_date BETWEEN $3 AND $4),
		    (SELECT COUNT(*) FROM revenue_days rd
		        WHERE rd.tenant_id = $1 AND rd.station_id = $2
		          AND rd.business_date BETWEEN $3 AND $4),
		    (SELECT COUNT(*) FROM revenue_days rd
		        WHERE rd.tenant_id = $1 AND rd.station_id = $2 AND rd.status <> 'locked'
		          AND rd.business_date BETWEEN $3 AND $4)
	`, tenantID, stationID, from, to).Scan(
		&dq.Shifts, &dq.UnapprovedShifts, &dq.RevenueDays, &dq.UnlockedDays,
	)
	return dq, err
}
