package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
)

func TestHealthAndReady(t *testing.T) {
	t.Parallel()

	srv := newTestServer()

	tests := []struct {
		path       string
		wantStatus string
	}{
		{"/healthz", "ok"},
		{"/readyz", "ready"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, http.NoBody)
			rec := httptest.NewRecorder()

			srv.http.Handler.ServeHTTP(rec, req)

			res := rec.Result()
			defer func() { _ = res.Body.Close() }()

			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", res.StatusCode)
			}

			var body map[string]any
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got, _ := body["status"].(string); got != tc.wantStatus {
				t.Errorf("status field = %q, want %q", got, tc.wantStatus)
			}
			if got := res.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
		})
	}
}

func TestRequestIDHeader(t *testing.T) {
	t.Parallel()

	srv := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, req)

	// chi's RequestID middleware should populate the response header.
	res := rec.Result()
	defer func() { _ = res.Body.Close() }()

	if got := res.Header.Get("X-Request-Id"); got == "" {
		t.Error("expected X-Request-Id response header to be set by middleware")
	}
}

// TestRecovererWrapsWholeChain asserts that the production middleware
// ordering places chimiddleware.Recoverer outermost enough that a panic in a
// *downstream middleware* — not merely a route handler — is converted into a
// clean 500 and the response still completes. Before arch-05 the logger and
// metrics middleware ran ahead of Recoverer, so a panic there escaped the
// recovery and tore down the connection.
//
// The chain is reconstructed here in the exact same order as server.New so
// the test is DB-free yet faithful to the production composition.
func TestRecovererWrapsWholeChain(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{logger: logger}

	cases := []struct {
		name string
		// panicAt registers a middleware that panics, positioned where the
		// logger/metrics middleware live (downstream of Recoverer in the new
		// order). nil means the panic comes from the final handler instead.
		panicAt func(next http.Handler) http.Handler
	}{
		{
			name: "panic in downstream middleware is recovered",
			panicAt: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					panic("boom from middleware")
				})
			},
		},
		{
			name:    "panic in handler is recovered",
			panicAt: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := chi.NewRouter()

			// Mirror server.New's r.Use ordering exactly.
			r.Use(chimiddleware.RequestID)
			r.Use(echoRequestID)
			r.Use(chimiddleware.Recoverer)
			if tc.panicAt != nil {
				r.Use(tc.panicAt)
			}
			r.Use(s.logRequests)
			r.Use(s.recordMetrics)
			r.Use(chimiddleware.Timeout(30 * time.Second))
			r.Use(limitRequestBody)

			r.Get("/boom", func(http.ResponseWriter, *http.Request) {
				if tc.panicAt == nil {
					panic("boom from handler")
				}
			})

			req := httptest.NewRequest(http.MethodGet, "/boom", http.NoBody)
			rec := httptest.NewRecorder()

			// If recovery failed the panic would propagate out of ServeHTTP
			// and fail the test; reaching the assertions means it completed.
			r.ServeHTTP(rec, req)

			res := rec.Result()
			defer func() { _ = res.Body.Close() }()

			if res.StatusCode != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", res.StatusCode)
			}
			// X-Request-Id is set before Recoverer, so even a recovered
			// request stays correlatable.
			if got := res.Header.Get("X-Request-Id"); got == "" {
				t.Error("expected X-Request-Id to be set on a recovered response")
			}
		})
	}
}

func newTestServer() *Server {
	cfg := config.Config{
		Host: "127.0.0.1",
		Port: 0,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, logger, Deps{})
}
