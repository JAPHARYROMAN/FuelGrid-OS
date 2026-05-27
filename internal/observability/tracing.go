package observability

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// TracingConfig controls which exporter the tracer provider uses.
//
// Exporter values:
//
//	"none"   — tracing disabled; spans are created but discarded
//	"stdout" — write spans as JSON to stderr (dev / CI default)
//	"otlp"   — placeholder for the OTLP gRPC exporter (lands when we
//	            stand up a real collector; not wired in this stage)
type TracingConfig struct {
	Exporter    string
	ServiceName string
	Version     string
	Environment string
}

// SetupTracing configures the global tracer provider and propagator.
// Returns a shutdown function the caller must invoke on process exit.
func SetupTracing(ctx context.Context, cfg TracingConfig, logger *slog.Logger) (func(context.Context) error, error) {
	// NewSchemaless on purpose — resource.Default() already carries a
	// SchemaURL, and Merge() refuses to combine two different schema
	// versions. We let the SDK keep its own schema and just add the
	// service attributes that everyone needs.
	custom := resource.NewSchemaless(
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.Version),
		semconv.DeploymentEnvironment(cfg.Environment),
	)
	res, err := resource.Merge(resource.Default(), custom)
	if err != nil {
		return nil, fmt.Errorf("tracing: build resource: %w", err)
	}

	var exporter sdktrace.SpanExporter
	switch cfg.Exporter {
	case "", "none":
		// Set a no-op tracer provider so callers can blindly use
		// otel.Tracer(...) without nil checks.
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		logger.Info("tracing disabled (exporter=none)")
		return func(context.Context) error { return nil }, nil
	case "stdout":
		exporter, err = stdouttrace.New(stdouttrace.WithWriter(os.Stderr), stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("tracing: stdout exporter: %w", err)
		}
	default:
		return nil, fmt.Errorf("tracing: unknown exporter %q", cfg.Exporter)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger.Info("tracing initialized", "exporter", cfg.Exporter)
	return tp.Shutdown, nil
}
