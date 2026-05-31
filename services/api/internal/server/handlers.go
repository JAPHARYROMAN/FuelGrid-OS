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
		err := s.deps.DB.Ping(ctx)
		cancel()
		if err != nil {
			checks["postgres"] = "unreachable: " + err.Error()
			allOK = false
		} else {
			checks["postgres"] = "ok"
		}
		// Keep the cached health flag (REL-5) consistent with this live ping so
		// readyz and the cheap shedding path agree; the background checker keeps
		// it fresh between readyz hits. Never flips to unhealthy when the checker
		// hasn't started (default-healthy) — readyz only observes the real ping.
		s.dbHealthy.Store(err == nil)
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

// decodeJSON parses a request body. Surfaces a single uniform "invalid
// JSON body" error so handlers don't each do their own decoder boilerplate.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errInvalidJSON
	}
	return nil
}

var errInvalidJSON = &decodeError{msg: "invalid JSON body"}

type decodeError struct{ msg string }

func (e *decodeError) Error() string { return e.msg }
