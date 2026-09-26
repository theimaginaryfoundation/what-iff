package telemetry

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Metric export defaults. The 5-minute interval is a cost decision (samples ingested scale with
// export frequency); OTEL_METRIC_EXPORT_INTERVAL overrides it, e.g. for local debugging.
const (
	defaultMetricExportInterval = 5 * time.Minute
	metricCardinalityLimit      = 1000
	defaultPrometheusHost       = "localhost"
	defaultPrometheusPort       = "9464"
)

// Metrics exporters selected by OTEL_METRICS_EXPORTER.
const (
	exporterOTLP       = "otlp"
	exporterPrometheus = "prometheus"
	exporterConsole    = "console"
	exporterNone       = "none"
)

type Telemetry struct {
	Logger         *zap.Logger
	Metrics        *Metrics
	Tracer         trace.Tracer
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	metricsServer  *http.Server
}

// Init sets up OpenTelemetry metrics and tracing from the standard OTEL_* environment variables
// and returns a Telemetry that must be shut down on exit.
//
//   - OTEL_METRICS_EXPORTER: otlp (default when OTEL_EXPORTER_OTLP_ENDPOINT is set), prometheus
//     (serves /metrics on OTEL_EXPORTER_PROMETHEUS_HOST:PORT, default localhost:9464), console
//     (writes each export to stderr), or none (default otherwise).
//   - OTEL_METRIC_EXPORT_INTERVAL: export interval in milliseconds for otlp and console
//     (default 5 minutes).
//   - OTEL_EXPORTER_OTLP_ENDPOINT: OTLP gRPC endpoint; also enables trace export.
//   - OTEL_SERVICE_NAME: service.name resource attribute (default chat-api).
//
// With no exporter, metrics are recorded to a no-op provider, so instrumentation costs nothing.
func Init(ctx context.Context, logger *zap.Logger) (*Telemetry, error) {
	t := &Telemetry{Logger: logger}
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	exporter := metricsExporterFromEnv(endpoint)
	interval := metricExportIntervalFromEnv(logger)
	res := newResource(logger)

	reader, err := t.newMetricReader(ctx, exporter, endpoint, interval)
	if err != nil {
		return nil, err
	}
	if reader != nil {
		t.meterProvider = sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(reader),
			sdkmetric.WithResource(res),
			sdkmetric.WithCardinalityLimit(metricCardinalityLimit),
		)
		otel.SetMeterProvider(t.meterProvider)
	}

	if endpoint != "" {
		traceExp, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			return nil, err
		}
		t.tracerProvider = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExp),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(t.tracerProvider)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
	}

	logger.Info("OTel initialized",
		zap.String("metrics_exporter", exporter),
		zap.Duration("metric_export_interval", interval),
		zap.Bool("traces", t.tracerProvider != nil),
		zap.Int("metric_cardinality_limit", metricCardinalityLimit),
	)

	t.Metrics = NewMetrics(logger)
	SetGlobal(t.Metrics)
	t.Tracer = otel.Tracer(AppName)
	return t, nil
}

func metricsExporterFromEnv(endpoint string) string {
	exporter := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_METRICS_EXPORTER")))
	if exporter != "" {
		return exporter
	}
	if endpoint != "" {
		return exporterOTLP
	}
	return exporterNone
}

func metricExportIntervalFromEnv(logger *zap.Logger) time.Duration {
	raw := strings.TrimSpace(os.Getenv("OTEL_METRIC_EXPORT_INTERVAL"))
	if raw == "" {
		return defaultMetricExportInterval
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		logger.Warn("ignoring invalid OTEL_METRIC_EXPORT_INTERVAL (want milliseconds)", zap.String("value", raw))
		return defaultMetricExportInterval
	}
	return time.Duration(ms) * time.Millisecond
}

// newResource builds the resource attributes shared by all signals.
func newResource(logger *zap.Logger) *resource.Resource {
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = AppName
	}
	// The hostname identifies the instance in time series data. Without it, instances of the
	// same service collide during collection.
	host, err := os.Hostname()
	if err != nil {
		logger.Error("Failed to get hostname", zap.Error(err))
		host = uuid.NewString()
	}
	return resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		attribute.String("hostname", host),
	)
}

// newMetricReader returns the reader for the chosen exporter, or nil for none.
func (t *Telemetry) newMetricReader(ctx context.Context, exporter, endpoint string, interval time.Duration) (sdkmetric.Reader, error) {
	switch exporter {
	case exporterNone:
		return nil, nil
	case exporterOTLP:
		if endpoint == "" {
			t.Logger.Warn("OTEL_METRICS_EXPORTER=otlp but OTEL_EXPORTER_OTLP_ENDPOINT is empty; metrics disabled")
			return nil, nil
		}
		exp, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpoint(endpoint), otlpmetricgrpc.WithInsecure())
		if err != nil {
			return nil, err
		}
		return sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(interval)), nil
	case exporterConsole:
		exp, err := stdoutmetric.New(stdoutmetric.WithWriter(os.Stderr))
		if err != nil {
			return nil, err
		}
		return sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(interval)), nil
	case exporterPrometheus:
		return t.newPrometheusReader()
	default:
		t.Logger.Warn("unknown OTEL_METRICS_EXPORTER; metrics disabled", zap.String("value", exporter))
		return nil, nil
	}
}

// newPrometheusReader serves the current metrics at /metrics for local scraping or curl. The
// names match what the OTLP -> Prometheus remote-write path produces in deployed environments.
func (t *Telemetry) newPrometheusReader() (sdkmetric.Reader, error) {
	registry := prometheus.NewRegistry()
	exp, err := promexporter.New(promexporter.WithRegisterer(registry))
	if err != nil {
		return nil, err
	}
	host := envOr("OTEL_EXPORTER_PROMETHEUS_HOST", defaultPrometheusHost)
	port := envOr("OTEL_EXPORTER_PROMETHEUS_PORT", defaultPrometheusPort)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	t.metricsServer = &http.Server{Addr: net.JoinHostPort(host, port), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := t.metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Logger.Error("Prometheus metrics endpoint stopped", zap.Error(err))
		}
	}()
	t.Logger.Info("Serving Prometheus metrics", zap.String("url", "http://"+t.metricsServer.Addr+"/metrics"))
	return exp, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	var firstErr error
	if t.tracerProvider != nil {
		if err := t.tracerProvider.Shutdown(ctx); err != nil {
			firstErr = err
		}
	}
	if t.meterProvider != nil {
		if err := t.meterProvider.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if t.metricsServer != nil {
		if err := t.metricsServer.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
