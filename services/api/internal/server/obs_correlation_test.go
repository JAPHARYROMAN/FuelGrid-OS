package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// captureHandler is a minimal slog.Handler that records the attributes of the
// last record so tests can assert on individual log fields.
type captureHandler struct {
	mu    sync.Mutex
	attrs map[string]string
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *captureHandler) WithGroup(string) slog.Handler            { return h }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.attrs = map[string]string{}
	r.Attrs(func(a slog.Attr) bool {
		h.attrs[a.Key] = a.Value.String()
		return true
	})
	return nil
}
func (h *captureHandler) get(key string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attrs[key]
}

// newCorrelationRouter wires the minimal middleware chain exercised by these
// tests: RequestID feeds correlation_id's fallback, traceRequests opens the
// span, and logRequests stamps the correlation_id we assert on.
func newCorrelationRouter(h *captureHandler) (*Server, chi.Router) {
	s := &Server{logger: slog.New(h)}
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(s.traceRequests)
	r.Use(s.logRequests)
	r.Get("/api/v1/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return s, r
}

// TestCorrelationIDFallsBackToRequestID covers OBS-5 with tracing disabled: the
// global provider is the OTel no-op tracer, so SpanContext is invalid and
// correlation_id mirrors request_id. This is the default production path when
// OtelExporter=none.
func TestCorrelationIDFallsBackToRequestID(t *testing.T) {
	// Mirror OtelExporter=none: SetupTracing installs the no-op provider in
	// that mode. Set it explicitly so the assertion is independent of any
	// global provider a sibling test may have installed.
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	h := &captureHandler{}
	_, r := newCorrelationRouter(h)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))

	reqID := h.get("request_id")
	if reqID == "" {
		t.Fatal("request_id was not logged")
	}
	if got := h.get("correlation_id"); got != reqID {
		t.Fatalf("correlation_id = %q, want request_id %q (no-span fallback)", got, reqID)
	}
}

// TestCorrelationIDEqualsTraceID covers OBS-5 with tracing enabled: a real
// TracerProvider is installed for the request, so traceRequests opens a
// recording span and correlation_id is the active TraceID — distinct from the
// request_id — letting logs and traces join on the same id.
func TestCorrelationIDEqualsTraceID(t *testing.T) {
	// otelhttp resolves its tracer from the global provider, so install a real
	// SDK provider for the duration of the test and restore the prior one
	// (the package default no-op) afterward so sibling tests stay unaffected.
	prev := otel.GetTracerProvider()
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	h := &captureHandler{}
	s := &Server{logger: slog.New(h)}
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(s.traceRequests)
	r.Use(s.logRequests)

	var spanTraceID string
	r.Get("/api/v1/ping", func(w http.ResponseWriter, req *http.Request) {
		spanTraceID = trace.SpanContextFromContext(req.Context()).TraceID().String()
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))

	if spanTraceID == "" || spanTraceID == "00000000000000000000000000000000" {
		t.Fatalf("expected a valid recording span TraceID, got %q", spanTraceID)
	}
	corr := h.get("correlation_id")
	if corr != spanTraceID {
		t.Fatalf("correlation_id = %q, want active TraceID %q", corr, spanTraceID)
	}
	if corr == h.get("request_id") {
		t.Fatal("correlation_id should be the TraceID, not the request_id, when a span is recording")
	}
}
