package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/reugn/go-quartz/quartz"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// runningManager is a manager that is up on this instance but has no quartz scheduler, as on a
// distributed follower.
func runningManager(ds schedulerStore) *Manager {
	m := &Manager{ds: ds, logger: zap.NewNop(), inFlight: map[uuid.UUID]bool{}, fingerprints: map[uuid.UUID]string{}}
	m.running.Store(true)
	return m
}

// Issue #427: a rate-limited manual run must say so, not report the scheduler as inactive. It is
// checked on a follower (no quartz scheduler), where the old code answered ErrSchedulerNotActive
// before looking at the limit at all.
func TestRunAgentJobNow_RateLimitedIsNotSchedulerNotActive(t *testing.T) {
	now := time.Now().UTC()
	times := make([]time.Time, 0, schedulerMaxExecutionsPerWindow)
	for i := range schedulerMaxExecutionsPerWindow {
		times = append(times, now.Add(-25*time.Minute+time.Duration(i)*time.Minute))
	}
	ds := &congestionStoreStub{countToReturn: schedulerMaxExecutionsPerWindow, times: times}
	m := runningManager(ds)

	err := m.RunAgentJobNow(context.Background(), uuid.New(), uuid.New())

	require.Error(t, err)
	require.False(t, errors.Is(err, ErrSchedulerNotActive), "rate limit reported as scheduler not active: %v", err)
	require.NotContains(t, strings.ToLower(err.Error()), "not active")
	require.ErrorIs(t, err, ErrRunLimitReached)
	var limitErr *RunLimitError
	require.ErrorAs(t, err, &limitErr)
	require.Equal(t, schedulerMaxExecutionsPerWindow, limitErr.Limit)
	require.Equal(t, schedulerExecutionWindow, limitErr.Window)
	require.NotNil(t, limitErr.RetryAt)
	// The oldest counted run (25 minutes ago) leaves the 30-minute window in 5 minutes.
	require.WithinDuration(t, now.Add(5*time.Minute), *limitErr.RetryAt, time.Second)
	require.Empty(t, ds.messages, "a refused manual run posts nothing to the job's chat")
}

func TestRunAgentJobNow_RateLimitedWithoutTimesHasNoRetryAt(t *testing.T) {
	m := runningManager(&congestionStoreStub{countToReturn: schedulerMaxExecutionsPerWindow + 3})

	err := m.RunAgentJobNow(context.Background(), uuid.New(), uuid.New())

	var limitErr *RunLimitError
	require.ErrorAs(t, err, &limitErr)
	require.Nil(t, limitErr.RetryAt)
}

func TestRunAgentJobNow_CountFailureIsNotSchedulerNotActive(t *testing.T) {
	m := runningManager(&congestionStoreStub{countErr: errors.New("db down")})

	err := m.RunAgentJobNow(context.Background(), uuid.New(), uuid.New())

	require.Error(t, err)
	require.False(t, errors.Is(err, ErrSchedulerNotActive))
	require.False(t, errors.Is(err, ErrRunLimitReached))
}

func TestRunAgentJobNow_NotRunningIsSchedulerNotActive(t *testing.T) {
	m := &Manager{ds: &congestionStoreStub{}, logger: zap.NewNop()}
	require.ErrorIs(t, m.RunAgentJobNow(context.Background(), uuid.New(), uuid.New()), ErrSchedulerNotActive)

	var none *Manager
	require.ErrorIs(t, none.RunAgentJobNow(context.Background(), uuid.New(), uuid.New()), ErrSchedulerNotActive)
}

// A distributed follower (the lock is held elsewhere, so it has no quartz scheduler) accepts
// manual runs while Run is going, and stops accepting them once Run has returned.
func TestRunAgentJobNow_DistributedFollowerAcceptsUntilStopped(t *testing.T) {
	ds := &fakeDatastoreProvider{}
	m, err := NewManager(ds, nil, zap.NewNop(), Config{
		Distributed:       true,
		LockKey:           44,
		LockRetryInterval: 5 * time.Millisecond,
		InstanceID:        "follower",
	})
	require.NoError(t, err)
	m.schedulerFactory = func() (quartz.Scheduler, error) {
		t.Fatal("a follower must not start a quartz scheduler")
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.Run(ctx)
	}()
	require.Eventually(t, m.running.Load, time.Second, 5*time.Millisecond)

	m.mu.RLock()
	require.Nil(t, m.sched)
	m.mu.RUnlock()
	require.NoError(t, m.RunAgentJobNow(context.Background(), uuid.New(), uuid.New()))

	cancel()
	<-done
	require.ErrorIs(t, m.RunAgentJobNow(context.Background(), uuid.New(), uuid.New()), ErrSchedulerNotActive)
}

func TestManagerStart_FailedStartIsNotRunning(t *testing.T) {
	m, err := NewManager(&fakeDatastoreProvider{}, nil, zap.NewNop(), Config{})
	require.NoError(t, err)
	m.schedulerFactory = func() (quartz.Scheduler, error) { return nil, errors.New("no scheduler") }

	m.Start(context.Background())

	require.ErrorIs(t, m.RunAgentJobNow(context.Background(), uuid.New(), uuid.New()), ErrSchedulerNotActive)
}

// A manual run that loses the last slot after RunAgentJobNow accepted it is skipped, never
// deferred, even for a long-recurrence job and with no quartz scheduler to defer on.
func TestHandleSchedulerCongestion_ManualRunSkipsWithoutDeferring(t *testing.T) {
	ds := &congestionStoreStub{countToReturn: schedulerMaxExecutionsPerWindow}
	m := &Manager{ds: ds, logger: zap.NewNop()}

	blocked := m.handleSchedulerCongestion(
		context.Background(), uuid.New(), uuid.New(), uuid.New(),
		&models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron},
		time.Now().UTC(),
		executionOptions{manual: true, allowPaused: true, recurrenceHint: recurrenceBucketLong},
	)

	require.True(t, blocked)
	require.Len(t, ds.messages, 1)
	require.Equal(t, schedulerManualSkipMessage, ds.messages[0].Message)
}

func TestRetryAtFromExecutionTimes(t *testing.T) {
	base := time.Date(2026, time.November, 3, 12, 0, 0, 0, time.UTC)
	at := func(minutes ...int) []time.Time {
		out := make([]time.Time, 0, len(minutes))
		for _, m := range minutes {
			out = append(out, base.Add(time.Duration(m)*time.Minute))
		}
		return out
	}
	window := 30 * time.Minute

	tests := []struct {
		name  string
		times []time.Time
		limit int
		want  *time.Time
	}{
		{name: "under the limit", times: at(0, 1), limit: 3, want: nil},
		{name: "at the limit: oldest ages out", times: at(0, 5, 10), limit: 3, want: ptrTime(base.Add(30 * time.Minute))},
		{name: "over the limit: wait for enough to age out", times: at(0, 5, 10, 15), limit: 3, want: ptrTime(base.Add(35 * time.Minute))},
		{name: "no limit", times: at(0), limit: 0, want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := retryAtFromExecutionTimes(tc.times, tc.limit, window)
			if tc.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.True(t, tc.want.Equal(*got), "got %s, want %s", got, tc.want)
		})
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
