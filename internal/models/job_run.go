package models

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// JobRun is the agent job a turn ran for (ADR 0x040); it lives in modeltypes so the Ent schema can
// store it.
type JobRun = modeltypes.JobRun

type jobRunKey struct{}

// WithJobRun marks ctx as running the agent job run. The scheduler sets it on a run's context; the
// agent records it on the run's queue ticket and its reply. Carried on the context, like the job's
// turn settings, because several interfaces sit between the two.
func WithJobRun(ctx context.Context, run JobRun) context.Context {
	return context.WithValue(ctx, jobRunKey{}, run)
}

// JobRunFromContext returns the agent job ctx runs for, or nil when it runs for none.
func JobRunFromContext(ctx context.Context) *JobRun {
	if ctx == nil {
		return nil
	}
	run, ok := ctx.Value(jobRunKey{}).(JobRun)
	if !ok || run.AgentJobID == uuid.Nil {
		return nil
	}
	return &run
}

// CopyJobRun copies from's job run, if any, onto to.
func CopyJobRun(from, to context.Context) context.Context {
	if run := JobRunFromContext(from); run != nil {
		return WithJobRun(to, *run)
	}
	return to
}
