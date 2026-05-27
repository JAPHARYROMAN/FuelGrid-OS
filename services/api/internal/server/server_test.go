package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

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

			var body map[string]string
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["status"] != tc.wantStatus {
				t.Errorf("status field = %q, want %q", body["status"], tc.wantStatus)
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

func newTestServer() *Server {
	cfg := config.Config{
		Host: "127.0.0.1",
		Port: 0,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, logger)
}
