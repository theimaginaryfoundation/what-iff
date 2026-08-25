package telemetry

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetOrCreateCounterConcurrent(t *testing.T) {
	t.Parallel()

	m := NewMetrics(nil)
	const goroutines = 32

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.getOrCreateCounter("concurrent_counter")
			errs <- err
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	count := 0
	m.counters.Range(func(_, _ any) bool {
		count++
		return true
	})
	require.Equal(t, 1, count)
}

func TestGetOrCreateCounterTypeMismatch(t *testing.T) {
	t.Parallel()

	m := NewMetrics(nil)
	m.counters.Store("bad_counter", "not-a-counter")

	_, err := m.getOrCreateCounter("bad_counter")
	require.Error(t, err)
}

func TestInitStartupMetricsCreatesCounter(t *testing.T) {
	t.Parallel()

	m := NewMetrics(nil)
	err := m.InitStartupMetrics(context.Background())
	require.NoError(t, err)

	_, ok := m.counters.Load(StartupCounter)
	require.True(t, ok)
}
