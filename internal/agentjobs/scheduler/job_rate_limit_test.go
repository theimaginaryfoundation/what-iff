package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/reugn/go-quartz/quartz"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type congestionStoreStub struct {
	countToReturn int
	countErr      error
	messages      []models.ChatMessage
}

func (s *congestionStoreStub) TryAcquireSchedulerLeaderLock(ctx context.Context, lockKey int64) (datastore.SchedulerLeaderLock, bool, error) {
	return nil, false, nil
}

func (s *congestionStoreStub) ListAgentJobsForScheduler(ctx context.Context) ([]*models.AgentJob, error) {
	return nil, nil
}

func (s *congestionStoreStub) GetAgentJob(ctx context.Context, userID, id uuid.UUID) (*models.AgentJob, error) {
	return nil, nil
}

func (s *congestionStoreStub) CountRecentSuccessfulAgentJobExecutions(ctx context.Context, userID uuid.UUID, since time.Time) (int, error) {
	return s.countToReturn, s.countErr
}

func (s *congestionStoreStub) RecordAgentJobRun(ctx context.Context, userID, id uuid.UUID, lastRunAt time.Time, nextRunAt *time.Time, runError string, newStatus *models.AgentJobStatus) (*models.AgentJob, error) {
	return nil, nil
}

func (s *congestionStoreStub) SetAgentJobChat(ctx context.Context, userID, id uuid.UUID, chatID *uuid.UUID) (*models.AgentJob, error) {
	return nil, nil
}

func (s *congestionStoreStub) CreateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error) {
	return nil, nil
}

func (s *congestionStoreStub) GetUserByID(ctx context.Context, userID uuid.UUID) (*models.UserResponse, error) {
	return nil, nil
}

func (s *congestionStoreStub) CreateChatMessage(ctx context.Context, userID uuid.UUID, chatMessage models.ChatMessage) (*models.ChatMessage, error) {
	s.messages = append(s.messages, chatMessage)
	msg := chatMessage
	return &msg, nil
}

type congestionSchedulerStub struct {
	scheduleCalls int
	scheduleErr   error
}

func (s *congestionSchedulerStub) Start(context.Context) {}
func (s *congestionSchedulerStub) IsStarted() bool       { return true }
func (s *congestionSchedulerStub) GetJobKeys(...quartz.Matcher[quartz.ScheduledJob]) ([]*quartz.JobKey, error) {
	return nil, nil
}
func (s *congestionSchedulerStub) GetScheduledJob(*quartz.JobKey) (quartz.ScheduledJob, error) {
	return nil, nil
}
func (s *congestionSchedulerStub) DeleteJob(*quartz.JobKey) error { return nil }
func (s *congestionSchedulerStub) PauseJob(*quartz.JobKey) error  { return nil }
func (s *congestionSchedulerStub) ResumeJob(*quartz.JobKey) error { return nil }
func (s *congestionSchedulerStub) Clear() error                   { return nil }
func (s *congestionSchedulerStub) Wait(context.Context)           {}
func (s *congestionSchedulerStub) Stop()                          {}
func (s *congestionSchedulerStub) ScheduleJob(*quartz.JobDetail, quartz.Trigger) error {
	s.scheduleCalls++
	return s.scheduleErr
}

