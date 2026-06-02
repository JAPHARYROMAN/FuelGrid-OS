package observability

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSetupTracing_NoopWhenUnconfigured verifies the safe default: an empty
// or "none" exporter installs a no-op provider, returns a usable (non-nil)
// shutdown func, and never errors — boot must not depend on telemetry.
func TestSetupTracing_NoopWhenUnconfigured(t *testing.T) {
	for _, exporter := range []string{"", "none"} {
		t.Run("exporter="+exporter, func(t *testing.T) {
			shutdown, err := SetupTracing(context.Background(), TracingConfig{
				Exporter:    exporter,
				ServiceName: "test",
				Version:     "0.0.0",
				Environment: "test",
			}, quietLogger())
			if err != nil {
				t.Fatalf("expected no error for exporter=%q, got %v", exporter, err)
			}
			if shutdown == nil {
				t.Fatalf("expected non-nil shutdown func for exporter=%q", exporter)
			}
			if err := shutdown(context.Background()); err != nil {
				t.Fatalf("shutdown returned error: %v", err)
			}
		})
	}
}

// TestSetupTracing_OTLPRequiresEndpoint asserts the fail-stop contract: when
// the operator selects the otlp exporter but leaves the endpoint empty, setup
// returns an error (which main.go escalates to a fatal boot failure) rather
// than silently dialing a default.
func TestSetupTracing_OTLPRequiresEndpoint(t *testing.T) {
	shutdown, err := SetupTracing(context.Background(), TracingConfig{
		Exporter:    "otlp",
		ServiceName: "test",
		Version:     "0.0.0",
		Environment: "test",
		Endpoint:    "",
	}, quietLogger())
	if err == nil {
		t.Fatal("expected error for otlp exporter with empty endpoint, got nil")
	}
	if shutdown != nil {
		t.Fatal("expected nil shutdown on error, got non-nil")
	}
}

// TestSetupTracing_OTLPBogusEndpoint asserts that a configured-but-broken
// endpoint fails at setup. An endpoint that cannot resolve must surface an
// error inside the dial timeout so the caller can fail-stop at boot.
func TestSetupTracing_OTLPBogusEndpoint(t *testing.T) {
	shutdown, err := SetupTracing(context.Background(), TracingConfig{
		Exporter:    "otlp",
		ServiceName: "test",
		Version:     "0.0.0",
		Environment: "test",
		// A syntactically broken endpoint (no host after the scheme) is
		// rejected synchronously, independent of any network connectivity in
		// the test environment.
		Endpoint: "http://",
	}, quietLogger())
	if err == nil {
		t.Fatal("expected error for otlp exporter with bogus endpoint, got nil")
	}
	if shutdown != nil {
		t.Fatal("expected nil shutdown on error, got non-nil")
	}
}

// TestSetupTracing_UnknownExporter rejects an exporter name the switch does
// not recognise rather than silently degrading.
func TestSetupTracing_UnknownExporter(t *testing.T) {
	_, err := SetupTracing(context.Background(), TracingConfig{
		Exporter: "carrier-pigeon",
	}, quietLogger())
	if err == nil {
		t.Fatal("expected error for unknown exporter, got nil")
	}
}
