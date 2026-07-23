// Package telemetry wires OpenTelemetry traces and metrics to an OTLP/HTTP
// endpoint — in this deployment the Elastic APM Server, which stores them in
// Elasticsearch for the Kibana dashboards.
//
// Metrics are not re-instrumented: the Prometheus bridge forwards everything
// already registered in internal/metrics (plus Go runtime collectors) over
// OTLP, so the /metrics endpoint and the OTLP export stay in lockstep.
//
// Configuration is the standard OTEL_EXPORTER_OTLP_* environment variables
// plus ELASTIC_APM_SECRET_TOKEN for the APM server's bearer-token auth.
// When OTEL_EXPORTER_OTLP_ENDPOINT is unset the whole subsystem is disabled
// and the app runs exactly as before (local development, tests, CI).
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	prombridge "go.opentelemetry.io/contrib/bridges/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	// Must match the schema version of resource.Default() for the current
	// SDK, or resource.Merge fails with a schema-URL conflict.
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// Setup initialises the global tracer and meter providers. The returned
// shutdown function flushes buffered telemetry; call it before exit.
func Setup(ctx context.Context, serviceName, version string) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		slog.Info("telemetry disabled: OTEL_EXPORTER_OTLP_ENDPOINT not set")
		return func(context.Context) error { return nil }, nil
	}

	// The Elastic APM Server authenticates OTLP requests with a bearer
	// secret token rather than per-signal API keys.
	var headers map[string]string
	if token := os.Getenv("ELASTIC_APM_SECRET_TOKEN"); token != "" {
		headers = map[string]string{"Authorization": "Bearer " + token}
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(version),
	))
	if err != nil {
		return nil, err
	}

	traceExp, err := otlptracehttp.New(ctx, otlptracehttp.WithHeaders(headers))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))

	metricExp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithHeaders(headers))
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp,
			sdkmetric.WithInterval(30*time.Second),
			sdkmetric.WithProducer(prombridge.NewMetricProducer()),
		)),
	)
	otel.SetMeterProvider(mp)

	slog.Info("telemetry enabled", "otlp_endpoint", endpoint)
	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}
