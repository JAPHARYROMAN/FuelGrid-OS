package server

import (
	"errors"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/ratelimit"
)

// rateLimiter holds the request-throttling state shared across requests: the
// Redis-backed per-tenant limiter and the process-wide in-flight counter.
//
// It implements two independent guards (REL-4), layered on top of the existing
// login limiter, both designed to fail open so limiter infrastructure can never
// hard-fail live traffic:
//
//   - perTenant: an authenticated-request quota keyed by tenant_id over a fixed
//     window (reusing internal/identity/ratelimit.Limiter). On exceed it returns
//     429 with a Retry-After header.
//   - inflight: a counted cap on concurrent in-flight requests. The request that
//     would push the count past max is shed with 503 + Retry-After to protect the
//     process under overload.
//
// A non-positive tenant limit disables the per-tenant guard; a non-positive
// max-inflight disables the cap. Both default to disabled when the Config is
// built without Load() (Go zero values), so test harnesses are never throttled.
type rateLimiter struct {
	limiter     *ratelimit.Limiter // nil => per-tenant guard disabled (no Redis)
	tenantLimit int64
	tenantWin   time.Duration

	// SR-L3: per-IP guard for the public password-reset endpoints (request /
	// confirm). It reuses the same Redis-backed limiter under its own key prefix.
	// A nil limiter or non-positive limit disables it.
	pwResetLimiter *ratelimit.Limiter
	pwResetLimit   int64
	pwResetWin     time.Duration

	maxInflight int64         // 0 => unlimited
	cur         atomic.Int64  // current in-flight count
	retryAfter  time.Duration // advisory Retry-After for shed/limited responses

	logOnce   sync.Once // per-tenant fail-open log is emitted at most once per process
	pwLogOnce sync.Once // password-reset fail-open log is emitted at most once per process
}

// newRateLimiter builds the throttling state from config. limiter may be nil
// (Redis not wired): the per-tenant guard then no-ops and only the in-flight
// cap (if configured) applies.
func newRateLimiter(limiter *ratelimit.Limiter, tenantLimit int64, tenantWin time.Duration, maxInflight int64) *rateLimiter {
	if tenantWin <= 0 {
		tenantWin = time.Minute
	}
	return &rateLimiter{
		limiter:     limiter,
		tenantLimit: tenantLimit,
		tenantWin:   tenantWin,
		maxInflight: maxInflight,
		retryAfter:  time.Second,
	}
}

// perTenantEnabled reports whether the per-tenant quota is active.
func (rl *rateLimiter) perTenantEnabled() bool {
	return rl != nil && rl.limiter != nil && rl.tenantLimit > 0
}

// withPasswordReset wires the per-IP password-reset guard (SR-L3) onto an
// already-built limiter. Kept separate from newRateLimiter so the password-reset
// guard can be configured without disturbing the existing constructor signature
// and the call sites/tests that depend on it.
func (rl *rateLimiter) withPasswordReset(limiter *ratelimit.Limiter, limit int64, win time.Duration) *rateLimiter {
	if win <= 0 {
		win = 15 * time.Minute
	}
	rl.pwResetLimiter = limiter
	rl.pwResetLimit = limit
	rl.pwResetWin = win
	return rl
}

// pwResetEnabled reports whether the per-IP password-reset guard is active.
func (rl *rateLimiter) pwResetEnabled() bool {
	return rl != nil && rl.pwResetLimiter != nil && rl.pwResetLimit > 0
}

// pwResetBucketKey derives the per-IP rate-limit bucket key for the
// password-reset endpoints. Pure (no Redis) so the keying scheme is unit
// testable.
func pwResetBucketKey(ip string) string {
	return "pwreset:ip:" + ip
}

// inflightEnabled reports whether the global in-flight cap is active.
func (rl *rateLimiter) inflightEnabled() bool {
	return rl != nil && rl.maxInflight > 0
}

// tenantBucketKey derives the per-tenant rate-limit bucket key for an actor.
// It is a pure function (no Redis) so the keying scheme can be unit tested.
func tenantBucketKey(a identity.Actor) string {
	return "tenant:" + a.TenantID.String()
}

