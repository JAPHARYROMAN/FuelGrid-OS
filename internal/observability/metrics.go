// Package observability wires the API's Prometheus, OpenTelemetry, and
// Sentry surfaces. The metrics half lives here; tracing and Sentry are
// in tracing.go and sentry.go.
//
// The exporter handler (`/metrics`) is mounted from services/api/internal/server.
package observability

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Metrics is the registry plus the standard collectors the API exposes.
// One instance per process; main.go constructs it, hands it to the
// server for /metrics, and the http middleware for per-request stats.
type Metrics struct {
	Registry *prometheus.Registry

	HTTPRequests  *prometheus.CounterVec
	HTTPLatency   *prometheus.HistogramVec
	HTTPInflight  prometheus.Gauge
	OutboxBacklog prometheus.Gauge
	OutboxLag     prometheus.Gauge // seconds since the oldest unpublished event was written

	// Business/operational gauges. All are cheap SELECT count(*) probes
	// sampled on the same observe ticker as the outbox gauges, so they add
	// no hot-path cost and need no wiring into the request handlers.
	OutboxDeadLettered prometheus.Gauge // outbox rows parked after exhausting the retry budget
	OpenShifts         prometheus.Gauge // shifts currently in the 'open' state across all tenants
	JournalEntries     prometheus.Gauge // posted journal entries (financial-throughput signal)

	// pgx connection-pool stats, sampled from pool.Stat() on the same observe
	// ticker. These are deliberately NOT fuelgrid_-prefixed: the names match the
	// community pgxpool collector convention (pgxpool_*) that the dashboards and
	// the DbPoolSaturation alert already reference, so wiring them here turns
	// those previously-inert panels/alerts live without touching the YAML/JSON.
	PoolAcquiredConns prometheus.Gauge // connections currently checked out (in use)
	PoolIdleConns     prometheus.Gauge // idle connections held open in the pool
	PoolTotalConns    prometheus.Gauge // total connections currently established (acquired + idle + constructing)
	PoolMaxConns      prometheus.Gauge // configured pool ceiling

	// Scheduler (internal/scheduler). The background-jobs runner records one
	// observation per job execution: a success/failure-labelled counter and a
	// duration histogram, both keyed by job name. These let operators alert on a
	// job that has stopped succeeding (or has started taking too long) without
	// scraping the job_runs ledger.
	SchedulerRuns     *prometheus.CounterVec   // labels: job, result(success|failure|skipped)
	SchedulerDuration *prometheus.HistogramVec // labels: job
}

// NewMetrics builds the registry with the Go runtime + process collectors
// plus FuelGrid-specific gauges/counters. Application code records into
// the typed fields; the /metrics handler exposes Registry.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := &Metrics{
		Registry: reg,
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "fuelgrid",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Count of HTTP requests by method, path template, and status.",
		}, []string{"method", "path", "status"}),
		HTTPLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "fuelgrid",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "Latency of HTTP requests in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.005, 2, 12),
		}, []string{"method", "path", "status"}),
		HTTPInflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "fuelgrid",
			Subsystem: "http",
			Name:      "requests_inflight",
			Help:      "HTTP requests currently being served.",
		}),
		OutboxBacklog: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "fuelgrid",
			Subsystem: "outbox",
			Name:      "unpublished_events",
			Help:      "Count of outbox_events rows where published_at IS NULL.",
		}),
		OutboxLag: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "fuelgrid",
			Subsystem: "outbox",
			Name:      "oldest_unpublished_age_seconds",
			Help:      "Age of the oldest unpublished outbox row, in seconds.",
		}),
		OutboxDeadLettered: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "fuelgrid",
			Subsystem: "outbox",
			Name:      "dead_lettered_events",
			Help:      "Count of outbox_events rows parked with failed_at set (retry budget exhausted).",
		}),
		OpenShifts: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "fuelgrid",
			Subsystem: "shifts",
			Name:      "open",
			Help:      "Count of shifts currently in the 'open' state across all tenants.",
		}),
		JournalEntries: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "fuelgrid",
			Subsystem: "accounting",
			Name:      "journal_entries_posted",
			Help:      "Count of journal_entries rows in the 'posted' state across all tenants.",
		}),
		SchedulerRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "fuelgrid",
			Subsystem: "scheduler",
			Name:      "job_runs_total",
			Help:      "Count of background scheduler job executions by job and result.",
		}, []string{"job", "result"}),
		SchedulerDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "fuelgrid",
			Subsystem: "scheduler",
			Name:      "job_duration_seconds",
			Help:      "Wall-clock duration of background scheduler job executions, in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.01, 3, 10),
		}, []string{"job"}),
		// Bare pgxpool_* names (no Namespace/Subsystem) to match the dashboard
		// and DbPoolSaturation alert references verbatim.
		PoolAcquiredConns: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pgxpool_acquired_conns",
			Help: "Number of pgx pool connections currently acquired (in use).",
		}),
		PoolIdleConns: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pgxpool_idle_conns",
			Help: "Number of idle pgx pool connections held open.",
		}),
		PoolTotalConns: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pgxpool_total_conns",
			Help: "Total number of pgx pool connections currently established.",
		}),
		PoolMaxConns: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pgxpool_max_conns",
			Help: "Configured maximum size of the pgx connection pool.",
		}),
	}

	reg.MustRegister(
		m.HTTPRequests, m.HTTPLatency, m.HTTPInflight,
		m.OutboxBacklog, m.OutboxLag, m.OutboxDeadLettered,
		m.OpenShifts, m.JournalEntries,
		m.SchedulerRuns, m.SchedulerDuration,
		m.PoolAcquiredConns, m.PoolIdleConns, m.PoolTotalConns, m.PoolMaxConns,
	)
	return m
}

