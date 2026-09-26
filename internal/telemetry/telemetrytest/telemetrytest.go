// Package telemetrytest records metrics in memory so tests can assert what was emitted.
//
//	tm := telemetrytest.New(t)
//	code.Under(Test(tm.Metrics))
//	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.DependencyDuration.Name,
//		telemetry.AttrDependency.String("s3")))
//
// For code that records through telemetry.Global(), use telemetrytest.UseGlobal(t) instead.
package telemetrytest

import (
	"context"
	"testing"

	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// Recorder is a telemetry.Metrics backed by an in-memory reader.
type Recorder struct {
	*telemetry.Metrics
	reader *sdkmetric.ManualReader
}

// New returns a Recorder with its own meter provider, shut down when the test ends.
func New(tb testing.TB) *Recorder {
	tb.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	tb.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return &Recorder{
		Metrics: telemetry.NewMetricsWithMeter(nil, provider.Meter(telemetry.AppName)),
		reader:  reader,
	}
}

// UseGlobal makes a new Recorder the process-wide telemetry.Global() for the rest of the test.
// Tests that use it must not run in parallel with each other.
func UseGlobal(tb testing.TB) *Recorder {
	tb.Helper()
	r := New(tb)
	previous := telemetry.SetGlobal(r.Metrics)
	tb.Cleanup(func() { telemetry.SetGlobal(previous) })
	return r
}

// Collect returns everything recorded so far.
func (r *Recorder) Collect(tb testing.TB) metricdata.ResourceMetrics {
	tb.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.reader.Collect(context.Background(), &rm); err != nil {
		tb.Fatalf("collect metrics: %v", err)
	}
	return rm
}

// HistogramCount returns how many values were recorded on the named histogram across points
// whose attributes include all of attrs.
func (r *Recorder) HistogramCount(tb testing.TB, name string, attrs ...attribute.KeyValue) uint64 {
	tb.Helper()
	var total uint64
	for _, p := range r.histogramPoints(tb, name) {
		if hasAttrs(p.Attributes, attrs) {
			total += p.Count
		}
	}
	return total
}

// HistogramSum returns the sum of values recorded on the named histogram across points whose
// attributes include all of attrs.
func (r *Recorder) HistogramSum(tb testing.TB, name string, attrs ...attribute.KeyValue) float64 {
	tb.Helper()
	var total float64
	for _, p := range r.histogramPoints(tb, name) {
		if hasAttrs(p.Attributes, attrs) {
			total += p.Sum
		}
	}
	return total
}

// CounterValue returns the total of the named counter or up-down counter across points whose
// attributes include all of attrs.
func (r *Recorder) CounterValue(tb testing.TB, name string, attrs ...attribute.KeyValue) int64 {
	tb.Helper()
	var total int64
	for _, m := range r.metrics(tb, name) {
		sum, ok := m.Data.(metricdata.Sum[int64])
		if !ok {
			tb.Fatalf("metric %q is %T, not an int64 counter", name, m.Data)
		}
		for _, p := range sum.DataPoints {
			if hasAttrs(p.Attributes, attrs) {
				total += p.Value
			}
		}
	}
	return total
}

// GaugeValue returns the last observed value of the named gauge for the point whose
// attributes include all of attrs, and whether one was found.
func (r *Recorder) GaugeValue(tb testing.TB, name string, attrs ...attribute.KeyValue) (float64, bool) {
	tb.Helper()
	for _, m := range r.metrics(tb, name) {
		gauge, ok := m.Data.(metricdata.Gauge[float64])
		if !ok {
			tb.Fatalf("metric %q is %T, not a float64 gauge", name, m.Data)
		}
		for _, p := range gauge.DataPoints {
			if hasAttrs(p.Attributes, attrs) {
				return p.Value, true
			}
		}
	}
	return 0, false
}

// AttributeValues returns every value seen for key on the named metric, for asserting that a
// label stays within a bounded set.
func (r *Recorder) AttributeValues(tb testing.TB, name string, key attribute.Key) []string {
	tb.Helper()
	seen := map[string]bool{}
	var out []string
	add := func(set attribute.Set) {
		if v, ok := set.Value(key); ok && !seen[v.Emit()] {
			seen[v.Emit()] = true
			out = append(out, v.Emit())
		}
	}
	for _, m := range r.metrics(tb, name) {
		switch data := m.Data.(type) {
		case metricdata.Histogram[float64]:
			for _, p := range data.DataPoints {
				add(p.Attributes)
			}
		case metricdata.Sum[int64]:
			for _, p := range data.DataPoints {
				add(p.Attributes)
			}
		case metricdata.Gauge[float64]:
			for _, p := range data.DataPoints {
				add(p.Attributes)
			}
		}
	}
	return out
}

func (r *Recorder) histogramPoints(tb testing.TB, name string) []metricdata.HistogramDataPoint[float64] {
	tb.Helper()
	var points []metricdata.HistogramDataPoint[float64]
	for _, m := range r.metrics(tb, name) {
		hist, ok := m.Data.(metricdata.Histogram[float64])
		if !ok {
			tb.Fatalf("metric %q is %T, not a float64 histogram", name, m.Data)
		}
		points = append(points, hist.DataPoints...)
	}
	return points
}

func (r *Recorder) metrics(tb testing.TB, name string) []metricdata.Metrics {
	tb.Helper()
	var out []metricdata.Metrics
	for _, scope := range r.Collect(tb).ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				out = append(out, m)
			}
		}
	}
	return out
}

func hasAttrs(set attribute.Set, want []attribute.KeyValue) bool {
	for _, kv := range want {
		v, ok := set.Value(kv.Key)
		if !ok || v != kv.Value {
			return false
		}
	}
	return true
}
