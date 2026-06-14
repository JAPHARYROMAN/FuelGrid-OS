package receivables

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AR aging time buckets + per-customer credit standing (Reports Center §5.9).
//
// The Aging()/InvoiceAging() views above return FLAT outstanding balances; the
// blueprint 5.9 Customer Credit report needs real TIME BUCKETS of the issued-
// invoice ledger, aged by due_date vs a report date. All bucketing is done in
// SQL date math against customer_invoices (migration 0045: due_date,
// outstanding_amount) and the per-customer credit position is read from the
// same enforceable-terms tables the live credit check uses (customers.credit_limit,
// ar_entries, fuel_authorizations, customer_credit_profiles — migrations 0034 /
// 0051). Every money figure is summed in SQL ::numeric and carried as an exact
// decimal STRING; nothing is recomputed in Go float.

// AgingBucketRow is one credit customer's outstanding AR aged into the standard
// buckets, alongside their enforceable credit standing. Every money field is an
// exact decimal STRING (numeric ::text). The bucket figures sum to Outstanding.
type AgingBucketRow struct {
	CustomerID uuid.UUID
	Code       string
	Name       string

	// Aging buckets of outstanding issued-invoice balances by due date vs the
	// report date. An invoice with no due_date counts as Current (it is not yet
	// past due) and is also surfaced via the NoDueDate count for data-quality.
	Current     string // not yet due (due_date >= as-of, or no due date)
	Days1To30   string // 1-30 days past due
	Days31To60  string // 31-60 days past due
	Days61To90  string // 61-90 days past due
	Days90Plus  string // 90+ days past due
	Outstanding string // total outstanding (sum of the buckets)
	Overdue     string // outstanding past due (Outstanding − Current)

	// Credit standing (sensitive — CREDIT EXPOSURE). Exposure = AR balance +
	// live authorization holds; Available = limit − exposure; Utilization is the
	// exposure-over-limit percent (display ratio, computed in SQL).
	CreditLimit  string
	Exposure     string
	Available    string
	Utilization  string // exposure / limit * 100, "0" when limit is 0
	WarningPct   string // soft warning threshold (% of limit)
	OverLimit    bool
	OnHold       bool
	HoldReason   *string
	RiskCategory string
	Status       string // customer account status

	// Invoice hygiene for the report's data-quality band.
	InvoicesWithoutDueDate int
}

