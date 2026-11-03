package datastore

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// countRecentSuccessfulAgentJobExecutions, when non-nil, counts recent job_run
// usage events for a user. It is nil in builds without usage metering (the
// open-source build): no usage events are recorded there, so the count is
// always zero and the scheduler's per-user congestion limiting is inert. A
// linked metering implementation sets it in an init() (see the overlay).
var countRecentSuccessfulAgentJobExecutions func(ctx context.Context, d *Datastore, userID uuid.UUID, since time.Time) (int, error)

// listRecentAgentJobExecutionTimes, when non-nil, returns when each run
// countRecentSuccessfulAgentJobExecutions counts started, oldest first. It is
// optional even when the counter is linked: without it the scheduler still
// limits runs but cannot say when the next one is allowed.
var listRecentAgentJobExecutionTimes func(ctx context.Context, d *Datastore, userID uuid.UUID, since time.Time) ([]time.Time, error)

// CountRecentSuccessfulAgentJobExecutions returns how many agent-job runs the
// user started since a timestamp, which the agent-job scheduler uses as its
// per-user run count (congestion control, not billing logic). Returns 0 when no
// metering implementation is linked (usage events do not exist in that build).
//
// An implementation counts runs, not ActionTypeJobRun usage events (issue
// #427): those are billing records, so an Advanced-loop run records one per
// step plus its final one, and run_subagent records one from any turn (chat
// turns included). The overlay groups a run's events by turn and leaves
// subagent events out. A webhook-started run, and a failed run that made model
// calls, are runs and count.
func (d *Datastore) CountRecentSuccessfulAgentJobExecutions(ctx context.Context, userID uuid.UUID, since time.Time) (int, error) {
	if countRecentSuccessfulAgentJobExecutions == nil {
		return 0, nil
	}
	return countRecentSuccessfulAgentJobExecutions(ctx, d, userID, since)
}

// ListRecentAgentJobExecutionTimes returns when each run counted by
// CountRecentSuccessfulAgentJobExecutions started, oldest first. The scheduler
// reads it only once a user is over the limit, to tell them when the window
// frees a slot. Returns nil when no implementation is linked; callers treat
// that as "reset time unknown".
func (d *Datastore) ListRecentAgentJobExecutionTimes(ctx context.Context, userID uuid.UUID, since time.Time) ([]time.Time, error) {
	if listRecentAgentJobExecutionTimes == nil {
		return nil, nil
	}
	return listRecentAgentJobExecutionTimes(ctx, d, userID, since)
}
