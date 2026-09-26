package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// startJobRun marks an async job's worker as started: it records how long the job waited
// since it was created (telemetry.JobQueueWait) and starts telemetry.TrackJob. Call finish with
// the job's outcome when the worker ends.
func (a *Agent) startJobRun(ctx context.Context, job *models.Job) (finish func(outcome string)) {
	if job == nil {
		return func(string) {}
	}
	m := a.metrics()
	if !job.CreatedAt.IsZero() {
		m.RecordDuration(ctx, telemetry.JobQueueWait, time.Since(job.CreatedAt), telemetry.AttrJobType.String(job.JobType))
	}
	return m.TrackJob(ctx, job.JobType)
}

// runTrackedJob runs a job worker's body under telemetry.TrackJob, deriving the outcome from
// the body's error (see chatJobOutcome). A panic is recorded as outcome panic and keeps
// unwinding, so the caller's own recover still marks the job failed.
func (a *Agent) runTrackedJob(ctx context.Context, job *models.Job, body func() error) error {
	finish := a.startJobRun(ctx, job)
	outcome := telemetry.JobOutcomePanic
	defer func() { finish(outcome) }()
	err := body()
	outcome = chatJobOutcome(err)
	return err
}

// chatJobOutcome maps a chat or agent-job turn's final error to a job outcome: quota for a
// quota rejection, otherwise telemetry.JobOutcomeFromError (context cancellation = cancelled).
func chatJobOutcome(err error) string {
	if errors.Is(err, ErrQuotaExceeded) {
		return telemetry.JobOutcomeQuota
	}
	return telemetry.JobOutcomeFromError(err)
}

// recordQuotaRejection counts a turn refused by the quota gate, labeled by the ctx call path.
func (a *Agent) recordQuotaRejection(ctx context.Context) {
	a.metrics().Add(ctx, telemetry.QuotaRejections, 1, a.callPathAttr(ctx))
}

// timeTurnStage starts timing a chat turn stage; call the returned func when it ends.
func (a *Agent) timeTurnStage(ctx context.Context, stage string) func() {
	start := time.Now()
	return func() { a.recordTurnStage(ctx, stage, time.Since(start)) }
}

// Tool label values for tool calls whose name isn't a known function tool.
const (
	toolMetricMCP   = "mcp"
	toolMetricOther = "other"
)

// toolMetricName maps a model-emitted tool name to a bounded label: the name itself for a
// function tool in the catalog (built-in or registered by an extension), "mcp" for MCP tools,
// and "other" for anything else — including names a model made up.
func toolMetricName(name string) string {
	if strings.HasPrefix(name, mcpToolNamePrefix) {
		return toolMetricMCP
	}
	for _, def := range tools.FunctionToolCatalog() {
		if def.Spec.Name == name {
			return name
		}
	}
	return toolMetricOther
}
