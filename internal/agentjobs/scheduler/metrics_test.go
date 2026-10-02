package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
	"go.uber.org/zap"
)

// jobStoreStub returns a fixed agent job (or error) from GetAgentJob and fails chat creation.
type jobStoreStub struct {
	congestionStoreStub
	job    *models.AgentJob
	getErr error
}

func (s *jobStoreStub) GetAgentJob(context.Context, uuid.UUID, uuid.UUID) (*models.AgentJob, error) {
	return s.job, s.getErr
}

func (s *jobStoreStub) CreateChat(context.Context, uuid.UUID, models.Chat) (*models.Chat, error) {
	return nil, errors.New("db down")
}

func runOutcomes(t *testing.T, tm *telemetrytest.Recorder) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, o := range tm.AttributeValues(t, telemetry.SchedulerRuns.Name, telemetry.AttrOutcome) {
		out[o] = tm.CounterValue(t, telemetry.SchedulerRuns.Name, telemetry.AttrOutcome.String(o))
	}
	return out
}

// Not parallel: records through telemetry.Global().
func TestExecuteAgentJob_RecordsRunOutcomes(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	ctx := context.Background()
	userID, jobID := uuid.New(), uuid.New()
	chatID := uuid.New()
	past := time.Now().Add(-2 * time.Minute)
	active := &models.AgentJob{ID: jobID, UserID: userID, Status: models.AgentJobStatusActive,
		ScheduleType: models.AgentJobScheduleTypeAt, RunAt: &past, NextRunAt: &past}

	newManager := func(ds schedulerStore) *Manager {
		return &Manager{ds: ds, logger: zap.NewNop(), inFlight: map[uuid.UUID]bool{}, fingerprints: map[uuid.UUID]string{}}
	}

	// Overlap: the same job is already in flight.
	m := newManager(&jobStoreStub{job: active})
	m.inFlight[jobID] = true
	m.executeAgentJob(ctx, userID, jobID)

	// Inactive: paused jobs don't run on their schedule.
	paused := *active
	paused.Status = models.AgentJobStatusPaused
	newManager(&jobStoreStub{job: &paused}).executeAgentJob(ctx, userID, jobID)

	// Load failure.
	newManager(&jobStoreStub{getErr: errors.New("db down")}).executeAgentJob(ctx, userID, jobID)

	// No chat yet, and creating one fails.
	newManager(&jobStoreStub{job: active}).executeAgentJob(ctx, userID, jobID)

	// Congested user window on a short-recurrence job: skipped.
	withChat := *active
	withChat.ChatID = &chatID
	congested := &jobStoreStub{job: &withChat}
	congested.countToReturn = schedulerMaxExecutionsPerWindow
	newManager(congested).executeAgentJob(ctx, userID, jobID)

	require.Equal(t, map[string]int64{
		runOutcomeSkippedOverlap:    1,
		runOutcomeSkippedInactive:   1,
		runOutcomeLoadFailed:        1,
		runOutcomeChatResolveFailed: 1,
		runOutcomeSkippedCongestion: 1,
	}, runOutcomes(t, tm))

	// Lateness is recorded for the two regular firings that got past loading (chat resolve and
	// congestion), each about two minutes late.
	require.Equal(t, uint64(2), tm.HistogramCount(t, telemetry.SchedulerLateness.Name))
	require.InDelta(t, 240, tm.HistogramSum(t, telemetry.SchedulerLateness.Name), 10)
}

func TestHandleSchedulerCongestion_DeferRecordsDeferredOutcome(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	ds := &congestionStoreStub{countToReturn: schedulerMaxExecutionsPerWindow}
	m := &Manager{ds: ds, logger: zap.NewNop(), sched: &congestionSchedulerStub{}}

	blocked := m.handleSchedulerCongestion(context.Background(), uuid.New(), uuid.New(), uuid.New(),
		&models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron}, time.Now().UTC(),
		executionOptions{recurrenceHint: recurrenceBucketLong})
	require.True(t, blocked)
	require.Equal(t, map[string]int64{runOutcomeDeferred: 1}, runOutcomes(t, tm))
}

func TestRecordLateness_SkipsManualRunsAndRetries(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	planned := time.Now().Add(-time.Minute)
	recordLateness(context.Background(), &planned, time.Now(), executionOptions{allowPaused: true})
	recordLateness(context.Background(), &planned, time.Now(), executionOptions{deferredRetryCount: 1})
	recordLateness(context.Background(), nil, time.Now(), executionOptions{})
	require.Equal(t, uint64(0), tm.HistogramCount(t, telemetry.SchedulerLateness.Name))
}
