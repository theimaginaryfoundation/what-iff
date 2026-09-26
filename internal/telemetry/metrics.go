package telemetry

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

const AppName = "chat-api"

// Metrics records the metrics declared in catalog.go. Every method is safe on a nil *Metrics
// (it does nothing), so code holding an optional *Metrics needs no nil checks.
//
// Components that don't have a *Metrics handed to them use Global(), which records through the
// process-wide meter provider set up by Init.
type Metrics struct {
	logger      *zap.Logger
	meter       metric.Meter
	instruments sync.Map // name -> instrument
}

// NewMetrics returns a Metrics on the global meter provider. Instruments created before Init
// installs the real provider are forwarded to it once it does.
func NewMetrics(logger *zap.Logger) *Metrics {
	return NewMetricsWithMeter(logger, otel.Meter(AppName))
}

// NewMetricsWithMeter returns a Metrics on a specific meter (tests use a manual reader).
func NewMetricsWithMeter(logger *zap.Logger, meter metric.Meter) *Metrics {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Metrics{logger: logger, meter: meter}
}

var global atomic.Pointer[Metrics]

// Global returns the process-wide Metrics. Init replaces it with one that has the real logger;
// before that (and in tests that don't set one) it records to the global meter provider.
func Global() *Metrics {
	if m := global.Load(); m != nil {
		return m
	}
	global.CompareAndSwap(nil, NewMetrics(nil))
	return global.Load()
}

// SetGlobal replaces the process-wide Metrics and returns the previous one. Tests use
// telemetrytest.UseGlobal, which restores it afterwards.
func SetGlobal(m *Metrics) *Metrics {
	return global.Swap(m)
}

// Record adds one value to a histogram.
func (m *Metrics) Record(ctx context.Context, h Histogram, value float64, attrs ...attribute.KeyValue) {
	if m == nil {
		return
	}
	inst, ok := m.histogram(h)
	if !ok {
		return
	}
	inst.Record(ctx, value, metric.WithAttributes(attrs...))
}

// RecordDuration adds a duration, in seconds, to a histogram.
func (m *Metrics) RecordDuration(ctx context.Context, h Histogram, d time.Duration, attrs ...attribute.KeyValue) {
	m.Record(ctx, h, d.Seconds(), attrs...)
}

// Time starts timing and returns a function that records the elapsed time on h, adding
// error.type when the error passed to it is non-nil:
//
//	done := metrics.Time(ctx, telemetry.DependencyDuration, attrs...)
//	err := call()
//	done(err)
func (m *Metrics) Time(ctx context.Context, h Histogram, attrs ...attribute.KeyValue) func(err error) {
	if m == nil {
		return func(error) {}
	}
	start := time.Now()
	return func(err error) {
		all := append(append(make([]attribute.KeyValue, 0, len(attrs)+1), attrs...), ErrorAttrs(err)...)
		m.RecordDuration(ctx, h, time.Since(start), all...)
	}
}

// TimeDependency times one logical call to an external dependency on DependencyDuration.
func (m *Metrics) TimeDependency(ctx context.Context, dependency, operation string) func(err error) {
	return m.Time(ctx, DependencyDuration, AttrDependency.String(dependency), AttrOperation.String(operation))
}

// Add increments a counter.
func (m *Metrics) Add(ctx context.Context, c Counter, n int64, attrs ...attribute.KeyValue) {
	if m == nil || n == 0 {
		return
	}
	inst, ok := m.counter(c)
	if !ok {
		return
	}
	inst.Add(ctx, n, metric.WithAttributes(attrs...))
}

// AddUpDown moves an up-down counter (use +1/-1 around work in flight).
func (m *Metrics) AddUpDown(ctx context.Context, u UpDownCounter, n int64, attrs ...attribute.KeyValue) {
	if m == nil || n == 0 {
		return
	}
	inst, ok := m.upDownCounter(u)
	if !ok {
		return
	}
	inst.Add(ctx, n, metric.WithAttributes(attrs...))
}

