package email

import (
	"context"
	"testing"
)

// New returns the console (no-op) driver when SMTP is unconfigured, so dev and
// CI never attempt a real send.
func TestNewFallsBackToConsoleWhenUnconfigured(t *testing.T) {
	t.Parallel()
	s := New(Config{}, nil)
	if got := s.Driver(); got != "console" {
		t.Fatalf("Driver() = %q, want console", got)
	}
	// Console send never errors.
	if err := s.Send(context.Background(), Message{To: "a@b.test", Subject: "hi"}); err != nil {
		t.Fatalf("console Send returned error: %v", err)
	}
}

// New returns the SMTP driver once a host is configured.
func TestNewUsesSMTPWhenHostSet(t *testing.T) {
	t.Parallel()
	s := New(Config{Host: "smtp.example.test", Port: 587}, nil)
	if got := s.Driver(); got != "smtp" {
		t.Fatalf("Driver() = %q, want smtp", got)
	}
}
