package reportcatalog

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Tenant-wide hub metric aggregates.
//
// These are the figures the Reports Home shows on a category card. Every
// monetary figure is returned as an EXACT decimal STRING straight from
// Postgres (numeric::text) — it is never parsed to float on this path. Counts
// are plain integers. Each query is tenant-scoped by an explicit WHERE
// tenant_id clause (RLS is the belt-and-braces companion), so a tenant can only
// ever aggregate its own rows.
//
// Only metrics that have a genuine tenant-wide source are computed here. A
// category whose only data is station-scoped (no tenant rollup query exists)
// gets NO metric — the handler marks it partial with an honest reason rather
// than fabricate a number.

// SalesRollup is the tenant-wide revenue rollup over a recent window, read from
// the pre-aggregated revenue_days table. GrossRevenue and MarginTotal are exact
// decimal strings; MarginTotal is SENSITIVE (margin.view-gated) and the handler
// omits it for actors lacking that permission.
type SalesRollup struct {
	GrossRevenue string // numeric::text, e.g. "1234567.89"
	MarginTotal  string // numeric::text — sensitive
	DayCount     int
}

// SalesRollup sums gross revenue and margin across all the tenant's stations
// over the [from, to) window from revenue_days. COALESCE keeps the strings
// well-formed ("0.00") when there are no days yet.
func (r *Repo) SalesRollup(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (SalesRollup, error) {
	var out SalesRollup
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(gross_revenue), 0)::text,
		       COALESCE(SUM(margin_total), 0)::text,
		       count(*)
		FROM revenue_days
		WHERE tenant_id = $1 AND business_date >= $2 AND business_date < $3
	`, tenantID, from, to).Scan(&out.GrossRevenue, &out.MarginTotal, &out.DayCount)
	if err != nil {
		return SalesRollup{}, err
	}
	return out, nil
}

// DeliveryCount returns the number of fuel deliveries recorded tenant-wide over
// the [from, to) window. Deliveries carry a tenant_id, so this is a clean
// tenant rollup (unlike per-station delivery detail).
func (r *Repo) DeliveryCount(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM deliveries
		WHERE tenant_id = $1 AND received_at >= $2 AND received_at < $3
	`, tenantID, from, to).Scan(&n)
	return n, err
}

// AuditEventCount returns the number of audit-log events recorded for the
// tenant over the [from, to) window — the Audit category's key metric.
func (r *Repo) AuditEventCount(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE tenant_id = $1 AND occurred_at >= $2 AND occurred_at < $3
	`, tenantID, from, to).Scan(&n)
	return n, err
}

// ExportCount returns the number of report exports the tenant has recorded —
// the Export History category's key metric.
func (r *Repo) ExportCount(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM export_jobs WHERE tenant_id = $1
	`, tenantID).Scan(&n)
	return n, err
}

// ReceivablesExposure is the tenant's outstanding credit-customer exposure: the
// sum of POSITIVE customer balances (customers in arrears), mirroring the
// overview's "receivables" headline (customers in credit are excluded). The sum
// is computed in SQL as an exact numeric::text — no float accumulation on money.
// HasData is false when no customer carries a positive balance, so the handler
// can emit an honest "no outstanding balances" reason rather than "0".
func (r *Repo) ReceivablesExposure(ctx context.Context, tenantID uuid.UUID) (value string, hasData bool, err error) {
	var positives int
	err = r.pool.QueryRow(ctx, `
		WITH balances AS (
			SELECT COALESCE(SUM(e.amount), 0) AS bal
			FROM customers c
			LEFT JOIN ar_entries e ON e.customer_id = c.id AND e.tenant_id = c.tenant_id
			WHERE c.tenant_id = $1 AND c.status <> 'deleted'
			GROUP BY c.id
		)
		SELECT COALESCE(SUM(bal) FILTER (WHERE bal > 0), 0)::text,
		       count(*)          FILTER (WHERE bal > 0)
		FROM balances
	`, tenantID).Scan(&value, &positives)
	if err != nil {
		return "", false, err
	}
	return value, positives > 0, nil
}
