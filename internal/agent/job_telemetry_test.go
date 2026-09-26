package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
	"go.uber.org/zap"
)

func newMetricsAgent(t *testing.T) (*Agent, *telemetrytest.Recorder) {
	t.Helper()
	tm := telemetrytest.New(t)
	return &Agent{logger: zap.NewNop(), telemetry: &telemetry.Telemetry{Logger: zap.NewNop(), Metrics: tm.Metrics}}, tm
}

func TestRunTrackedJob_OutcomeFromError(t *testing.T) {
	t.Parallel()
	a, tm := newMetricsAgent(t)
	ctx := context.Background()
	job := &models.Job{JobType: JobTypeChatMessage, CreatedAt: time.Now().Add(-time.Second)}

	cases := []struct {
		err     error
		outcome string
	}{
		{nil, telemetry.JobOutcomeSuccess},
		{fmt.Errorf("%w for user x", ErrQuotaExceeded), telemetry.JobOutcomeQuota},
		{context.Canceled, telemetry.JobOutcomeCancelled},
		{fmt.Errorf("inference: %w", context.DeadlineExceeded), telemetry.JobOutcomeTimeout},
		{errors.New("provider exploded"), telemetry.JobOutcomeFailed},
	}
	for _, c := range cases {
		got := a.runTrackedJob(ctx, job, func() error { return c.err })
		require.Equal(t, c.err, got)
	}

	chatType := telemetry.AttrJobType.String(JobTypeChatMessage)
	for _, c := range cases {
		require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.JobDuration.Name, chatType, telemetry.AttrOutcome.String(c.outcome)), c.outcome)
	}
	require.Equal(t, uint64(len(cases)), tm.HistogramCount(t, telemetry.JobQueueWait.Name, chatType))
	require.Equal(t, int64(0), tm.CounterValue(t, telemetry.JobsInFlight.Name, chatType), "every run left the in-flight count")
}

func TestRunTrackedJob_PanicIsRecordedAndPropagates(t *testing.T) {
	t.Parallel()
	a, tm := newMetricsAgent(t)
	job := &models.Job{JobType: JobTypeAgentJobRun}

	require.Panics(t, func() {
		_ = a.runTrackedJob(context.Background(), job, func() error { panic("boom") })
	})
	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.JobDuration.Name,
		telemetry.AttrJobType.String(JobTypeAgentJobRun), telemetry.AttrOutcome.String(telemetry.JobOutcomePanic)))
}

func TestRunPersonalityMediaJob_MissingUserRecordsFailedOutcome(t *testing.T) {
	t.Parallel()
	ds, _, cleanup := newTestDatastore(t)
	defer cleanup()
	a, tm := newMetricsAgent(t)
	a.ds = ds
	job := &models.Job{ID: uuid.New(), UserID: uuid.New(), JobType: JobTypeExpressionGrid}

	a.runPersonalityMediaJob(context.Background(), job, func(context.Context) (uuid.UUID, error) {
		return uuid.Nil, nil
	})
	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.JobDuration.Name,
		telemetry.AttrJobType.String(JobTypeExpressionGrid), telemetry.AttrOutcome.String(telemetry.JobOutcomeFailed)))
}

func TestToolMetricName_StaysBounded(t *testing.T) {
	t.Parallel()
	require.Equal(t, "web_search", toolMetricName("web_search"))
	require.Equal(t, "create_memory", toolMetricName("create_memory"))
	require.Equal(t, toolMetricMCP, toolMetricName("mcp__jira__get_ticket"))
	require.Equal(t, toolMetricOther, toolMetricName("definitely_not_a_tool"))
	require.Equal(t, toolMetricOther, toolMetricName(""))
}

func TestExecuteToolUse_UnknownToolRecordsOtherWithError(t *testing.T) {
	t.Parallel()
	a, tm := newMetricsAgent(t)
	chatCtx := &chatContext{chat: &models.Chat{}}

	res, _ := a.executeToolUseWithRecovery(context.Background(), chatCtx, provider.ToolUse{ID: "t1", Name: "hallucinated_tool_42"})
	require.True(t, res.IsErr)

	name := telemetry.ToolDuration.Name
	require.Equal(t, uint64(1), tm.HistogramCount(t, name,
		telemetry.AttrTool.String(toolMetricOther), telemetry.AttrErrorType.String(telemetry.ErrorTypeOther)))
	require.Equal(t, []string{toolMetricOther}, tm.AttributeValues(t, name, telemetry.AttrTool))
}

func TestRecordQuotaRejection_LabelsCallPath(t *testing.T) {
	t.Parallel()
	a, tm := newMetricsAgent(t)
	a.recordQuotaRejection(telemetry.WithCallPath(context.Background(), telemetry.CallPathAgentJob))
	require.Equal(t, int64(1), tm.CounterValue(t, telemetry.QuotaRejections.Name,
		telemetry.AttrCallPath.String(string(telemetry.CallPathAgentJob))))
}

func TestTimeTurnStage_RecordsStageAndCallPath(t *testing.T) {
	t.Parallel()
	a, tm := newMetricsAgent(t)
	ctx := telemetry.WithCallPath(context.Background(), telemetry.CallPathUserChat)
	a.timeTurnStage(ctx, turnStageCheckpointSummary)()
	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.ChatTurnStageDuration.Name,
		telemetry.AttrStage.String(turnStageCheckpointSummary),
		telemetry.AttrCallPath.String(string(telemetry.CallPathUserChat))))
}