// AgingBuckets returns every credit customer with a positive outstanding
// issued-invoice balance, aged into Current / 1-30 / 31-60 / 61-90 / 90+ buckets
// by due_date vs asOf, plus their enforceable credit standing. Largest
// outstanding first. The buckets and the credit position are computed entirely
// in SQL (numeric), so the report never recomputes money in Go.
func (r *Repo) AgingBuckets(ctx context.Context, tenantID uuid.UUID, asOf time.Time) ([]AgingBucketRow, error) {
	rows, err := r.pool.Query(ctx, `
		WITH inv AS (
		    -- Issued / partially-paid invoices carry the live outstanding balance.
		    -- Age each by the whole-day gap between the report date and its due
		    -- date; a NULL due date is treated as not-yet-due (Current).
		    SELECT
		        ci.customer_id,
		        ci.outstanding_amount AS amt,
		        CASE WHEN ci.due_date IS NULL THEN 0
		             ELSE GREATEST(0, ($2::date - ci.due_date)) END AS days_overdue,
		        (ci.due_date IS NULL)::int AS no_due
		    FROM customer_invoices ci
		    WHERE ci.tenant_id = $1
		      AND ci.status IN ('issued', 'partially_paid')
		      AND ci.outstanding_amount > 0
		),
		buckets AS (
		    SELECT
		        customer_id,
		        SUM(amt) FILTER (WHERE days_overdue = 0)                          AS b_current,
		        SUM(amt) FILTER (WHERE days_overdue BETWEEN 1 AND 30)            AS b_1_30,
		        SUM(amt) FILTER (WHERE days_overdue BETWEEN 31 AND 60)           AS b_31_60,
		        SUM(amt) FILTER (WHERE days_overdue BETWEEN 61 AND 90)           AS b_61_90,
		        SUM(amt) FILTER (WHERE days_overdue > 90)                        AS b_90_plus,
		        SUM(amt)                                                          AS outstanding,
		        SUM(amt) FILTER (WHERE days_overdue > 0)                         AS overdue,
		        SUM(no_due)                                                       AS no_due_count
		    FROM inv
		    GROUP BY customer_id
		    HAVING SUM(amt) > 0
		),
		exposure AS (
		    -- Live credit exposure = AR ledger balance + outstanding approved
		    -- (not-expired) authorization holds, mirroring CreditPosition.
		    SELECT b.customer_id,
		           COALESCE((SELECT SUM(amount) FROM ar_entries e
		                     WHERE e.tenant_id = $1 AND e.customer_id = b.customer_id), 0)
		         + COALESCE((SELECT SUM(approved_amount) FROM fuel_authorizations fa
		                     WHERE fa.tenant_id = $1 AND fa.customer_id = b.customer_id
		                       AND fa.status = 'approved'
		                       AND (fa.expiry_at IS NULL OR fa.expiry_at > now())), 0) AS exposure
		    FROM buckets b
		)
		SELECT
		    c.id, c.code, c.name,
		    COALESCE(b.b_current, 0)::text,
		    COALESCE(b.b_1_30, 0)::text,
		    COALESCE(b.b_31_60, 0)::text,
		    COALESCE(b.b_61_90, 0)::text,
		    COALESCE(b.b_90_plus, 0)::text,
		    b.outstanding::text,
		    COALESCE(b.overdue, 0)::text,
		    c.credit_limit::text,
		    x.exposure::text,
		    (c.credit_limit - x.exposure)::text,
		    CASE WHEN c.credit_limit > 0
		         THEN ROUND(x.exposure / c.credit_limit * 100, 2)::text
		         ELSE '0' END,
		    COALESCE(p.warning_threshold_pct, 80)::text,
		    -- A zero credit limit is "cash-only", not "over limit"; the credit_limit > 0
		    -- guard keeps the per-row flag identical to the tenant-wide over-limit count
		    -- (AgingSummary) so the risk badge can't contradict the KPI.
		    (c.credit_limit > 0 AND x.exposure > c.credit_limit),
		    (COALESCE(p.hold, false) OR c.status IN ('on_hold', 'suspended')),
		    p.hold_reason,
		    COALESCE(p.risk_category, 'standard'),
		    c.status,
		    COALESCE(b.no_due_count, 0)
		FROM buckets b
		JOIN customers c ON c.id = b.customer_id AND c.tenant_id = $1
		JOIN exposure x ON x.customer_id = b.customer_id
		LEFT JOIN customer_credit_profiles p ON p.tenant_id = c.tenant_id AND p.customer_id = c.id
		WHERE c.status <> 'deleted'
		ORDER BY b.outstanding DESC, c.name
	`, tenantID, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgingBucketRow{}
	for rows.Next() {
		var b AgingBucketRow
		if err := rows.Scan(
			&b.CustomerID, &b.Code, &b.Name,
			&b.Current, &b.Days1To30, &b.Days31To60, &b.Days61To90, &b.Days90Plus,
			&b.Outstanding, &b.Overdue,
			&b.CreditLimit, &b.Exposure, &b.Available, &b.Utilization, &b.WarningPct,
			&b.OverLimit, &b.OnHold, &b.HoldReason, &b.RiskCategory, &b.Status,
			&b.InvoicesWithoutDueDate,
		); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AgingTotals is the tenant-wide aging KPI rollup: the bucket sums across every
// credit customer plus the counts that drive the KPI hero. Money is summed in
// SQL ::numeric and carried as exact decimal strings.
type AgingTotals struct {
	Current     string
	Days1To30   string
	Days31To60  string
	Days61To90  string
	Days90Plus  string
	Outstanding string
	Overdue     string

	CustomersWithBalance int
	CustomersOverLimit   int
	CustomersOnHold      int

	// Data-quality signals.
	InvoicesWithoutDueDate int
	UnallocatedPayments    int // posted payments with allocated < amount
	UnallocatedAmount      string
}

// AgingSummary rolls the per-customer buckets up into the tenant-wide KPI hero
// figures and the data-quality counts (customers over limit / on hold, invoices
// without a due date, and posted-but-unallocated customer payments). The bucket
// sums are computed in SQL alongside the per-customer query's CTE so the hero and
// the table can never disagree.
func (r *Repo) AgingSummary(ctx context.Context, tenantID uuid.UUID, asOf time.Time) (AgingTotals, error) {
	var t AgingTotals
	err := r.pool.QueryRow(ctx, `
		WITH inv AS (
		    SELECT
		        ci.customer_id,
		        ci.outstanding_amount AS amt,
		        CASE WHEN ci.due_date IS NULL THEN 0
		             ELSE GREATEST(0, ($2::date - ci.due_date)) END AS days_overdue,
		        (ci.due_date IS NULL)::int AS no_due
		    FROM customer_invoices ci
		    WHERE ci.tenant_id = $1
		      AND ci.status IN ('issued', 'partially_paid')
		      AND ci.outstanding_amount > 0
		      -- Scope to the SAME non-deleted customers the per-customer bucket table
		      -- shows, so the hero KPIs and Σ(table rows) can never disagree.
		      AND EXISTS (SELECT 1 FROM customers c
		                  WHERE c.id = ci.customer_id AND c.tenant_id = $1
		                    AND c.status <> 'deleted')
		),
		per_customer AS (
		    SELECT customer_id, SUM(amt) AS outstanding
		    FROM inv GROUP BY customer_id HAVING SUM(amt) > 0
		)
		SELECT
		    COALESCE(SUM(amt) FILTER (WHERE days_overdue = 0), 0)::text,
		    COALESCE(SUM(amt) FILTER (WHERE days_overdue BETWEEN 1 AND 30), 0)::text,
		    COALESCE(SUM(amt) FILTER (WHERE days_overdue BETWEEN 31 AND 60), 0)::text,
		    COALESCE(SUM(amt) FILTER (WHERE days_overdue BETWEEN 61 AND 90), 0)::text,
		    COALESCE(SUM(amt) FILTER (WHERE days_overdue > 90), 0)::text,
		    COALESCE(SUM(amt), 0)::text,
		    COALESCE(SUM(amt) FILTER (WHERE days_overdue > 0), 0)::text,
		    (SELECT COUNT(*) FROM per_customer),
		    COALESCE(SUM(no_due), 0)
		FROM inv
	`, tenantID, asOf).Scan(
		&t.Current, &t.Days1To30, &t.Days31To60, &t.Days61To90, &t.Days90Plus,
		&t.Outstanding, &t.Overdue, &t.CustomersWithBalance, &t.InvoicesWithoutDueDate,
	)
	if err != nil {
		return AgingTotals{}, err
	}

	// Over-limit / on-hold counts across all non-deleted credit customers (a
	// customer can be over limit or on hold with no outstanding invoices, so this
	// is intentionally not restricted to customers-with-balance).
	err = r.pool.QueryRow(ctx, `
		WITH expo AS (
		    SELECT c.id, c.credit_limit,
		           COALESCE((SELECT SUM(amount) FROM ar_entries e
		                     WHERE e.tenant_id = $1 AND e.customer_id = c.id), 0)
		         + COALESCE((SELECT SUM(approved_amount) FROM fuel_authorizations fa
		                     WHERE fa.tenant_id = $1 AND fa.customer_id = c.id
		                       AND fa.status = 'approved'
		                       AND (fa.expiry_at IS NULL OR fa.expiry_at > now())), 0) AS exposure,
		           (COALESCE(p.hold, false) OR c.status IN ('on_hold', 'suspended')) AS on_hold
		    FROM customers c
		    LEFT JOIN customer_credit_profiles p ON p.tenant_id = c.tenant_id AND p.customer_id = c.id
		    WHERE c.tenant_id = $1 AND c.status <> 'deleted'
		)
		SELECT
		    COUNT(*) FILTER (WHERE credit_limit > 0 AND exposure > credit_limit),
		    COUNT(*) FILTER (WHERE on_hold)
		FROM expo
	`, tenantID).Scan(&t.CustomersOverLimit, &t.CustomersOnHold)
	if err != nil {
		return AgingTotals{}, err
	}

	// Posted customer payments not fully allocated to invoices — money received
	// but not yet applied, a data-quality signal that the aging may overstate.
	err = r.pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(amount - allocated_amount), 0)::text
		FROM customer_payments cp
		WHERE cp.tenant_id = $1 AND cp.status = 'posted' AND cp.allocated_amount < cp.amount
		  -- Same non-deleted customer scope as the aging table / hero.
		  AND EXISTS (SELECT 1 FROM customers c
		              WHERE c.id = cp.customer_id AND c.tenant_id = $1
		                AND c.status <> 'deleted')
	`, tenantID).Scan(&t.UnallocatedPayments, &t.UnallocatedAmount)
	if err != nil {
		return AgingTotals{}, err
	}
	return t, nil
}

// CustomerInvoiceLine is one open invoice in a customer's drilldown: its
// identity, dates, amount, live outstanding balance, days overdue and bucket.
// Money is an exact decimal STRING.
type CustomerInvoiceLine struct {
	InvoiceID     uuid.UUID
	InvoiceNumber *string
	InvoiceDate   time.Time
	DueDate       *time.Time
	Amount        string
	Outstanding   string
	DaysOverdue   int
	Bucket        string // current | 1-30 | 31-60 | 61-90 | 90+
	Status        string
}

// CustomerPaymentLine is one posted payment in a customer's drilldown.
type CustomerPaymentLine struct {
	PaymentID   uuid.UUID
	PaymentDate time.Time
	Method      string
	Reference   *string
	Amount      string
	Allocated   string
	Status      string
}

// CustomerCreditDrilldown reads one customer's open invoices (aged) and recent
// payments for the report's balance -> invoices -> payments drilldown. Both
// lists carry exact decimal strings; the bucket label is derived from the same
// SQL date math as AgingBuckets.
func (r *Repo) CustomerCreditDrilldown(ctx context.Context, tenantID, customerID uuid.UUID, asOf time.Time) ([]CustomerInvoiceLine, []CustomerPaymentLine, error) {
	invRows, err := r.pool.Query(ctx, `
		SELECT
		    ci.id, ci.invoice_number, ci.invoice_date, ci.due_date,
		    ci.amount::text, ci.outstanding_amount::text,
		    CASE WHEN ci.due_date IS NULL THEN 0
		         ELSE GREATEST(0, ($3::date - ci.due_date)) END AS days_overdue,
		    ci.status
		FROM customer_invoices ci
		WHERE ci.tenant_id = $1 AND ci.customer_id = $2
		  AND ci.status IN ('issued', 'partially_paid') AND ci.outstanding_amount > 0
		ORDER BY ci.due_date NULLS LAST, ci.invoice_date
	`, tenantID, customerID, asOf)
	if err != nil {
		return nil, nil, err
	}
	defer invRows.Close()
	invoices := []CustomerInvoiceLine{}
	for invRows.Next() {
		var l CustomerInvoiceLine
		if err := invRows.Scan(
			&l.InvoiceID, &l.InvoiceNumber, &l.InvoiceDate, &l.DueDate,
			&l.Amount, &l.Outstanding, &l.DaysOverdue, &l.Status,
		); err != nil {
			return nil, nil, err
		}
		l.Bucket = bucketLabel(l.DaysOverdue)
		invoices = append(invoices, l)
	}
	if err := invRows.Err(); err != nil {
		return nil, nil, err
	}

	payRows, err := r.pool.Query(ctx, `
		SELECT id, payment_date, method, reference, amount::text, allocated_amount::text, status
		FROM customer_payments
		WHERE tenant_id = $1 AND customer_id = $2
		ORDER BY payment_date DESC, created_at DESC
		LIMIT 50
	`, tenantID, customerID)
	if err != nil {
		return nil, nil, err
	}
	defer payRows.Close()
	payments := []CustomerPaymentLine{}
	for payRows.Next() {
		var p CustomerPaymentLine
		if err := payRows.Scan(
			&p.PaymentID, &p.PaymentDate, &p.Method, &p.Reference, &p.Amount, &p.Allocated, &p.Status,
		); err != nil {
			return nil, nil, err
		}
		payments = append(payments, p)
	}
	return invoices, payments, payRows.Err()
}

// bucketLabel maps a whole-day overdue count to its aging bucket label, matching
// the SQL bucketing in AgingBuckets.
func bucketLabel(daysOverdue int) string {
	switch {
	case daysOverdue <= 0:
		return "current"
	case daysOverdue <= 30:
		return "1-30"
	case daysOverdue <= 60:
		return "31-60"
	case daysOverdue <= 90:
		return "61-90"
	default:
		return "90+"
	}
}