// limitInflight is the global concurrency guard. It runs early in the chain
// (before auth) and sheds load with 503 when the in-flight count would exceed
// the configured cap. It is a no-op when the cap is disabled or for the
// operational probes (/healthz, /readyz, /metrics), which must stay reachable
// even when the service is shedding application traffic.
func (s *Server) limitInflight(next http.Handler) http.Handler {
	rl := s.rateLimit
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.inflightEnabled() || isProbePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		n := rl.cur.Add(1)
		defer rl.cur.Add(-1)
		if n > rl.maxInflight {
			// Over capacity: shed this request so the in-flight ones can drain.
			w.Header().Set("Retry-After", strconv.Itoa(int(rl.retryAfter.Seconds())))
			writeError(w, http.StatusServiceUnavailable, "server busy, retry shortly")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimitPerTenant is the per-tenant request-quota guard. It is applied on
// the authenticated route subtrees (after requireAuth has injected the actor),
// so it gates strictly on actor presence: unauthenticated requests fall through
// untouched (login is throttled separately by the identity service).
//
// On Redis error the request is allowed (fail-open) and the failure is logged
// once — limiter infrastructure must never hard-fail live traffic.
func (s *Server) rateLimitPerTenant(next http.Handler) http.Handler {
	rl := s.rateLimit
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.perTenantEnabled() {
			next.ServeHTTP(w, r)
			return
		}
		actor := identity.ActorFrom(r.Context())
		if !actor.IsAuthenticated() {
			// No actor yet (or anonymous): out of scope for the per-tenant quota.
			next.ServeHTTP(w, r)
			return
		}

		err := rl.limiter.Allow(r.Context(), tenantBucketKey(actor), rl.tenantLimit, rl.tenantWin)
		switch {
		case err == nil:
			next.ServeHTTP(w, r)
		case errors.Is(err, ratelimit.ErrLimited):
			w.Header().Set("Retry-After", strconv.Itoa(int(rl.tenantWin.Seconds())))
			writeError(w, http.StatusTooManyRequests, "tenant request quota exceeded")
		default:
			// Redis unavailable / errored: FAIL OPEN. Allow the request and log
			// once so we don't flood logs on a sustained Redis outage.
			rl.logOnce.Do(func() {
				s.logger.Warn("rate limiter failing open (redis unavailable)", "error", err)
			})
			next.ServeHTTP(w, r)
		}
	})
}

// rateLimitPasswordResetIP is the per-IP guard for the public password-reset
// endpoints (SR-L3). These routes carry no actor (they run before auth) and are
// not covered by the identity service's login buckets, so this is the only
// HTTP-layer throttle on the reset-token confirm/replay surface. It is keyed by
// client IP (clientIP honours X-Forwarded-For only behind a trusted proxy,
// AUTH-09) and fails open on a Redis error so limiter infrastructure can never
// hard-fail the reset flow.
func (s *Server) rateLimitPasswordResetIP(next http.Handler) http.Handler {
	rl := s.rateLimit
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.pwResetEnabled() {
			next.ServeHTTP(w, r)
			return
		}
		err := rl.pwResetLimiter.Allow(r.Context(), pwResetBucketKey(clientIP(r)), rl.pwResetLimit, rl.pwResetWin)
		switch {
		case err == nil:
			next.ServeHTTP(w, r)
		case errors.Is(err, ratelimit.ErrLimited):
			w.Header().Set("Retry-After", strconv.Itoa(int(rl.pwResetWin.Seconds())))
			writeError(w, http.StatusTooManyRequests, "too many password-reset attempts, retry later")
		default:
			// Redis unavailable / errored: FAIL OPEN, log once.
			rl.pwLogOnce.Do(func() {
				s.logger.Warn("password-reset rate limiter failing open (redis unavailable)", "error", err)
			})
			next.ServeHTTP(w, r)
		}
	})
}

// isProbePath reports whether the path is one of the operational endpoints that
// must bypass throttling so liveness/readiness/metrics stay observable under
// load.
func isProbePath(p string) bool {
	switch p {
	case "/healthz", "/readyz", "/metrics":
		return true
	default:
		return false
	}
}
