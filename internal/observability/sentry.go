package observability

import (
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
)

// SentryConfig keeps the small set of knobs the Go SDK actually cares
// about for an HTTP API. A blank DSN disables Sentry entirely — the
// init returns nil cleanup and the global hub stays a no-op.
type SentryConfig struct {
	DSN         string
	Environment string
	Release     string
	// TracesSampleRate is what fraction of transactions Sentry traces.
	// Keep it tiny in prod; 1.0 in dev for full visibility.
	TracesSampleRate float64
}

// SetupSentry initializes the Sentry SDK if a DSN is supplied. Returns
// a flush function the caller invokes on shutdown — it blocks until
// pending events drain or the timeout elapses.
func SetupSentry(cfg SentryConfig, logger *slog.Logger) (func(), error) {
	if cfg.DSN == "" {
		logger.Info("sentry disabled (DSN unset)")
		return func() {}, nil
	}

	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              cfg.DSN,
		Environment:      cfg.Environment,
		Release:          cfg.Release,
		TracesSampleRate: cfg.TracesSampleRate,
		// We don't ship breadcrumbs from secrets-bearing places yet.
		// When we do, scrub them here.
	}); err != nil {
		return nil, err
	}

	logger.Info("sentry initialized", "environment", cfg.Environment, "release", cfg.Release)
	return func() {
		sentry.Flush(2 * time.Second)
	}, nil
}
