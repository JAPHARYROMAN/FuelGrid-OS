package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/ratelimit"
)

// TestTenantBucketKey pins the per-tenant bucket-key derivation: the key is
// stable, namespaced, and keyed strictly by tenant (not by user), so every
// request from a tenant shares one quota window.
func TestTenantBucketKey(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001")
	a1 := identity.Actor{UserID: uuid.New(), TenantID: tenant}
	a2 := identity.Actor{UserID: uuid.New(), TenantID: tenant}

	if got := tenantBucketKey(a1); got != "tenant:"+tenant.String() {
		t.Fatalf("unexpected bucket key: %q", got)
	}
	if tenantBucketKey(a1) != tenantBucketKey(a2) {
		t.Fatalf("two actors in the same tenant must share a bucket key")
	}

	other := identity.Actor{UserID: uuid.New(), TenantID: uuid.New()}
	if tenantBucketKey(a1) == tenantBucketKey(other) {
		t.Fatalf("different tenants must get different bucket keys")
	}
}

func newRateLimitServer(rl *rateLimiter) *Server {
	return &Server{
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		rateLimit: rl,
	}
}

// TestLimitInflightDisabled: a zero/negative max-inflight is a no-op — every
// request passes. This is the default the integration harness relies on.
func TestLimitInflightDisabled(t *testing.T) {
	s := newRateLimitServer(newRateLimiter(nil, 0, 0, 0))
	h := s.limitInflight(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 1000; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: want 200, got %d", i, rec.Code)
		}
	}
}

// TestLimitInflightSheds: with a cap of N, the N+1th concurrent in-flight
// request is shed with 503 + Retry-After; once requests drain, traffic flows
// again. We gate the handler on a release channel to hold requests in flight.
func TestLimitInflightSheds(t *testing.T) {
	const maxN = 3
	s := newRateLimitServer(newRateLimiter(nil, 0, 0, maxN))

	release := make(chan struct{})
	entered := make(chan struct{}, maxN)
	h := s.limitInflight(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	// Saturate the cap with maxN requests that block inside the handler.
	var wg sync.WaitGroup
	codes := make([]int, maxN)
	for i := 0; i < maxN; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
			codes[i] = rec.Code
		}(i)
	}
	// Wait until all maxN requests are actually inside the handler.
	for i := 0; i < maxN; i++ {
		<-entered
	}

	// The next request must be shed: the maxN is fully saturated.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("over-cap request: want 503, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatalf("503 response must carry a Retry-After header")
	}

	// Drain the in-flight requests; the counter must fall back so traffic flows.
	close(release)
	wg.Wait()
	for i, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("in-flight request %d: want 200, got %d", i, c)
		}
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("post-drain request: want 200, got %d", rec.Code)
	}
}

// TestLimitInflightSkipsProbes: operational probes must never be shed, even
// when the cap is saturated, so liveness/readiness/metrics stay observable.
func TestLimitInflightSkipsProbes(t *testing.T) {
	s := newRateLimitServer(newRateLimiter(nil, 0, 0, 1))
	// Pre-saturate the counter as if a request were already in flight.
	s.rateLimit.cur.Add(1)

	h := s.limitInflight(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, p := range []string{"/healthz", "/readyz", "/metrics"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("probe %s must bypass the cap: got %d", p, rec.Code)
		}
	}
}

// TestPerTenantNoActorPasses: the per-tenant guard ignores unauthenticated
// requests (no actor on the context) — those are out of scope (login is
// throttled separately). With no limiter wired it is also a plain no-op.
func TestPerTenantNoActorPasses(t *testing.T) {
	// limiter==nil disables the guard, so this must pass regardless of limit.
	s := newRateLimitServer(newRateLimiter(nil, 600, 0, 0))
	h := s.rateLimitPerTenant(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("disabled per-tenant guard must pass: got %d", rec.Code)
	}
}

// TestPwResetBucketKey pins the per-IP password-reset bucket-key derivation
// (SR-L3): namespaced and keyed strictly by client IP, so two different IPs get
// independent budgets.
func TestPwResetBucketKey(t *testing.T) {
	if got := pwResetBucketKey("203.0.113.7"); got != "pwreset:ip:203.0.113.7" {
		t.Fatalf("unexpected pw-reset bucket key: %q", got)
	}
	if pwResetBucketKey("203.0.113.7") == pwResetBucketKey("203.0.113.8") {
		t.Fatalf("different IPs must get different pw-reset bucket keys")
	}
}

// TestPwResetEnabledPredicate documents the enable/disable matrix for the
// per-IP password-reset guard so a future edit can't silently flip it on for
// the integration harness (which leaves the limit at its zero value).
func TestPwResetEnabledPredicate(t *testing.T) {
	if newRateLimiter(nil, 0, 0, 0).withPasswordReset(nil, 10, time.Minute).pwResetEnabled() {
		t.Fatalf("pw-reset guard must be off when no limiter (Redis) is wired")
	}
	if newRateLimiter(nil, 0, 0, 0).withPasswordReset(&ratelimit.Limiter{}, 0, time.Minute).pwResetEnabled() {
		t.Fatalf("pw-reset guard must be off when the limit is non-positive")
	}
	if !newRateLimiter(nil, 0, 0, 0).withPasswordReset(&ratelimit.Limiter{}, 10, time.Minute).pwResetEnabled() {
		t.Fatalf("pw-reset guard must be on when a limiter is wired and limit > 0")
	}
}

// TestPwResetDisabledPasses: a disabled per-IP guard is a no-op — every request
// passes regardless of how many arrive. This is the default the integration
// harness relies on.
func TestPwResetDisabledPasses(t *testing.T) {
	s := newRateLimitServer(newRateLimiter(nil, 0, 0, 0).withPasswordReset(nil, 10, time.Minute))
	h := s.rateLimitPasswordResetIP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 100; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/password-reset/confirm", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: disabled pw-reset guard must pass, got %d", i, rec.Code)
		}
	}
}

// TestPerTenantEnabledPredicate documents the enable/disable matrix the chain
// relies on so future edits don't silently flip a guard on for the test harness.
func TestPerTenantEnabledPredicate(t *testing.T) {
	if newRateLimiter(nil, 600, 0, 0).perTenantEnabled() {
		t.Fatalf("per-tenant guard must be off when no limiter (Redis) is wired")
	}
	if newRateLimiter(nil, 0, 0, 0).inflightEnabled() {
		t.Fatalf("in-flight cap must be off when max is non-positive")
	}
	if !newRateLimiter(nil, 0, 0, 10).inflightEnabled() {
		t.Fatalf("in-flight cap must be on when max is positive")
	}
}
