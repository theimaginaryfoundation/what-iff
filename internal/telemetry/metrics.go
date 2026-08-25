package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// TODO: this is a bit 'free for all' on metric names at the moment, we should consider a more structured approach.
const (
	AppName = "chat-api"
	// standard token metric name
	Tokens         = "token_count"
	StartupCounter = "app_startup_total"
	// ChatMessageContextItemsPersistFailures counts failed bulk inserts of chat_message_context_items (create/update).
	ChatMessageContextItemsPersistFailures = "chat_message_context_items_persist_failures_total"
	// FileAttachmentUploadTotal counts file attachment upload attempts.
	// Attributes: file_type (MIME type), status ("success" | "failure").
	FileAttachmentUploadTotal = "file_attachment_upload_total"
)

type Metrics struct {
	logger          *zap.Logger
	meter           metric.Meter
	msHistograms    sync.Map
	countHistograms sync.Map
	counters        sync.Map
}

func NewMetrics(logger *zap.Logger) *Metrics {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Metrics{
		logger: logger,
		meter:  otel.Meter(AppName),
	}
}

func (m *Metrics) RecordTime(ctx context.Context, name string, duration time.Duration, attributes ...metric.RecordOption) {
	h, err := m.getOrCreateHistogramMs(name)
	if err != nil {
		m.logger.Warn("failed to get/create duration histogram", zap.String("metric_name", name), zap.Error(err))
		return
	}
	h.Record(ctx, duration.Milliseconds(), attributes...)
}

func (m *Metrics) RecordCounter(ctx context.Context, name string, count int64, attributes ...metric.AddOption) {
	c, err := m.getOrCreateCounter(name)
	if err != nil {
		m.logger.Warn("failed to get/create counter", zap.String("metric_name", name), zap.Error(err))
		return
	}
	c.Add(ctx, count, attributes...)
}

func (m *Metrics) RecordCountHistogram(ctx context.Context, name string, count int64, attributes ...metric.RecordOption) {
	h, err := m.getOrCreateHistogramCount(name)
	if err != nil {
		m.logger.Warn("failed to get/create count histogram", zap.String("metric_name", name), zap.Error(err))
		return
	}
	h.Record(ctx, count, attributes...)
}

// InitStartupMetrics eagerly initializes startup telemetry and records a boot event.
// Call this once during application startup and fail fast if it returns an error.
func (m *Metrics) InitStartupMetrics(ctx context.Context) error {
	counter, err := m.getOrCreateCounter(StartupCounter)
	if err != nil {
		return err
	}
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("phase", "init")))
	return nil
}

func InputTokenAttr() attribute.KeyValue {
	return attribute.String("token_io", "input")

}

func OutputTokenAttr() attribute.KeyValue {
	return attribute.String("token_io", "output")
}

// RecordSegmentTokenEstimates records estimated input tokens per ModelContext segment kind
// (token_basis=segment_estimate, token_io=input).
func (m *Metrics) RecordSegmentTokenEstimates(ctx context.Context, estimates map[string]int64, callPath CallPath) {
	if m == nil || len(estimates) == 0 {
		return
	}
	cp := string(callPath)
	if cp == "" {
		cp = string(CallPathUnknown)
	}
	for segment, n := range estimates {
		if n <= 0 {
			continue
		}
		m.RecordCountHistogram(ctx, Tokens, n, metric.WithAttributes(
			InputTokenAttr(),
			attribute.String("token_basis", "segment_estimate"),
			attribute.String("segment", segment),
			attribute.String("call_path", cp),
		))
	}
}

func (m *Metrics) newHistogramMs(name string, description string) (metric.Int64Histogram, error) {
	return m.meter.Int64Histogram(name, metric.WithDescription(description), metric.WithUnit("ms"))
}

func (m *Metrics) newHistogramCount(name string, description string) (metric.Int64Histogram, error) {
	return m.meter.Int64Histogram(name, metric.WithDescription(description), metric.WithUnit("count"))
}

func (m *Metrics) newCounter(name string, description string) (metric.Int64Counter, error) {
	return m.meter.Int64Counter(name, metric.WithDescription(description), metric.WithUnit("count"))
}

func (m *Metrics) getOrCreateHistogramMs(name string) (metric.Int64Histogram, error) {
	if existing, ok := m.msHistograms.Load(name); ok {
		h, ok := existing.(metric.Int64Histogram)
		if !ok {
			return nil, fmt.Errorf("metric %q has unexpected histogram type", name)
		}
		return h, nil
	}

	created, err := m.newHistogramMs(name, "Duration of "+name)
	if err != nil {
		return nil, err
	}

	actual, loaded := m.msHistograms.LoadOrStore(name, created)
	if !loaded {
		return created, nil
	}
	h, ok := actual.(metric.Int64Histogram)
	if !ok {
		return nil, fmt.Errorf("metric %q has unexpected histogram type", name)
	}
	return h, nil
}

func (m *Metrics) getOrCreateHistogramCount(name string) (metric.Int64Histogram, error) {
	if existing, ok := m.countHistograms.Load(name); ok {
		h, ok := existing.(metric.Int64Histogram)
		if !ok {
			return nil, fmt.Errorf("metric %q has unexpected count histogram type", name)
		}
		return h, nil
	}

	created, err := m.newHistogramCount(name, "Count of "+name)
	if err != nil {
		return nil, err
	}

	actual, loaded := m.countHistograms.LoadOrStore(name, created)
	if !loaded {
		return created, nil
	}
	h, ok := actual.(metric.Int64Histogram)
	if !ok {
		return nil, fmt.Errorf("metric %q has unexpected count histogram type", name)
	}
	return h, nil
}

func (m *Metrics) getOrCreateCounter(name string) (metric.Int64Counter, error) {
	if existing, ok := m.counters.Load(name); ok {
		c, ok := existing.(metric.Int64Counter)
		if !ok {
			return nil, fmt.Errorf("metric %q has unexpected counter type", name)
		}
		return c, nil
	}

	created, err := m.newCounter(name, "Count of "+name)
	if err != nil {
		return nil, err
	}

	actual, loaded := m.counters.LoadOrStore(name, created)
	if !loaded {
		return created, nil
	}
	c, ok := actual.(metric.Int64Counter)
	if !ok {
		return nil, fmt.Errorf("metric %q has unexpected counter type", name)
	}
	return c, nil
}
