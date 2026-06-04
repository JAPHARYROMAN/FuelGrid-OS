package revenue

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Credit & cashflow aggregates (Feature 10.5).
//
// Every money figure is summed in SQL as ::numeric and returned as an exact
// decimal STRING — no figure is recomputed in Go float. The report ties out to
// the same facts the other finance surfaces use:
//
//   - sales by tender type come from the recorded payments ledger (the same
//     source as revenue.DayTenders), filtered to the station's operating days in
//     the window;
//   - collections are posted customer payments against the station's invoices;
//   - outstanding + overdue receivables come from the station's issued/
//     partially-paid customer invoices (outstanding_amount, due_date);
//   - supplier payments are the tenant's posted supplier payments in the window
//     (payables carry no station, so this is a tenant-wide figure);
//   - cash variance is the sum of the station's cash-reconciliation variances in
//     the window.
//
// The projected cash position is a transparent, deterministic identity computed
// in SQL: cash + mobile-money sales tendered, plus collections, minus supplier
// payments — i.e. cash actually moved through the station's tills and the
// tenant's bank in the window. It is NOT a forecast model; it is the realized
// net cash movement, surfaced so an operator sees the period's cash trajectory.

// CashflowTotals is a station's credit & cashflow picture over a window. Every
// field is an exact decimal string.
type CashflowTotals struct {
	CashSales          string // cash tenders recorded against the station's shifts
	MobileMoneySales   string // mobile-money tenders
	CardSales          string // card tenders
	CreditSales        string // credit tenders (billed to AR)
	VoucherSales       string // voucher tenders
	TotalTendered      string // sum of all tenders
	Collections        string // posted customer payments against the station's invoices
	OutstandingAR      string // outstanding balance on the station's issued/partially-paid invoices
	OverdueAR          string // outstanding balance whose due_date has passed
	SupplierPayments   string // posted supplier payments in the window (tenant-wide)
	CashVariance       string // sum of cash-reconciliation variances in the window (signed)
	ProjectedCashPos   string // realized net cash movement: cash+mobile sales + collections − supplier payments
	TenderCount        int    // number of recorded tender rows (data-quality signal)
	ReconciliationDays int    // number of cash reconciliations contributing to the variance
}

// Cashflow returns a station's credit & cashflow totals over [from, to]
// (inclusive business dates for sales/collections; payment_date for supplier
// payments; reconciliation operating-day date for the variance). All sums run in
// SQL ::numeric; supplier payments are tenant-wide (payables have no station).
func (r *Repo) Cashflow(ctx context.Context, q database.Querier, tenantID, stationID uuid.UUID, from, to time.Time) (CashflowTotals, error) {
	var t CashflowTotals
	if err := q.QueryRow(ctx, `
		WITH tender_facts AS (
		    SELECT
		        COALESCE(SUM(pm.amount) FILTER (WHERE pm.tender_type = 'cash'),         0) AS cash,
		        COALESCE(SUM(pm.amount) FILTER (WHERE pm.tender_type = 'mobile_money'), 0) AS mobile,
		        COALESCE(SUM(pm.amount) FILTER (WHERE pm.tender_type = 'card'),         0) AS card,
		        COALESCE(SUM(pm.amount) FILTER (WHERE pm.tender_type = 'credit'),       0) AS credit,
		        COALESCE(SUM(pm.amount) FILTER (WHERE pm.tender_type = 'voucher'),      0) AS voucher,
		        COALESCE(SUM(pm.amount), 0)                                               AS total,
		        COUNT(*)                                                                  AS tender_count
		    FROM payments pm
		    JOIN shifts sh
		        ON sh.id = pm.shift_id AND sh.tenant_id = pm.tenant_id
		    JOIN operating_days od
		        ON od.id = sh.operating_day_id AND od.tenant_id = sh.tenant_id
		    WHERE pm.tenant_id = $1 AND pm.station_id = $2 AND pm.status = 'recorded'
		      AND od.business_date BETWEEN $3 AND $4
		),
		collection_facts AS (
		    SELECT COALESCE(SUM(cp.amount), 0) AS collected
		    FROM customer_payments cp
		    WHERE cp.tenant_id = $1 AND cp.status = 'posted'
		      AND cp.payment_date BETWEEN $3 AND $4
		      AND cp.id IN (
		          SELECT cpa.customer_payment_id
		          FROM customer_payment_allocations cpa
		          JOIN customer_invoices ci
		              ON ci.id = cpa.customer_invoice_id AND ci.tenant_id = cpa.tenant_id
		          WHERE cpa.tenant_id = $1 AND ci.station_id = $2
		      )
		),
		ar_facts AS (
		    SELECT
		        COALESCE(SUM(ci.outstanding_amount), 0)                                          AS outstanding,
		        COALESCE(SUM(ci.outstanding_amount) FILTER (WHERE ci.due_date < CURRENT_DATE), 0) AS overdue
		    FROM customer_invoices ci
		    WHERE ci.tenant_id = $1 AND ci.station_id = $2
		      AND ci.status IN ('issued', 'partially_paid')
		),
		supplier_facts AS (
		    SELECT COALESCE(SUM(sp.amount), 0) AS paid
		    FROM supplier_payments sp
		    WHERE sp.tenant_id = $1 AND sp.status = 'posted'
		      AND sp.payment_date BETWEEN $3 AND $4
		),
		variance_facts AS (
		    SELECT
		        COALESCE(SUM(cr.variance), 0) AS variance,
		        COUNT(*)                      AS recon_days
		    FROM cash_reconciliations cr
		    JOIN operating_days od
		        ON od.id = cr.operating_day_id AND od.tenant_id = cr.tenant_id
		    WHERE cr.tenant_id = $1 AND cr.station_id = $2
		      AND cr.status IN ('approved', 'posted')
		      AND od.business_date BETWEEN $3 AND $4
		)
		SELECT
		    tf.cash::text,
		    tf.mobile::text,
		    tf.card::text,
		    tf.credit::text,
		    tf.voucher::text,
		    tf.total::text,
		    cf.collected::text,
		    af.outstanding::text,
		    af.overdue::text,
		    sf.paid::text,
		    vf.variance::text,
		    (tf.cash + tf.mobile + cf.collected - sf.paid)::text,
		    tf.tender_count,
		    vf.recon_days
		FROM tender_facts tf, collection_facts cf, ar_facts af, supplier_facts sf, variance_facts vf
	`, tenantID, stationID, from, to).Scan(
		&t.CashSales, &t.MobileMoneySales, &t.CardSales, &t.CreditSales, &t.VoucherSales,
		&t.TotalTendered, &t.Collections, &t.OutstandingAR, &t.OverdueAR, &t.SupplierPayments,
		&t.CashVariance, &t.ProjectedCashPos, &t.TenderCount, &t.ReconciliationDays,
	); err != nil {
		return CashflowTotals{}, err
	}
	return t, nil
}
