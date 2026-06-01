package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/enterprise"
	"github.com/japharyroman/fuelgrid-os/internal/payables"
	"github.com/japharyroman/fuelgrid-os/internal/receivables"
	"github.com/japharyroman/fuelgrid-os/internal/revenue"
	"github.com/japharyroman/fuelgrid-os/internal/risk"
)

// Deps groups the repos the recurring jobs call into. They are the same repo
// types the HTTP handlers use; the jobs invoke their EXISTING domain methods
// cross-tenant rather than re-implementing any logic. All run on the owner pool
// (cross-tenant background work), so RLS does not apply — every method is
// already explicitly tenant-scoped by argument.
type Deps struct {
	Pool        *database.Pool
	Revenue     *revenue.Repo
	Risk        *risk.Repo
	Receivables *receivables.Repo
	Payables    *payables.Repo
	Enterprise  *enterprise.Repo
	Logger      *slog.Logger
	// Report carries the email sender + recipients for the canned scheduled-email
	// digests (daily station-close, monthly P&L). When it is left zero (no
	// recipients / no real SMTP) those jobs run as a safe no-op.
	Report ReportDeps
}

// Intervals is the per-job cadence (mirrors the SCHEDULER_* config knobs). A
// non-positive interval disables that job: New() drops it before it is ever
// scheduled, so a zero-value Intervals (the test-harness Config{}) yields no
// jobs at all.
type Intervals struct {
	RevenueCompute time.Duration
	AgingRefresh   time.Duration
	RiskDetect     time.Duration
	Projection     time.Duration
	OutboxSweep    time.Duration
	SessionCleanup time.Duration
	// ReportDigest is the tick cadence for BOTH canned email digests (daily
	// station-close, monthly P&L). It is deliberately sub-day (e.g. 1h): the job
	// bodies gate on a configured send hour and a job_runs ledger guard, so a
	// frequent tick still produces exactly one daily / one monthly send while
	// guaranteeing a send lands shortly after the send hour. <= 0 disables both.
	ReportDigest time.Duration

	// Non-interval tuning the job bodies read.
	SessionRetention   time.Duration
	OutboxRequeueAfter time.Duration
	JobRunRetention    time.Duration
}

// BuildJobs assembles the full job set from the deps and intervals. Jobs whose
// interval is <= 0 still appear here but are filtered out by scheduler.New, so
// callers get one obvious place to read the catalog.
func BuildJobs(d Deps, iv Intervals) []Job {
	// The report digests share the owner pool + logger with the rest of the job
	// set, so backfill them onto the ReportDeps the caller supplied (which only
	// needs to carry the email sender, recipients, and send hour).
	report := d.Report
	report.Pool = d.Pool
	if report.Logger == nil {
		report.Logger = d.Logger
	}

	return []Job{
		{Name: "revenue_compute", Interval: iv.RevenueCompute, Run: d.revenueComputeJob},
		{Name: "aging_refresh", Interval: iv.AgingRefresh, Run: d.agingRefreshJob},
		{Name: "risk_detect", Interval: iv.RiskDetect, Run: d.riskDetectJob},
		{Name: "enterprise_projection", Interval: iv.Projection, Run: d.projectionJob},
		{Name: "outbox_dead_letter_sweep", Interval: iv.OutboxSweep, Run: outboxSweepJob(d.Pool, iv.OutboxRequeueAfter, iv.JobRunRetention)},
		{Name: "session_token_cleanup", Interval: iv.SessionCleanup, Run: sessionCleanupJob(d.Pool, iv.SessionRetention)},
		{Name: "report_daily_close_digest", Interval: iv.ReportDigest, Run: dailyDigestJob(report)},
		{Name: "report_monthly_pnl", Interval: iv.ReportDigest, Run: monthlyPnLJob(report)},
	}
}

