package server

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// defaultHealthcheckInterval is the fallback DB-ping cadence used when the
// Config carries a non-positive interval (e.g. a Config built without Load()).
const defaultHealthcheckInterval = 3 * time.Second

// healthcheckPingTimeout bounds each background DB ping so a hung/dead database
// can never wedge the checker goroutine; it is kept short relative to the
// interval so an unreachable DB is observed promptly.
const healthcheckPingTimeout = 2 * time.Second

// shedRetryAfter is the advisory Retry-After (seconds) returned on a shed 503,
// telling clients/orchestrators roughly when to try again.
const shedRetryAfter = 5

// startHealthcheck launches the background DB health checker (REL-5). It Pings
// the database on a timer and caches the result in s.dbHealthy, so the shedding
// middleware can decide whether to shed with a single atomic read — no DB call
// in the request hot path.
//
// It is a no-op when deps.DB is nil (a thin smoke deployment): the checker never
// runs and dbHealthy stays at its default-healthy value, so nothing is ever
// shed. Called from Start(); stopped by Shutdown() via s.stopHealthcheck.
func (s *Server) startHealthcheck() {
	if s.deps.DB == nil {
		return
	}

	interval := s.cfg.HealthcheckInterval
	if interval <= 0 {
		interval = defaultHealthcheckInterval
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.stopHealthcheck = cancel
	s.healthcheckDone = make(chan struct{})

	go func() {
		defer close(s.healthcheckDone)
		t := time.NewTicker(interval)
		defer t.Stop()

		// Prime once immediately so a DB that is already down is observed
		// without waiting a full interval.
		s.pingDBOnce(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.pingDBOnce(ctx)
			}
		}
	}()
}

// pingDBOnce performs one bounded DB ping and updates the cached health flag.
// A failed ping flips dbHealthy to false (shedding turns on); a successful ping
// flips it back to true (recovery turns shedding off). The transition is logged
// so operators see the degrade/recover edges.
func (s *Server) pingDBOnce(ctx context.Context) {
	pingCtx, cancel := context.WithTimeout(ctx, healthcheckPingTimeout)
	err := s.deps.DB.Ping(pingCtx)
	cancel()

	healthy := err == nil
	if prev := s.dbHealthy.Swap(healthy); prev != healthy {
		if healthy {
			s.logger.Info("database recovered — load shedding disabled")
		} else {
			s.logger.Warn("database unreachable — shedding regular traffic with 503", "error", err)
		}
	}
}

// stopHealthcheckChecker cancels the background checker and waits for the
// goroutine to exit, so shutdown never leaks it. Safe to call when the checker
// was never started (deps.DB nil, or Start() not called — as in the integration
// harness): both fields are then nil and this is a no-op.
func (s *Server) stopHealthcheckChecker() {
	if s.stopHealthcheck != nil {
		s.stopHealthcheck()
	}
	if s.healthcheckDone != nil {
		<-s.healthcheckDone
	}
}

// shedWhenUnhealthy is the readiness-aware load-shedding middleware (REL-5).
// For ordinary API traffic it reads the cached dbHealthy flag (a single atomic
// load — no DB call) and, when the database has been observed unreachable,
// returns 503 + Retry-After fast instead of letting the request block on a dead
// dependency and pile up.
//
// It always bypasses the operational probes (/healthz, /readyz, /metrics) so the
// orchestrator can still see health and route recovery. The flag defaults to
// healthy and is only flipped by an observed failed ping from the background
// checker, which is started solely by Start(); a harness that builds the Server
// against a real DB but never calls Start() (or runs against a healthy DB) keeps
// the flag healthy, so this middleware passes every request through and CI stays
// green.
func (s *Server) shedWhenUnhealthy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isProbePath(r.URL.Path) || s.dbHealthy.Load() {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Retry-After", strconv.Itoa(shedRetryAfter))
		writeError(w, http.StatusServiceUnavailable, "database unavailable, retry shortly")
	})
}