// GaugeObserver reports one gauge value from inside a RegisterGauges callback.
type GaugeObserver func(g Gauge, value float64, attrs ...attribute.KeyValue)

// RegisterGauges samples gauges at each export by calling observe. Keep the callback cheap: it
// runs once per export interval. Returns a function that unregisters the callback.
func (m *Metrics) RegisterGauges(gauges []Gauge, observe func(ctx context.Context, report GaugeObserver) error) (func() error, error) {
	if m == nil || len(gauges) == 0 {
		return func() error { return nil }, nil
	}
	byName := make(map[string]metric.Float64ObservableGauge, len(gauges))
	observables := make([]metric.Observable, 0, len(gauges))
	for _, g := range gauges {
		inst, err := m.meter.Float64ObservableGauge(g.Name, metric.WithUnit(g.Unit), metric.WithDescription(g.Description))
		if err != nil {
			return nil, err
		}
		byName[g.Name] = inst
		observables = append(observables, inst)
	}
	reg, err := m.meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		return observe(ctx, func(g Gauge, value float64, attrs ...attribute.KeyValue) {
			if inst, ok := byName[g.Name]; ok {
				o.ObserveFloat64(inst, value, metric.WithAttributes(attrs...))
			}
		})
	}, observables...)
	if err != nil {
		return nil, err
	}
	return reg.Unregister, nil
}

func (m *Metrics) histogram(h Histogram) (metric.Float64Histogram, bool) {
	if v, ok := m.instruments.Load(h.Name); ok {
		inst, ok := v.(metric.Float64Histogram)
		m.warnIfMismatched(h.Name, ok)
		return inst, ok
	}
	opts := []metric.Float64HistogramOption{metric.WithUnit(h.Unit), metric.WithDescription(h.Description)}
	if len(h.Buckets) > 0 {
		opts = append(opts, metric.WithExplicitBucketBoundaries(h.Buckets...))
	}
	inst, err := m.meter.Float64Histogram(h.Name, opts...)
	return storeInstrument(m, h.Name, inst, err)
}

func (m *Metrics) counter(c Counter) (metric.Int64Counter, bool) {
	if v, ok := m.instruments.Load(c.Name); ok {
		inst, ok := v.(metric.Int64Counter)
		m.warnIfMismatched(c.Name, ok)
		return inst, ok
	}
	inst, err := m.meter.Int64Counter(c.Name, metric.WithUnit(c.Unit), metric.WithDescription(c.Description))
	return storeInstrument(m, c.Name, inst, err)
}

func (m *Metrics) upDownCounter(u UpDownCounter) (metric.Int64UpDownCounter, bool) {
	if v, ok := m.instruments.Load(u.Name); ok {
		inst, ok := v.(metric.Int64UpDownCounter)
		m.warnIfMismatched(u.Name, ok)
		return inst, ok
	}
	inst, err := m.meter.Int64UpDownCounter(u.Name, metric.WithUnit(u.Unit), metric.WithDescription(u.Description))
	return storeInstrument(m, u.Name, inst, err)
}

// storeInstrument caches a newly created instrument; if another goroutine won the race, its
// instrument is used instead.
func storeInstrument[T any](m *Metrics, name string, inst T, err error) (T, bool) {
	var zero T
	if err != nil {
		m.logger.Warn("failed to create metric instrument", zap.String("metric_name", name), zap.Error(err))
		return zero, false
	}
	actual, _ := m.instruments.LoadOrStore(name, inst)
	typed, ok := actual.(T)
	m.warnIfMismatched(name, ok)
	return typed, ok
}

func (m *Metrics) warnIfMismatched(name string, ok bool) {
	if !ok {
		m.logger.Warn("metric name reused with a different instrument kind", zap.String("metric_name", name))
	}
}
