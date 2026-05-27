// Package server owns the HTTP composition root: router, middleware
// stack, route table, and graceful lifecycle.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// handleHealthz is the liveness probe. It returns 200 as long as the
// HTTP server is up; it intentionally does not touch databases or caches.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz is the readiness probe. It pings every configured backing
// service. A single unhealthy dep yields 503 with the failing dep named.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	allOK := true

	if s.deps.DB != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		if err := s.deps.DB.Ping(ctx); err != nil {
			checks["postgres"] = "unreachable: " + err.Error()
			allOK = false
		} else {
			checks["postgres"] = "ok"
		}
		cancel()
	}

	if s.deps.Redis != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		if err := s.deps.Redis.Ping(ctx).Err(); err != nil {
			checks["redis"] = "unreachable: " + err.Error()
			allOK = false
		} else {
			checks["redis"] = "ok"
		}
		cancel()
	}

	status := http.StatusOK
	body := map[string]any{"status": "ready", "checks": checks}
	if !allOK {
		status = http.StatusServiceUnavailable
		body["status"] = "not ready"
	}
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
