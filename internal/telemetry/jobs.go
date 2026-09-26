package telemetry

import (
	"context"
	"time"
)

// Job outcomes for the outcome attribute on JobDuration.
const (
	JobOutcomeSuccess   = "success"
	JobOutcomeFailed    = "failed"
	JobOutcomeCancelled = "cancelled"
	JobOutcomePanic     = "panic"
	JobOutcomeQuota     = "quota"
	JobOutcomeTimeout   = "timeout"
)

// TrackJob counts a job as in flight and returns a function that ends it, recording its run
// time and outcome. Call it where the job starts running (not where it's enqueued):
//
//	finish := metrics.TrackJob(ctx, "chat_import")
//	defer func() { finish(outcome) }()
func (m *Metrics) TrackJob(ctx context.Context, jobType string) func(outcome string) {
	if m == nil {
		return func(string) {}
	}
	jt := AttrJobType.String(jobType)
	m.AddUpDown(ctx, JobsInFlight, 1, jt)
	start := time.Now()
	return func(outcome string) {
		m.AddUpDown(ctx, JobsInFlight, -1, jt)
		m.RecordDuration(ctx, JobDuration, time.Since(start), jt, AttrOutcome.String(outcome))
	}
}

// JobOutcomeFromError maps a job's final error to an outcome: success for nil, cancelled or
// timeout for context errors, failed otherwise. Callers with more specific outcomes (panic,
// quota) pass those directly.
func JobOutcomeFromError(err error) string {
	switch ClassifyError(err) {
	case "":
		return JobOutcomeSuccess
	case ErrorTypeCanceled:
		return JobOutcomeCancelled
	case ErrorTypeTimeout:
		return JobOutcomeTimeout
	default:
		return JobOutcomeFailed
	}
}