func TestHandleSchedulerCongestion_ShortRecurrenceSkipsWithSystemMessage(t *testing.T) {
	ds := &congestionStoreStub{countToReturn: schedulerMaxExecutionsPerWindow}
	sched := &congestionSchedulerStub{}
	m := &Manager{ds: ds, logger: zap.NewNop(), sched: sched}

	blocked := m.handleSchedulerCongestion(
		context.Background(),
		uuid.New(),
		uuid.New(),
		uuid.New(),
		&models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron},
		time.Now().UTC(),
		executionOptions{deferredRetryCount: 0, recurrenceHint: recurrenceBucketShort},
	)
	if !blocked {
		t.Fatalf("expected blocked=true")
	}
	if sched.scheduleCalls != 0 {
		t.Fatalf("expected no deferred retry scheduling")
	}
	if len(ds.messages) != 1 {
		t.Fatalf("expected one skip message, got %d", len(ds.messages))
	}
	if ds.messages[0].GenerationPersonality != schedulerSystemGenerationPersonality {
		t.Fatalf("expected generation personality %q, got %q", schedulerSystemGenerationPersonality, ds.messages[0].GenerationPersonality)
	}
	if ds.messages[0].GenerationModel != schedulerSystemGenerationModel {
		t.Fatalf("expected generation model %q, got %q", schedulerSystemGenerationModel, ds.messages[0].GenerationModel)
	}
}

func TestHandleSchedulerCongestion_MediumRecurrenceDefersOnceThenAborts(t *testing.T) {
	ds := &congestionStoreStub{countToReturn: schedulerMaxExecutionsPerWindow}
	sched := &congestionSchedulerStub{}
	m := &Manager{ds: ds, logger: zap.NewNop(), sched: sched}

	blockedFirst := m.handleSchedulerCongestion(
		context.Background(),
		uuid.New(),
		uuid.New(),
		uuid.New(),
		&models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron},
		time.Now().UTC(),
		executionOptions{deferredRetryCount: 0, recurrenceHint: recurrenceBucketMedium},
	)
	if !blockedFirst || sched.scheduleCalls != 1 {
		t.Fatalf("expected first call to defer once")
	}
	if len(ds.messages) != 0 {
		t.Fatalf("expected no message on first defer")
	}

	blockedSecond := m.handleSchedulerCongestion(
		context.Background(),
		uuid.New(),
		uuid.New(),
		uuid.New(),
		&models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron},
		time.Now().UTC(),
		executionOptions{deferredRetryCount: 1, recurrenceHint: recurrenceBucketMedium},
	)
	if !blockedSecond {
		t.Fatalf("expected second call to remain blocked")
	}
	if sched.scheduleCalls != 1 {
		t.Fatalf("expected no second defer for medium recurrence")
	}
	if len(ds.messages) != 1 {
		t.Fatalf("expected one skip message after medium retry exhausted")
	}
}

func TestHandleSchedulerCongestion_LongRecurrenceAllowsTwoDeferredRetries(t *testing.T) {
	ds := &congestionStoreStub{countToReturn: schedulerMaxExecutionsPerWindow}
	sched := &congestionSchedulerStub{}
	m := &Manager{ds: ds, logger: zap.NewNop(), sched: sched}

	m.handleSchedulerCongestion(
		context.Background(), uuid.New(), uuid.New(), uuid.New(),
		&models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron},
		time.Now().UTC(), executionOptions{deferredRetryCount: 0, recurrenceHint: recurrenceBucketLong},
	)
	m.handleSchedulerCongestion(
		context.Background(), uuid.New(), uuid.New(), uuid.New(),
		&models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron},
		time.Now().UTC(), executionOptions{deferredRetryCount: 1, recurrenceHint: recurrenceBucketLong},
	)
	m.handleSchedulerCongestion(
		context.Background(), uuid.New(), uuid.New(), uuid.New(),
		&models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron},
		time.Now().UTC(), executionOptions{deferredRetryCount: 2, recurrenceHint: recurrenceBucketLong},
	)

	if sched.scheduleCalls != 2 {
		t.Fatalf("expected two deferred retries for long recurrence, got %d", sched.scheduleCalls)
	}
	if len(ds.messages) != 1 {
		t.Fatalf("expected one final skip message after retries exhausted, got %d", len(ds.messages))
	}
}

