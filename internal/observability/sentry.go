package observability

import (
	"log/slog"
	"regexp"
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
//
// SR-I2: BeforeSend and BeforeBreadcrumb run a conservative PII/secret
// scrubber over every outbound event so that even if an auth-path message or
// breadcrumb is added later, obvious emails, tokens, phone numbers and
// password fields are redacted before anything leaves the process.
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
		// SR-I2: defensive PII/secret scrubbing. No PII reaches Sentry today
		// (we only ship request_id/method/status/route), but these hooks make
		// the redaction unconditional so a future breadcrumb in an auth path
		// can't leak credentials.
		BeforeSend:       scrubEvent,
		BeforeBreadcrumb: scrubBreadcrumb,
	}); err != nil {
		return nil, err
	}

	logger.Info("sentry initialized", "environment", cfg.Environment, "release", cfg.Release)
	return func() {
		sentry.Flush(2 * time.Second)
	}, nil
}

// ---- PII / secret scrubbing (SR-I2) ----
//
// These patterns are intentionally conservative: they target the high-signal
// shapes (email, bearer/authorization token, password key=value, long digit
// runs that look like phone numbers) and replace the sensitive span with a
// fixed placeholder. They are NOT a guarantee of zero leakage — they are
// defense in depth on top of the fact that we don't deliberately ship PII to
// Sentry. Keep them cheap and order-independent.
const piiRedacted = "[redacted]"

var (
	// Email addresses, e.g. alice@example.com.
	reEmail = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)

	// Bearer / authorization tokens. Matches "Bearer <token>" and
	// "Authorization: <scheme> <token>" forms, redacting the credential while
	// leaving the scheme word for context.
	reBearer = regexp.MustCompile(`(?i)\b(bearer|authorization:\s*\w+)\s+[A-Za-z0-9._\-+/=]{8,}`)

	// password / secret / token / api[_-]key assignments in either key=value or
	// key": "value JSON-ish forms. The value (up to a delimiter) is redacted.
	reSecretKV = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|pepper)\b"?\s*[=:]\s*"?[^"&,;\s]+`)

	// Phone numbers: an optional +, then 9+ digits possibly separated by spaces
	// or dashes. Deliberately requires a fairly long run to avoid eating ordinary
	// small integers (ids, counts, ports).
	rePhone = regexp.MustCompile(`\+?\d[\d \-]{8,}\d`)
)

// scrubString applies every redaction pattern to s. Order matters only in that
// the more specific secret/token patterns run before the generic phone-number
// pass so a token's digit runs are already gone.
func scrubString(s string) string {
	if s == "" {
		return s
	}
	s = reBearer.ReplaceAllString(s, "$1 "+piiRedacted)
	s = reSecretKV.ReplaceAllString(s, "$1="+piiRedacted)
	s = reEmail.ReplaceAllString(s, piiRedacted)
	s = rePhone.ReplaceAllString(s, piiRedacted)
	return s
}

// scrubEvent is the Sentry BeforeSend hook. It redacts PII/secrets from the
// places free-form text can ride along: the message, exception values, request
// data/query/headers, breadcrumbs, and string tag values. It never drops the
// event (returns nil) — redaction only.
func scrubEvent(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event == nil {
		return event
	}
	event.Message = scrubString(event.Message)
	event.Transaction = scrubString(event.Transaction)

	for i := range event.Exception {
		event.Exception[i].Value = scrubString(event.Exception[i].Value)
	}

	if event.Request != nil {
		event.Request.Data = scrubString(event.Request.Data)
		event.Request.QueryString = scrubString(event.Request.QueryString)
		event.Request.Cookies = scrubString(event.Request.Cookies)
		event.Request.URL = scrubString(event.Request.URL)
		for k, v := range event.Request.Headers {
			event.Request.Headers[k] = scrubString(v)
		}
	}

	for _, b := range event.Breadcrumbs {
		scrubBreadcrumb(b, nil)
	}

	for k, v := range event.Tags {
		event.Tags[k] = scrubString(v)
	}

	return event
}

// scrubBreadcrumb is the Sentry BeforeBreadcrumb hook (and is also reused to
// scrub breadcrumbs already attached to an event). It redacts the message and
// any string values in the breadcrumb data map.
func scrubBreadcrumb(breadcrumb *sentry.Breadcrumb, _ *sentry.BreadcrumbHint) *sentry.Breadcrumb {
	if breadcrumb == nil {
		return breadcrumb
	}
	breadcrumb.Message = scrubString(breadcrumb.Message)
	for k, v := range breadcrumb.Data {
		if sv, ok := v.(string); ok {
			breadcrumb.Data[k] = scrubString(sv)
		}
	}
	return breadcrumb
}
