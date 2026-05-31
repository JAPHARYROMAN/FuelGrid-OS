package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// captureErrors reports server faults to Sentry with request context. It sits
// immediately inside Recoverer (registered right after it): on a panic it
// captures the exception to Sentry, flushes, then re-panics so Recoverer still
// turns it into a clean 500; on a normal 5xx response it captures a message.
// Every event is tagged with the request id and matched route for correlation.
// A blank Sentry DSN leaves CurrentHub clientless, so capture is a safe no-op —
// no guard or config flag needed (OBS-3). 4xx and below are never captured.
func (s *Server) captureErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub := sentry.CurrentHub().Clone()
		reqID := chimiddleware.GetReqID(r.Context())
		hub.ConfigureScope(func(scope *sentry.Scope) {
			scope.SetTag("request_id", reqID)
			scope.SetTag("http.method", r.Method)
			scope.SetContext("request", map[string]any{"method": r.Method, "path": r.URL.Path})
		})

		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		defer func() {
			if rec := recover(); rec != nil {
				hub.ConfigureScope(func(scope *sentry.Scope) { scope.SetTag("http.route", routePattern(r)) })
				hub.RecoverWithContext(r.Context(), rec)
				hub.Flush(2 * time.Second)
				panic(rec) // let Recoverer produce the 500 response
			}
			if ww.Status() >= 500 {
				hub.WithScope(func(scope *sentry.Scope) {
					scope.SetLevel(sentry.LevelError)
					scope.SetTag("http.route", routePattern(r))
					scope.SetTag("http.status", strconv.Itoa(ww.Status()))
					hub.CaptureMessage("HTTP " + strconv.Itoa(ww.Status()) + " " + r.Method + " " + routePattern(r))
				})
			}
		}()
		next.ServeHTTP(ww, r)
	})
}

// echoRequestID copies the chi-generated request ID into the X-Request-Id
// response header so clients can quote it in bug reports and we can grep
// logs by it. chi's RequestID middleware only stores it on the context.
func echoRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := chimiddleware.GetReqID(r.Context()); id != "" {
			w.Header().Set("X-Request-Id", id)
		}
		next.ServeHTTP(w, r)
	})
}

// traceRequests wraps every request in an OpenTelemetry server span using the
// otelhttp instrumentation. The span carries standard http.server semantic
// attributes and propagates inbound traceparent headers so the API joins an
// existing distributed trace when one is present.
//
// The span is named by the chi route template (e.g. "GET /api/v1/stations/
// {stationID}") rather than the raw URL, keeping span cardinality bounded the
// same way recordMetrics bounds its Prometheus labels. Because otelhttp runs
// before chi has matched a route, the template is unknown when the span is
// created; the inner tagRoute step rewrites the span name and sets the
// http.route attribute once routing has resolved.
//
// When tracing is disabled (OtelExporter=none) the global provider is the OTel
// no-op tracer: spans are non-recording and carry an invalid SpanContext, so
// this is a zero-config no-op and logRequests falls back to request_id.
func (s *Server) traceRequests(next http.Handler) http.Handler {
	tagRoute := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if span := trace.SpanFromContext(r.Context()); span.IsRecording() {
			pattern := routePattern(r)
			span.SetName(r.Method + " " + pattern)
			span.SetAttributes(semconv.HTTPRoute(pattern))
		}
		next.ServeHTTP(w, r)
	})
	// otelhttp creates the span; the operation arg is only a placeholder
	// because the span name formatter (and tagRoute) override it per request.
	return otelhttp.NewHandler(tagRoute, "http.server",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
}

// logRequests emits one structured log line per HTTP request. Field
// names follow the architecture doc §24.1 + the Stage-10 standardization:
//
//	request_id, correlation_id, tenant_id, user_id, service, operation,
//	method, path, status, bytes, latency_ms, remote, user_agent
//
// tenant_id and user_id are populated when the auth middleware has
// already injected an actor. correlation_id is the active trace's TraceID
// when a recording span exists (set by traceRequests), so log lines and
// distributed traces join on the same id; it falls back to request_id when
// tracing is disabled or no span is in flight.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()

		defer func() {
			reqID := chimiddleware.GetReqID(r.Context())
			actor := identity.ActorFrom(r.Context())

			// Join logs to the trace on the TraceID when a span is
			// recording; otherwise mirror request_id (no-op tracer or no
			// inbound trace) so the field is always populated.
			correlationID := reqID
			if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
				correlationID = sc.TraceID().String()
			}

			attrs := []any{
				"request_id", reqID,
				"correlation_id", correlationID,
				"service", "fuelgrid-api",
				"operation", routePattern(r),
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"latency_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			}
			if actor.IsAuthenticated() {
				attrs = append(attrs, "tenant_id", actor.TenantID.String(), "user_id", actor.UserID.String())
			}

			s.logger.Info("http request", attrs...)
		}()

		next.ServeHTTP(ww, r)
	})
}

// recordMetrics increments the HTTP counters and histograms. The status
// label is bucketed (2xx / 3xx / 4xx / 5xx) to keep cardinality bounded;
// path uses the chi route pattern (e.g. "/api/v1/stations/{stationID}")
// rather than the literal URL so we don't blow up the label space.
func (s *Server) recordMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.metrics == nil {
			next.ServeHTTP(w, r)
			return
		}

		s.metrics.HTTPInflight.Inc()
		defer s.metrics.HTTPInflight.Dec()

		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)

		pattern := routePattern(r)
		statusClass := strconv.Itoa(ww.Status()/100) + "xx"

		s.metrics.HTTPRequests.WithLabelValues(r.Method, pattern, statusClass).Inc()
		s.metrics.HTTPLatency.WithLabelValues(r.Method, pattern, statusClass).
			Observe(time.Since(start).Seconds())
	})
}

// routePattern returns the chi route template that matched the request,
// or the raw path when no template matched (e.g. /healthz, /metrics).
// Stripped to keep Prometheus label cardinality bounded.
func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	// Fall back to a coarse bucket so we don't accidentally label by
	// per-request id.
	path := r.URL.Path
	if i := strings.IndexByte(path[1:], '/'); i > 0 {
		return path[:i+1]
	}
	return path
}

// maxRequestBody is the global cap on request body size: generous enough for
// JSON payloads and CSV strapping-chart uploads, bounded so a client can't
// exhaust server memory with an unbounded (or malicious) body.
const maxRequestBody = 4 << 20 // 4 MiB

// limitRequestBody wraps every request body in http.MaxBytesReader so a read
// past the cap fails instead of buffering unbounded input. Handlers surface
// the failed read as a 400 ("invalid JSON body") or the server emits 413.
func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}