// ObserveJob records one scheduler job execution: it increments the
// result-labelled run counter and observes the run's duration. result is one of
// "success", "failure", or "skipped". Safe to call from the runner goroutine;
// a nil *Metrics is tolerated so the scheduler can run without metrics wired.
func (m *Metrics) ObserveJob(job, result string, d time.Duration) {
	if m == nil {
		return
	}
	m.SchedulerRuns.WithLabelValues(job, result).Inc()
	m.SchedulerDuration.WithLabelValues(job).Observe(d.Seconds())
}

// ObserveOutbox reads outbox stats and updates the gauges. Safe to call
// on a timer from a worker.
//
// Backlog and lag count only *drainable* rows (failed_at IS NULL) so they
// track what the publisher will actually attempt — a parked dead-letter row
// is unpublished but is not work the loop will retry, and counting it would
// make the backlog look permanently non-zero. Dead-lettered rows get their
// own gauge so the parked count is still visible (and alertable).
func (m *Metrics) ObserveOutbox(ctx context.Context, pool *database.Pool) error {
	var backlog, deadLettered int64
	var oldestAgeSeconds float64
	err := pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE published_at IS NULL AND failed_at IS NULL),
			coalesce(extract(epoch FROM (now() - min(occurred_at) FILTER (WHERE published_at IS NULL AND failed_at IS NULL))), 0),
			count(*) FILTER (WHERE failed_at IS NOT NULL)
		FROM outbox_events
		WHERE published_at IS NULL
	`).Scan(&backlog, &oldestAgeSeconds, &deadLettered)
	if err != nil {
		return err
	}
	m.OutboxBacklog.Set(float64(backlog))
	m.OutboxLag.Set(oldestAgeSeconds)
	m.OutboxDeadLettered.Set(float64(deadLettered))
	return nil
}

// ObserveBusiness samples cheap domain gauges (open shifts, posted journal
// entries) and updates them. Like ObserveOutbox it is read-only and safe to
// call on the metrics observe timer; both counts are single indexed
// count(*) probes. Sampling here keeps the request handlers free of metrics
// wiring at the cost of refresh granularity equal to the observe interval.
func (m *Metrics) ObserveBusiness(ctx context.Context, pool *database.Pool) error {
	var openShifts, journalEntries int64
	err := pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM shifts WHERE status = 'open'),
			(SELECT count(*) FROM journal_entries WHERE status = 'posted')
	`).Scan(&openShifts, &journalEntries)
	if err != nil {
		return err
	}
	m.OpenShifts.Set(float64(openShifts))
	m.JournalEntries.Set(float64(journalEntries))
	return nil
}

// ObservePool snapshots pgxpool.Pool.Stat() into the pgxpool_* gauges. Unlike
// the other observers it issues no SQL — Stat() reads in-process pool
// bookkeeping — so it is cheap, always succeeds, and works even while the
// database is unreachable (acquired/idle drop, total reflects what's left).
// A nil *Metrics or nil pool is tolerated so callers need not guard.
func (m *Metrics) ObservePool(pool *database.Pool) {
	if m == nil || pool == nil || pool.Pool == nil {
		return
	}
	s := pool.Stat()
	m.PoolAcquiredConns.Set(float64(s.AcquiredConns()))
	m.PoolIdleConns.Set(float64(s.IdleConns()))
	m.PoolTotalConns.Set(float64(s.TotalConns()))
	m.PoolMaxConns.Set(float64(s.MaxConns()))
}

// inflight is an int64 monotonic counter exposed via the HTTPInflight
// gauge. atomic so the middleware can bump it without locking.
type inflight struct{ n atomic.Int64 }

func (i *inflight) Inc() int64 { return i.n.Add(1) }
func (i *inflight) Dec() int64 { return i.n.Add(-1) }

// Inflight returns a counter the HTTP middleware uses to keep
// HTTPInflight current without holding a lock.
func (m *Metrics) Inflight() interface {
	Inc() int64
	Dec() int64
} {
	return &inflight{}
}
