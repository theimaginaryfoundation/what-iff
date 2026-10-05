package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestApplyOneOffRunOutcome(t *testing.T) {
	scheduled := time.Now().Add(time.Hour).UTC()
	complete := models.AgentJobStatusComplete
	atJob := &models.AgentJob{ScheduleType: models.AgentJobScheduleTypeAt, NextRunAt: &scheduled}
	cronJob := &models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron, NextRunAt: &scheduled}
	cronNext := scheduled.Add(24 * time.Hour)

	t.Run("scheduled firing completes the one-off job and clears its next run", func(t *testing.T) {
		status, next := applyOneOffRunOutcome(atJob, executionOptions{}, &complete, &scheduled)
		require.Equal(t, &complete, status)
		require.Nil(t, next)
	})

	t.Run("manual run leaves the one-off job's status and scheduled time alone", func(t *testing.T) {
		status, next := applyOneOffRunOutcome(atJob, executionOptions{manual: true, allowPaused: true}, &complete, nil)
		require.Nil(t, status)
		require.Equal(t, &scheduled, next)
	})

	t.Run("manual run of a paused one-off job with no stored time stays unscheduled", func(t *testing.T) {
		paused := &models.AgentJob{ScheduleType: models.AgentJobScheduleTypeAt, Status: models.AgentJobStatusPaused}
		status, next := applyOneOffRunOutcome(paused, executionOptions{manual: true, allowPaused: true}, &complete, nil)
		require.Nil(t, status)
		require.Nil(t, next)
	})

	t.Run("recurring jobs are unaffected, manual or not", func(t *testing.T) {
		for _, opts := range []executionOptions{{}, {manual: true, allowPaused: true}} {
			status, next := applyOneOffRunOutcome(cronJob, opts, nil, &cronNext)
			require.Nil(t, status)
			require.Equal(t, &cronNext, next)
		}
	})
}

type recordedRun struct {
	nextRunAt *time.Time
	status    *models.AgentJobStatus
	errText   string
}

// runRecordingStore returns a fixed job, fails chat creation (so a run ends early, before the
// agent), and records what the run persisted.
type runRecordingStore struct {
	congestionStoreStub
	job  *models.AgentJob
	runs []recordedRun
}

func (s *runRecordingStore) GetAgentJob(context.Context, uuid.UUID, uuid.UUID) (*models.AgentJob, error) {
	return s.job, nil
}

func (s *runRecordingStore) CreateChat(context.Context, uuid.UUID, models.Chat) (*models.Chat, error) {
	return nil, errors.New("db down")
}

func (s *runRecordingStore) RecordAgentJobRun(_ context.Context, _, _ uuid.UUID, _ time.Time, nextRunAt *time.Time, runError string, newStatus *models.AgentJobStatus) (*models.AgentJob, error) {
	s.runs = append(s.runs, recordedRun{nextRunAt: nextRunAt, status: newStatus, errText: runError})
	return nil, nil
}

// A failing manual run of an active one-off job records the attempt (and its error) but must not
// mark the job Failed or drop its scheduled time; a scheduled firing still ends it as Failed.
func TestExecuteAgentJob_ManualRunKeepsOneOffJobScheduled(t *testing.T) {
	userID, jobID := uuid.New(), uuid.New()
	scheduled := time.Now().Add(time.Hour).UTC()
	job := &models.AgentJob{ID: jobID, UserID: userID, Status: models.AgentJobStatusActive,
		ScheduleType: models.AgentJobScheduleTypeAt, RunAt: &scheduled, NextRunAt: &scheduled}

	run := func(opts executionOptions) recordedRun {
		ds := &runRecordingStore{job: job}
		m := &Manager{ds: ds, logger: zap.NewNop(), inFlight: map[uuid.UUID]bool{}, fingerprints: map[uuid.UUID]string{}}
		m.executeAgentJobWithOptions(context.Background(), userID, jobID, opts)
		require.Len(t, ds.runs, 1)
		return ds.runs[0]
	}

	manual := run(executionOptions{manual: true, allowPaused: true})
	require.Nil(t, manual.status)
	require.Equal(t, &scheduled, manual.nextRunAt)
	require.NotEmpty(t, manual.errText)

	regular := run(executionOptions{})
	require.NotNil(t, regular.status)
	require.Equal(t, models.AgentJobStatusFailed, *regular.status)
	require.Nil(t, regular.nextRunAt)
}
