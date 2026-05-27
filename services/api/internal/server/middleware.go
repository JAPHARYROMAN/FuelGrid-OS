package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

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

// logRequests emits one structured log line per HTTP request. Field
// names follow the architecture doc §24.1 + the Stage-10 standardization:
//
//	request_id, correlation_id, tenant_id, user_id, service, operation,
//	method, path, status, bytes, latency_ms, remote, user_agent
//
// tenant_id and user_id are populated when the auth middleware has
// already injected an actor. correlation_id mirrors request_id today;
// when distributed tracing matures it will come from the trace context.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()

		defer func() {
			reqID := chimiddleware.GetReqID(r.Context())
			actor := identity.ActorFrom(r.Context())

			attrs := []any{
				"request_id", reqID,
				"correlation_id", reqID,
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
