package observability

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
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
//	"otlp"   — OTLP/gRPC exporter shipping spans to a real collector
//	            (Tempo, Honeycomb, …) addressed by Endpoint
//
// When Exporter is "otlp" the Endpoint must be set and reachable enough to
// build an exporter; a configured-but-broken endpoint is a fatal boot error
// (see SetupTracing) rather than a silent degrade — telemetry that the
// operator explicitly asked for must not vanish unnoticed.
type TracingConfig struct {
	Exporter    string
	ServiceName string
	Version     string
	Environment string
	// Endpoint is the OTLP collector address (host:port, optionally with an
	// http:// or https:// scheme) used when Exporter == "otlp". A bare
	// host:port or an https:// URL uses TLS; an explicit http:// scheme
	// switches the exporter to plaintext (insecure) — handy for a local
	// collector or a sidecar on the same host.
	Endpoint string
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
	case "otlp":
		exporter, err = newOTLPExporter(ctx, cfg.Endpoint)
		if err != nil {
			// Fail-stop: the operator selected OTLP and pointed it
			// somewhere; if we can't build the exporter we must not
			// limp on with traces silently dropped. main.go treats a
			// non-nil error as fatal for the otlp exporter.
			return nil, fmt.Errorf("tracing: otlp exporter: %w", err)
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

// newOTLPExporter builds an OTLP/gRPC span exporter pointed at endpoint.
//
// endpoint accepts either a bare host:port (TLS, the secure default for a
// remote collector) or a URL with an explicit scheme: http:// forces a
// plaintext/insecure connection (local collector / same-host sidecar), while
// https:// keeps TLS. An empty endpoint is rejected so a misconfigured
// OTEL_EXPORTER=otlp without OTEL_EXPORTER_OTLP_ENDPOINT fails fast at boot
// rather than dialing the SDK's localhost default.
//
// The constructor dials lazily (WithDialOption does not block boot), but we
// pass a short context so a genuinely unresolvable endpoint surfaces an error
// at SetupTracing time, which the caller escalates to fatal.
func newOTLPExporter(ctx context.Context, endpoint string) (sdktrace.SpanExporter, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT must be set when OTEL_EXPORTER=otlp")
	}

	insecure := false
	switch {
	case strings.HasPrefix(endpoint, "http://"):
		insecure = true
		endpoint = strings.TrimPrefix(endpoint, "http://")
	case strings.HasPrefix(endpoint, "https://"):
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}
	// otlptracegrpc wants a host:port, not a URL with a trailing path.
	endpoint = strings.TrimSuffix(endpoint, "/")
	if endpoint == "" {
		return nil, fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT has no host after the scheme")
	}

	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
	if insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	exp, err := otlptrace.New(dialCtx, otlptracegrpc.NewClient(opts...))
	if err != nil {
		return nil, fmt.Errorf("dial %q: %w", endpoint, err)
	}
	return exp, nil
}
