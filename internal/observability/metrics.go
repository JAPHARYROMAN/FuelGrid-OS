// Package observability wires the API's Prometheus, OpenTelemetry, and
// Sentry surfaces. The metrics half lives here; tracing and Sentry are
// in tracing.go and sentry.go.
//
// The exporter handler (`/metrics`) is mounted from services/api/internal/server.
package observability

import (
	"context"
	"sync/atomic"

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
	}

	reg.MustRegister(m.HTTPRequests, m.HTTPLatency, m.HTTPInflight, m.OutboxBacklog, m.OutboxLag)
	return m
}

// ObserveOutbox reads outbox stats and updates the gauges. Safe to call
// on a timer from a worker.
func (m *Metrics) ObserveOutbox(ctx context.Context, pool *database.Pool) error {
	var backlog int64
	var oldestAgeSeconds float64
	err := pool.QueryRow(ctx, `
		SELECT count(*),
		       coalesce(extract(epoch FROM (now() - min(occurred_at))), 0)
		FROM outbox_events
		WHERE published_at IS NULL
	`).Scan(&backlog, &oldestAgeSeconds)
	if err != nil {
		return err
	}
	m.OutboxBacklog.Set(float64(backlog))
	m.OutboxLag.Set(oldestAgeSeconds)
	return nil
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
