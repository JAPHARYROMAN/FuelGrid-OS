package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// recordingTransport is a synchronous Sentry transport that captures events in
// memory for assertions (the SDK calls SendEvent inline for a custom transport).
type recordingTransport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (t *recordingTransport) Configure(sentry.ClientOptions) {}
func (t *recordingTransport) SendEvent(e *sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, e)
}
func (t *recordingTransport) Flush(time.Duration) bool              { return true }
func (t *recordingTransport) FlushWithContext(context.Context) bool { return true }
func (t *recordingTransport) Close()                                {}
func (t *recordingTransport) snapshot() []*sentry.Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*sentry.Event(nil), t.events...)
}

// TestCaptureErrorsToSentry covers OBS-3: a panic is captured as an exception
// (and still becomes a 500 via Recoverer), a 5xx response is captured as a
// message, every event carries the request_id tag, and a 4xx is NOT captured.
func TestCaptureErrorsToSentry(t *testing.T) {
	rt := &recordingTransport{}
	if err := sentry.Init(sentry.ClientOptions{Dsn: "https://test@sentry.invalid/1", Transport: rt}); err != nil {
		t.Fatalf("sentry init: %v", err)
	}
	defer func() { _ = sentry.Init(sentry.ClientOptions{}) }() // disable after the test

	s := &Server{}
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Recoverer)
	r.Use(s.captureErrors)
	r.Get("/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })
	r.Get("/fail", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	r.Get("/bad", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic should still 500 via Recoverer, got %d", rec.Code)
	}
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/fail", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/bad", nil))

	sentry.Flush(2 * time.Second)
	evts := rt.snapshot()
	if len(evts) != 2 {
		t.Fatalf("expected 2 captured events (panic + 5xx), got %d", len(evts))
	}
	for _, e := range evts {
		if e.Tags["request_id"] == "" {
			t.Fatalf("captured event missing request_id tag: tags=%v", e.Tags)
		}
	}
}
