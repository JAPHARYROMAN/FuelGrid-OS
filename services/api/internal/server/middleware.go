package server

import (
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
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

// logRequests emits one structured log line per HTTP request. Field names
// are aligned with the observability standard documented in the
// architecture doc §24.1 so downstream log queries stay consistent.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()

		defer func() {
			s.logger.Info("http request",
				"request_id", chimiddleware.GetReqID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"latency_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			)
		}()

		next.ServeHTTP(ww, r)
	})
}
