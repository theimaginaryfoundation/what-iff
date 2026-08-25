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

// CountRecentSuccessfulAgentJobExecutions returns recent successful agent-job
// runs for a user since a timestamp. ActionTypeJobRun usage events are emitted
// only from successful executions, so counting them is equivalent to counting
// successful runs. The agent-job scheduler uses this for per-user congestion
// control; it is not billing logic. Returns 0 when no metering implementation
// is linked (usage events do not exist in that build).
func (d *Datastore) CountRecentSuccessfulAgentJobExecutions(ctx context.Context, userID uuid.UUID, since time.Time) (int, error) {
	if countRecentSuccessfulAgentJobExecutions == nil {
		return 0, nil
	}
	return countRecentSuccessfulAgentJobExecutions(ctx, d, userID, since)
}
