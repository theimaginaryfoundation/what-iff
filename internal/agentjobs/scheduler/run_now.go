package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ErrRunLimitReached is what a *RunLimitError matches with errors.Is.
var ErrRunLimitReached = errors.New("agent job run limit reached")

// RunLimitError is returned by RunAgentJobNow when the user has used up the per-user execution
// window (schedulerMaxExecutionsPerWindow runs per schedulerExecutionWindow). RetryAt is when the
// window next frees a slot, or nil when the datastore cannot say (issue #427).
type RunLimitError struct {
	Limit   int
	Window  time.Duration
	RetryAt *time.Time
}

func (e *RunLimitError) Error() string {
	if e.RetryAt != nil {
		return fmt.Sprintf("agent job run limit reached (%d per %s); next run allowed at %s", e.Limit, e.Window, e.RetryAt.UTC().Format(time.RFC3339))
	}
	return fmt.Sprintf("agent job run limit reached (%d per %s)", e.Limit, e.Window)
}

// Is makes errors.Is(err, ErrRunLimitReached) hold for any *RunLimitError.
func (e *RunLimitError) Is(target error) bool { return target == ErrRunLimitReached }

// RunAgentJobNow triggers an agent job execution asynchronously outside normal schedule recurrence.
//
// It works on every instance while the manager is running, leader or follower: a manual run needs
// only the datastore and the agent (never the quartz scheduler, which exists on the leader alone),
// and the per-chat turn gate orders it against any scheduled run of the same job on the leader.
// Before issue #427 it required the local quartz scheduler, so with more than one API instance a
// request that landed on a follower failed with ErrSchedulerNotActive.
//
// It returns ErrSchedulerNotActive only when the manager is not running here (not started, its
// start failed, or it was stopped), and a *RunLimitError when the user is over the execution
// window, which is checked here so the caller can say so instead of the run being skipped later.
func (m *Manager) RunAgentJobNow(ctx context.Context, userID, id uuid.UUID) error {
	if m == nil || !m.running.Load() {
		return ErrSchedulerNotActive
	}
	if err := m.checkManualRunLimit(ctx, userID, time.Now().UTC()); err != nil {
		return err
	}

	execCtx := context.WithoutCancel(ctx)
	go m.executeAgentJobDetached(execCtx, userID, id, executionOptions{
		allowPaused:        true,
		manual:             true,
		deferredRetryCount: 0,
		recurrenceHint:     recurrenceBucketUnknown,
	})
	return nil
}

// checkManualRunLimit applies the execution-window limit handleSchedulerCongestion enforces, up
// front, so a manual run over the limit is refused with a *RunLimitError. The congestion check
// still runs when the run executes; it only fires here if another run took the last slot between
// the two.
func (m *Manager) checkManualRunLimit(ctx context.Context, userID uuid.UUID, now time.Time) error {
	since := now.Add(-schedulerExecutionWindow)
	executions, err := m.ds.CountRecentSuccessfulAgentJobExecutions(ctx, userID, since)
	if err != nil {
		return fmt.Errorf("check agent job run limit: %w", err)
	}
	if executions < schedulerMaxExecutionsPerWindow {
		return nil
	}
	return &RunLimitError{
		Limit:   schedulerMaxExecutionsPerWindow,
		Window:  schedulerExecutionWindow,
		RetryAt: m.runLimitRetryAt(ctx, userID, since),
	}
}

// runLimitRetryAt is when enough counted executions leave the window for the count to drop below
// the limit: the (count-limit+1)th oldest one plus the window. nil when the times are unavailable
// (no implementation linked, or the read failed); the limit still applies, the reset time is just
// not reported.
func (m *Manager) runLimitRetryAt(ctx context.Context, userID uuid.UUID, since time.Time) *time.Time {
	times, err := m.ds.ListRecentAgentJobExecutionTimes(ctx, userID, since)
	if err != nil {
		m.logger.Warn("failed to read agent job execution times for the run limit reset",
			zap.String("user_id", userID.String()),
			zap.Error(err),
		)
		return nil
	}
	return retryAtFromExecutionTimes(times, schedulerMaxExecutionsPerWindow, schedulerExecutionWindow)
}

// retryAtFromExecutionTimes takes execution times oldest first. With n of them and a limit of l,
// n-l+1 have to age out before a run is allowed, so the answer is times[n-l] + window.
func retryAtFromExecutionTimes(times []time.Time, limit int, window time.Duration) *time.Time {
	if limit <= 0 || len(times) < limit {
		return nil
	}
	at := times[len(times)-limit].Add(window).UTC()
	return &at
}
