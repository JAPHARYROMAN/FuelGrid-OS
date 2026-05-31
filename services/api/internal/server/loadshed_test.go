package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestShedWhenUnhealthy exercises the readiness-aware load-shedding middleware
// (REL-5) in isolation, with no database: it must pass requests through while
// the cached health flag is true, shed with 503 + Retry-After once the flag is
// flipped false, and always bypass the operational probe paths regardless of
// the flag.
func TestShedWhenUnhealthy(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	const okBody = "downstream-served"
	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, okBody)
	})

	newReq := func(path string) *httptest.ResponseRecorder {
		s := &Server{logger: logger}
		s.dbHealthy.Store(true) // mirror New()'s default-healthy init
		return serveThrough(s, downstream, path)
	}

	t.Run("healthy passes through", func(t *testing.T) {
		t.Parallel()
		rec := newReq("/api/v1/stations")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Body.String() != okBody {
			t.Errorf("body = %q, want %q", rec.Body.String(), okBody)
		}
	})

	t.Run("unhealthy sheds 503 with Retry-After", func(t *testing.T) {
		t.Parallel()
		s := &Server{logger: logger}
		s.dbHealthy.Store(false)
		rec := serveThrough(s, downstream, "/api/v1/stations")

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		if got := rec.Header().Get("Retry-After"); got == "" {
			t.Error("expected Retry-After header on a shed response")
		}
		if rec.Body.String() == okBody {
			t.Error("downstream handler must not run when shedding")
		}
	})

	t.Run("probe paths bypass shedding even when unhealthy", func(t *testing.T) {
		t.Parallel()
		for _, p := range []string{"/healthz", "/readyz", "/metrics"} {
			s := &Server{logger: logger}
			s.dbHealthy.Store(false)
			rec := serveThrough(s, downstream, p)
			if rec.Code != http.StatusOK {
				t.Errorf("%s: status = %d, want 200 (probe must bypass shedding)", p, rec.Code)
			}
			if rec.Body.String() != okBody {
				t.Errorf("%s: probe did not reach downstream", p)
			}
		}
	})
}

// serveThrough runs a request for path through the shedWhenUnhealthy middleware
// wrapping next, and returns the recorded response.
func serveThrough(s *Server, next http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	rec := httptest.NewRecorder()
	s.shedWhenUnhealthy(next).ServeHTTP(rec, req)
	return rec
}

// TestStartHealthcheckNoDBNeverSheds asserts the no-DB safety property: with
// deps.DB nil the checker never starts, the flag stays healthy, and shutdown is
// a clean no-op (no goroutine to stop).
func TestStartHealthcheckNoDBNeverSheds(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{logger: logger}
	s.dbHealthy.Store(true)

	s.startHealthcheck() // deps.DB is nil -> no-op
	if s.stopHealthcheck != nil || s.healthcheckDone != nil {
		t.Fatal("checker must not start when deps.DB is nil")
	}
	if !s.dbHealthy.Load() {
		t.Fatal("flag must stay healthy when no checker runs")
	}
	// stopHealthcheckChecker must be safe with nothing started.
	s.stopHealthcheckChecker()
}
