package scheduler

import (
	"context"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// Scheduled-run outcomes for telemetry.SchedulerRuns: a fixed set.
const (
	runOutcomeRan               = "ran"
	runOutcomeFailed            = "failed"
	runOutcomeSkippedOverlap    = "skipped_overlap"
	runOutcomeSkippedInactive   = "skipped_inactive"
	runOutcomeSkippedCongestion = "skipped_congestion"
	runOutcomeDeferred          = "deferred"
	runOutcomeLoadFailed        = "load_failed"
	runOutcomeChatResolveFailed = "chat_resolve_failed"
	runOutcomeMisfire           = "misfire"
)

// recordRun counts one scheduled-job firing by outcome. The scheduler isn't handed a
// *telemetry.Metrics, so it records through telemetry.Global().
func recordRun(ctx context.Context, outcome string) {
	telemetry.Global().Add(ctx, telemetry.SchedulerRuns, 1, telemetry.AttrOutcome.String(outcome))
}

// recordRunDuration records how long a scheduled job that actually ran took (ran or failed).
func recordRunDuration(ctx context.Context, outcome string, d time.Duration) {
	telemetry.Global().RecordDuration(ctx, telemetry.SchedulerRunDuration, d, telemetry.AttrOutcome.String(outcome))
}

// recordLateness records how long after its planned time a scheduled run started. Only regular
// firings count: manual runs and deferred retries have no planned time to be late against.
func recordLateness(ctx context.Context, planned *time.Time, startedAt time.Time, opts executionOptions) {
	if planned == nil || planned.IsZero() || opts.allowPaused || opts.deferredRetryCount > 0 {
		return
	}
	late := startedAt.Sub(*planned)
	if late < 0 {
		late = 0
	}
	telemetry.Global().RecordDuration(ctx, telemetry.SchedulerLateness, late)
}
