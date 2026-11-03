package models

import (
	"context"

	"github.com/google/uuid"
)

// JobRunSource names the agent job a turn runs for (ADR 0x040). The scheduler sets it on a run's
// context, and the agent shows it, with the job's prompt, as the turn's display-only run_job tool
// call, so a job's reply says where it came from. Carried on the context, like the job's turn
// settings, because several interfaces sit between the two.
type JobRunSource struct {
	AgentJobID uuid.UUID
	Title      string
}

type jobRunSourceKey struct{}

// WithJobRunSource marks ctx as running the agent job src.
func WithJobRunSource(ctx context.Context, src JobRunSource) context.Context {
	return context.WithValue(ctx, jobRunSourceKey{}, src)
}

// JobRunSourceFromContext returns the agent job ctx runs for, if any.
func JobRunSourceFromContext(ctx context.Context) (JobRunSource, bool) {
	if ctx == nil {
		return JobRunSource{}, false
	}
	src, ok := ctx.Value(jobRunSourceKey{}).(JobRunSource)
	return src, ok && src.AgentJobID != uuid.Nil
}

// CopyJobRunSource copies from's job run source, if any, onto to.
func CopyJobRunSource(from, to context.Context) context.Context {
	if src, ok := JobRunSourceFromContext(from); ok {
		return WithJobRunSource(to, src)
	}
	return to
}
