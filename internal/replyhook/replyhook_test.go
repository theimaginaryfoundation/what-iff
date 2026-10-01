package replyhook

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// withCleanHooks runs the test with the package-level hook list reset, restoring
// it afterwards so registrations do not leak between tests.
func withCleanHooks(t *testing.T) {
	t.Helper()
	orig := hooks
	hooks = nil
	t.Cleanup(func() { hooks = orig })
}

func TestFireWithNoHooksIsANoop(t *testing.T) {
	withCleanHooks(t)
	assert.False(t, Enabled())
	assert.NotPanics(t, func() { Fire(context.Background(), zap.NewNop(), Event{}) })
}

func TestFireDeliversTheEventToEveryHook(t *testing.T) {
	withCleanHooks(t)

	var mu sync.Mutex
	var got []Event
	var wg sync.WaitGroup
	record := func(_ context.Context, ev Event) {
		defer wg.Done()
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	}
	Register(record)
	Register(record)
	require.True(t, Enabled())

	jobID := uuid.New()
	ev := Event{
		UserID:    uuid.New(),
		ChatID:    uuid.New(),
		MessageID: uuid.New(),
		JobID:     &jobID,
		CallPath:  telemetry.CallPathUserChat,
	}
	wg.Add(2)
	Fire(context.Background(), zap.NewNop(), ev)
	waitOrFail(t, &wg)

	assert.Equal(t, []Event{ev, ev}, got)
}

func TestFireRecoversAPanickingHookAndStillRunsTheOthers(t *testing.T) {
	withCleanHooks(t)

	var wg sync.WaitGroup
	wg.Add(1)
	Register(func(context.Context, Event) { panic("boom") })
	Register(func(context.Context, Event) { wg.Done() })

	assert.NotPanics(t, func() { Fire(context.Background(), zap.NewNop(), Event{}) })
	waitOrFail(t, &wg)
}

func TestFireDoesNotWaitForHooks(t *testing.T) {
	withCleanHooks(t)

	release := make(chan struct{})
	defer close(release)
	Register(func(context.Context, Event) { <-release })

	done := make(chan struct{})
	go func() {
		Fire(context.Background(), zap.NewNop(), Event{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Fire blocked on a slow hook")
	}
}

func waitOrFail(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("hooks did not run")
	}
}

func TestUnregisterRemovesOnlyThatHook(t *testing.T) {
	withCleanHooks(t)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var calls []string
	hook := func(name string) Hook {
		return func(context.Context, Event) {
			defer wg.Done()
			mu.Lock()
			calls = append(calls, name)
			mu.Unlock()
		}
	}
	removeA := Register(hook("a"))
	Register(hook("b"))

	removeA()
	removeA() // removing twice is harmless

	wg.Add(1)
	Fire(context.Background(), zap.NewNop(), Event{})
	waitOrFail(t, &wg)
	assert.Equal(t, []string{"b"}, calls)
}