func TestHandleSchedulerCongestion_DeferSchedulingFailureSkipsExecution(t *testing.T) {
	ds := &congestionStoreStub{countToReturn: schedulerMaxExecutionsPerWindow}
	sched := &congestionSchedulerStub{scheduleErr: errors.New("transient scheduler failure")}
	m := &Manager{ds: ds, logger: zap.NewNop(), sched: sched}

	blocked := m.handleSchedulerCongestion(
		context.Background(),
		uuid.New(),
		uuid.New(),
		uuid.New(),
		&models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron},
		time.Now().UTC(),
		executionOptions{deferredRetryCount: 0, recurrenceHint: recurrenceBucketMedium},
	)

	if !blocked {
		t.Fatalf("expected blocked=true when defer scheduling fails")
	}
	if len(ds.messages) != 1 {
		t.Fatalf("expected one infra skip message on defer scheduling failure, got %d", len(ds.messages))
	}
	if ds.messages[0].Message != schedulerSkipInfraMessage {
		t.Fatalf("expected infra skip message %q, got %q", schedulerSkipInfraMessage, ds.messages[0].Message)
	}
}

func TestHandleSchedulerCongestion_SchedulerInactiveSkipsExecution(t *testing.T) {
	ds := &congestionStoreStub{countToReturn: schedulerMaxExecutionsPerWindow}
	m := &Manager{ds: ds, logger: zap.NewNop(), sched: nil}

	blocked := m.handleSchedulerCongestion(
		context.Background(),
		uuid.New(),
		uuid.New(),
		uuid.New(),
		&models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron},
		time.Now().UTC(),
		executionOptions{deferredRetryCount: 0, recurrenceHint: recurrenceBucketMedium},
	)

	if !blocked {
		t.Fatalf("expected blocked=true when scheduler is inactive")
	}
	if len(ds.messages) != 1 {
		t.Fatalf("expected one infra skip message when scheduler is inactive, got %d", len(ds.messages))
	}
	if ds.messages[0].Message != schedulerSkipInfraMessage {
		t.Fatalf("expected infra skip message %q, got %q", schedulerSkipInfraMessage, ds.messages[0].Message)
	}
}

func TestHandleSchedulerCongestion_CountErrorSkipsExecution(t *testing.T) {
	ds := &congestionStoreStub{
		countErr: errors.New("datastore unavailable"),
	}
	m := &Manager{ds: ds, logger: zap.NewNop(), sched: &congestionSchedulerStub{}}

	blocked := m.handleSchedulerCongestion(
		context.Background(),
		uuid.New(),
		uuid.New(),
		uuid.New(),
		&models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron},
		time.Now().UTC(),
		executionOptions{deferredRetryCount: 0, recurrenceHint: recurrenceBucketShort},
	)

	if !blocked {
		t.Fatalf("expected blocked=true when congestion check fails")
	}
	if len(ds.messages) != 1 {
		t.Fatalf("expected one infra skip message when congestion check fails, got %d", len(ds.messages))
	}
	if ds.messages[0].Message != schedulerSkipInfraMessage {
		t.Fatalf("expected infra skip message %q, got %q", schedulerSkipInfraMessage, ds.messages[0].Message)
	}
}

func TestClassifyRecurrenceBucket_CronIntervals(t *testing.T) {
	tests := []struct {
		name string
		cron string
		want recurrenceBucket
	}{
		{name: "short every five minutes", cron: "0 0/5 * * * ?", want: recurrenceBucketShort},
		{name: "medium every fifteen minutes", cron: "0 0/15 * * * ?", want: recurrenceBucketMedium},
		{name: "long every thirty minutes", cron: "0 0/30 * * * ?", want: recurrenceBucketLong},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			job := &models.AgentJob{
				ScheduleType: models.AgentJobScheduleTypeCron,
				Schedule:     &tc.cron,
				Timezone:     "UTC",
			}
			got, err := classifyRecurrenceBucket(job, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatalf("classifyRecurrenceBucket() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("classifyRecurrenceBucket() = %v, want %v", got, tc.want)
			}
		})
	}
}
