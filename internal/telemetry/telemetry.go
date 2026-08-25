package telemetry

import (
	"context"
	"os"
	"time"

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

// OTLP metric export cadence and per-instrument cardinality cap (see sdkmetric.WithCardinalityLimit).
const (
	metricExportInterval   = 5 * time.Minute
	metricCardinalityLimit = 1000
)

type Telemetry struct {
	Logger         *zap.Logger
	Metrics        *Metrics
	Tracer         trace.Tracer
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
}

// Init initializes OpenTelemetry tracing + metrics with OTLP over gRPC.
// Returns a telemetry object that must be shutdown on application exit.
// Config via env: OTEL_EXPORTER_OTLP_ENDPOINT (leave empty to disable)
func Init(ctx context.Context, logger *zap.Logger) (*Telemetry, error) {
	t := &Telemetry{
		Logger: logger,
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		logger.Info("OTel disabled (OTEL_EXPORTER_OTLP_ENDPOINT not set)")
		t.Metrics = NewMetrics(logger)
		t.Tracer = otel.Tracer(AppName)
		return t, nil
	}

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "chat-api"
	}

	// Adding hostname. IMPORTANT: This is used to identify the instance in time series data.
	// Without it, different instances of the same service will collide during collection.
	host, err := os.Hostname()
	if err != nil {
		logger.Error("Failed to get hostname", zap.Error(err))
		// fallback to a random uuid string
		host = uuid.NewString()
	}
	// Shared resource: service.name label applied to all signals
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		attribute.String("hostname", host),
	)

	// --- Metrics ---
	metricExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(endpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp,
			sdkmetric.WithInterval(metricExportInterval))),
		sdkmetric.WithResource(res),
		sdkmetric.WithCardinalityLimit(metricCardinalityLimit),
	)
	otel.SetMeterProvider(mp)
	// --- Traces ---
	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger.Info("OTel tracing + metrics initialized",
		zap.String("endpoint", endpoint),
		zap.String("service", serviceName),
		zap.Duration("metric_export_interval", metricExportInterval),
		zap.Int("metric_cardinality_limit", metricCardinalityLimit),
	)

	t.tracerProvider = tp
	t.meterProvider = mp
	t.Metrics = NewMetrics(logger)
	t.Tracer = otel.Tracer(AppName)

	return t, nil
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
	return firstErr
}
