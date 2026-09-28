package telemetry_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestHistogramsUseTheirBucketFamily(t *testing.T) {
	t.Parallel()
	tm := telemetrytest.New(t)

	// A 42s call must land in its own bucket, not the SDK default's 10s+ overflow.
	tm.RecordDuration(context.Background(), telemetry.GenAIOperationDuration, 42*time.Second)

	for _, scope := range tm.Collect(t).ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != telemetry.GenAIOperationDuration.Name {
				continue
			}
			require.Equal(t, "s", m.Unit)
			point := m.Data.(metricdata.Histogram[float64]).DataPoints[0]
			require.Equal(t, telemetry.BucketsSlow, point.Bounds)
			require.InDelta(t, 42, point.Sum, 0.001)
			return
		}
	}
	t.Fatal("histogram not recorded")
}

func TestNilMetricsIsANoop(t *testing.T) {
	t.Parallel()
	var m *telemetry.Metrics
	ctx := context.Background()
	require.NotPanics(t, func() {
		m.Record(ctx, telemetry.FileSize, 10)
		m.RecordDuration(ctx, telemetry.JobDuration, time.Second)
		m.Add(ctx, telemetry.JobsEnqueued, 1)
		m.AddUpDown(ctx, telemetry.JobsInFlight, 1)
		m.Time(ctx, telemetry.DependencyDuration)(errors.New("boom"))
		m.TimeDependency(ctx, telemetry.DependencyS3, "put_object")(nil)
		unregister, err := m.RegisterGauges([]telemetry.Gauge{telemetry.JobsBacklog}, nil)
		require.NoError(t, err)
		require.NoError(t, unregister())
	})
}

func TestTimeAddsErrorTypeOnlyOnFailure(t *testing.T) {
	t.Parallel()
	tm := telemetrytest.New(t)
	ctx := context.Background()

	tm.TimeDependency(ctx, telemetry.DependencyS3, "get_object")(nil)
	tm.TimeDependency(ctx, telemetry.DependencyS3, "get_object")(context.DeadlineExceeded)

	name := telemetry.DependencyDuration.Name
	s3 := telemetry.AttrDependency.String(telemetry.DependencyS3)
	require.Equal(t, uint64(2), tm.HistogramCount(t, name, s3, telemetry.AttrOperation.String("get_object")))
	require.Equal(t, uint64(1), tm.HistogramCount(t, name, s3, telemetry.AttrErrorType.String(telemetry.ErrorTypeTimeout)))
	require.Equal(t, []string{telemetry.ErrorTypeTimeout}, tm.AttributeValues(t, name, telemetry.AttrErrorType))
}

func TestTimeDoesNotAliasCallerAttributes(t *testing.T) {
	t.Parallel()
	tm := telemetrytest.New(t)
	attrs := make([]attribute.KeyValue, 1, 4)
	attrs[0] = telemetry.AttrDependency.String("a")

	done := tm.Time(context.Background(), telemetry.DependencyDuration, attrs...)
	extended := append(attrs, telemetry.AttrOperation.String("caller"))
	done(errors.New("boom"))

	require.Equal(t, "caller", extended[1].Value.AsString())
}

func TestCountersAndUpDownCounters(t *testing.T) {
	t.Parallel()
	tm := telemetrytest.New(t)
	ctx := context.Background()
	chat := telemetry.AttrJobType.String("chat_message")

	tm.Add(ctx, telemetry.JobsEnqueued, 2, chat)
	tm.AddUpDown(ctx, telemetry.JobsInFlight, 1, chat)
	tm.AddUpDown(ctx, telemetry.JobsInFlight, 1, chat)
	tm.AddUpDown(ctx, telemetry.JobsInFlight, -1, chat)

	require.Equal(t, int64(2), tm.CounterValue(t, telemetry.JobsEnqueued.Name, chat))
	require.Equal(t, int64(1), tm.CounterValue(t, telemetry.JobsInFlight.Name, chat))
}

func TestConcurrentFirstUseCreatesOneInstrument(t *testing.T) {
	t.Parallel()
	tm := telemetrytest.New(t)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tm.Add(context.Background(), telemetry.AppStartups, 1)
		}()
	}
	wg.Wait()
	require.Equal(t, int64(32), tm.CounterValue(t, telemetry.AppStartups.Name))
}

func TestReusingANameWithAnotherKindIsDroppedNotPanicking(t *testing.T) {
	t.Parallel()
	tm := telemetrytest.New(t)
	clash := telemetry.Histogram{Name: telemetry.AppStartups.Name, Unit: "s"}

	tm.Add(context.Background(), telemetry.AppStartups, 1)
	require.NotPanics(t, func() { tm.Record(context.Background(), clash, 1) })
	require.Equal(t, int64(1), tm.CounterValue(t, telemetry.AppStartups.Name))
}

func TestGaugesAreSampledAtCollection(t *testing.T) {
	t.Parallel()
	tm := telemetrytest.New(t)
	pending := telemetry.AttrStatus.String("pending")
	backlog := 3.0

	unregister, err := tm.RegisterGauges([]telemetry.Gauge{telemetry.JobsBacklog}, func(_ context.Context, report telemetry.GaugeObserver) error {
		report(telemetry.JobsBacklog, backlog, pending)
		return nil
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, unregister()) }()

	v, ok := tm.GaugeValue(t, telemetry.JobsBacklog.Name, pending)
	require.True(t, ok)
	require.Equal(t, 3.0, v)

	backlog = 5
	v, _ = tm.GaugeValue(t, telemetry.JobsBacklog.Name, pending)
	require.Equal(t, 5.0, v)
}

func TestUseGlobalSwapsAndRestores(t *testing.T) {
	before := telemetry.Global()
	t.Run("inner", func(t *testing.T) {
		tm := telemetrytest.UseGlobal(t)
		telemetry.Global().Add(context.Background(), telemetry.AppStartups, 1)
		require.Equal(t, int64(1), tm.CounterValue(t, telemetry.AppStartups.Name))
	})
	require.Same(t, before, telemetry.Global())
}

func TestTrackJobRecordsInFlightAndOutcome(t *testing.T) {
	t.Parallel()
	tm := telemetrytest.New(t)
	ctx := context.Background()
	imp := telemetry.AttrJobType.String("chat_import")

	finish := tm.TrackJob(ctx, "chat_import")
	require.Equal(t, int64(1), tm.CounterValue(t, telemetry.JobsInFlight.Name, imp))
	finish(telemetry.JobOutcomeFromError(context.Canceled))

	require.Equal(t, int64(0), tm.CounterValue(t, telemetry.JobsInFlight.Name, imp))
	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.JobDuration.Name, imp,
		telemetry.AttrOutcome.String(telemetry.JobOutcomeCancelled)))
	require.Equal(t, telemetry.JobOutcomeSuccess, telemetry.JobOutcomeFromError(nil))
	require.Equal(t, telemetry.JobOutcomeFailed, telemetry.JobOutcomeFromError(errors.New("boom")))
}