// activeTenantIDs lists every active tenant. The per-tenant jobs fan out over
// this so a single tick recomputes for the whole platform. Inactive/soft-deleted
// tenants are skipped.
func (d Deps) activeTenantIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := d.Pool.Query(ctx, `SELECT id FROM tenants WHERE status = 'active' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// revenueComputeJob recomputes draft revenue_days for every open operating day
// across every active tenant, calling revenue.ComputeDay (idempotent upsert;
// locked days are skipped by the method itself). This keeps each station-day's
// rolled-up revenue current without waiting for an operator to hit the
// /revenue-days endpoint.
func (d Deps) revenueComputeJob(ctx context.Context) (string, error) {
	tenants, err := d.activeTenantIDs(ctx)
	if err != nil {
		return "", fmt.Errorf("list tenants: %w", err)
	}
	var computed, tenantErrs int
	for _, tid := range tenants {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// Find this tenant's open operating days (station + day pairs) so we only
		// recompute days that are still mutable.
		rows, err := d.Pool.Query(ctx,
			`SELECT station_id, id FROM operating_days WHERE tenant_id = $1 AND status = 'open'`, tid)
		if err != nil {
			d.log().Warn("revenue_compute: list open days", "tenant_id", tid, "error", err)
			tenantErrs++
			continue
		}
		type pair struct{ station, day uuid.UUID }
		var days []pair
		for rows.Next() {
			var p pair
			if err := rows.Scan(&p.station, &p.day); err != nil {
				rows.Close()
				return "", fmt.Errorf("scan open day: %w", err)
			}
			days = append(days, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			d.log().Warn("revenue_compute: rows", "tenant_id", tid, "error", err)
			tenantErrs++
			continue
		}

		for _, p := range days {
			err := withTx(ctx, d.Pool, func(tx pgx.Tx) error {
				if _, cerr := d.Revenue.ComputeDay(ctx, tx, tid, p.station, p.day); cerr != nil {
					// A locked day is not an error here — just nothing to do.
					if errors.Is(cerr, revenue.ErrLocked) {
						return nil
					}
					return cerr
				}
				return nil
			})
			if err != nil {
				d.log().Warn("revenue_compute: compute day", "tenant_id", tid, "station_id", p.station, "day_id", p.day, "error", err)
				tenantErrs++
				continue
			}
			computed++
		}
	}
	return fmt.Sprintf("tenants=%d days_computed=%d errors=%d", len(tenants), computed, tenantErrs), nil
}

// agingRefreshJob warms AR and AP aging for every active tenant by importing any
// newly-approved supplier invoices into payables (payables.ImportApprovedInvoices,
// which persists) and recomputing the AR/AP aging snapshots (read methods that
// materialise the current position). Pulling approved invoices into payables on
// a timer is the persistent half; the aging reads validate the position is
// computable and keep the query plans warm.
func (d Deps) agingRefreshJob(ctx context.Context) (string, error) {
	tenants, err := d.activeTenantIDs(ctx)
	if err != nil {
		return "", fmt.Errorf("list tenants: %w", err)
	}
	var imported, tenantErrs int
	for _, tid := range tenants {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		err := withTx(ctx, d.Pool, func(tx pgx.Tx) error {
			created, ierr := d.Payables.ImportApprovedInvoices(ctx, tx, tid)
			if ierr != nil {
				return ierr
			}
			imported += len(created)
			return nil
		})
		if err != nil {
			d.log().Warn("aging_refresh: import payables", "tenant_id", tid, "error", err)
			tenantErrs++
			continue
		}
		// Read-side aging refresh: confirm both positions compute (and keep the
		// plans warm). Errors are per-tenant isolated.
		if _, aerr := d.Receivables.Aging(ctx, tid); aerr != nil {
			d.log().Warn("aging_refresh: AR aging", "tenant_id", tid, "error", aerr)
			tenantErrs++
		}
		if _, aerr := d.Payables.Aging(ctx, tid); aerr != nil {
			d.log().Warn("aging_refresh: AP aging", "tenant_id", tid, "error", aerr)
			tenantErrs++
		}
	}
	return fmt.Sprintf("tenants=%d payables_imported=%d errors=%d", len(tenants), imported, tenantErrs), nil
}

// riskDetectJob runs the detection packs and recomputes station risk scores for
// every active tenant (risk.RunDetection then risk.RecomputeStationScores), in
// one tx per tenant so new alerts and the scores derived from them are
// consistent.
func (d Deps) riskDetectJob(ctx context.Context) (string, error) {
	tenants, err := d.activeTenantIDs(ctx)
	if err != nil {
		return "", fmt.Errorf("list tenants: %w", err)
	}
	var alerts, scored, tenantErrs int
	for _, tid := range tenants {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		err := withTx(ctx, d.Pool, func(tx pgx.Tx) error {
			created, derr := d.Risk.RunDetection(ctx, tx, tid)
			if derr != nil {
				return fmt.Errorf("detect: %w", derr)
			}
			alerts += created
			n, serr := d.Risk.RecomputeStationScores(ctx, tx, tid)
			if serr != nil {
				return fmt.Errorf("recompute scores: %w", serr)
			}
			scored += n
			return nil
		})
		if err != nil {
			d.log().Warn("risk_detect", "tenant_id", tid, "error", err)
			tenantErrs++
		}
	}
	return fmt.Sprintf("tenants=%d alerts_created=%d stations_scored=%d errors=%d", len(tenants), alerts, scored, tenantErrs), nil
}

// projectionJob rebuilds the enterprise station-KPI projection for every active
// tenant (enterprise.RebuildStationKPIs), so the chain dashboards read fresh
// numbers without an operator triggering /enterprise/projections/rebuild.
func (d Deps) projectionJob(ctx context.Context) (string, error) {
	tenants, err := d.activeTenantIDs(ctx)
	if err != nil {
		return "", fmt.Errorf("list tenants: %w", err)
	}
	var rebuilt, tenantErrs int
	for _, tid := range tenants {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		err := withTx(ctx, d.Pool, func(tx pgx.Tx) error {
			n, rerr := d.Enterprise.RebuildStationKPIs(ctx, tx, tid)
			if rerr != nil {
				return rerr
			}
			rebuilt += n
			return nil
		})
		if err != nil {
			d.log().Warn("enterprise_projection", "tenant_id", tid, "error", err)
			tenantErrs++
		}
	}
	return fmt.Sprintf("tenants=%d kpis_rebuilt=%d errors=%d", len(tenants), rebuilt, tenantErrs), nil
}

func (d Deps) log() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}
