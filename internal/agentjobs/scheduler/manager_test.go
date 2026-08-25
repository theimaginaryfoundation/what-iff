package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/reugn/go-quartz/quartz"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type acquireResult struct {
	lock     datastore.SchedulerLeaderLock
	acquired bool
	err      error
}

type fakeDatastoreProvider struct {
	mu sync.Mutex

	acquireResults []acquireResult
	acquireCalls   int
	listCalls      int
}

func (f *fakeDatastoreProvider) TryAcquireSchedulerLeaderLock(ctx context.Context, lockKey int64) (datastore.SchedulerLeaderLock, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquireCalls++
	if len(f.acquireResults) == 0 {
		return nil, false, nil
	}
	next := f.acquireResults[0]
	f.acquireResults = f.acquireResults[1:]
	return next.lock, next.acquired, next.err
}

func (f *fakeDatastoreProvider) ListAgentJobsForScheduler(ctx context.Context) ([]*models.AgentJob, error) {
	f.mu.Lock()
	f.listCalls++
	f.mu.Unlock()
	return []*models.AgentJob{}, nil
}

func (f *fakeDatastoreProvider) GetAgentJob(ctx context.Context, userID, id uuid.UUID) (*models.AgentJob, error) {
	return nil, datastore.ErrAgentJobNotFound
}

func (f *fakeDatastoreProvider) RecordAgentJobRun(ctx context.Context, userID, id uuid.UUID, lastRunAt time.Time, nextRunAt *time.Time, runError string, newStatus *models.AgentJobStatus) (*models.AgentJob, error) {
	return nil, nil
}

func (f *fakeDatastoreProvider) CountRecentSuccessfulAgentJobExecutions(ctx context.Context, userID uuid.UUID, since time.Time) (int, error) {
	return 0, nil
}

func (f *fakeDatastoreProvider) SetAgentJobChat(ctx context.Context, userID, id uuid.UUID, chatID *uuid.UUID) (*models.AgentJob, error) {
	return nil, nil
}

func (f *fakeDatastoreProvider) CreateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error) {
	return nil, errors.New("not implemented in test")
}

func (f *fakeDatastoreProvider) GetUserByID(ctx context.Context, userID uuid.UUID) (*models.UserResponse, error) {
	return nil, nil
}

func (f *fakeDatastoreProvider) CreateChatMessage(ctx context.Context, userID uuid.UUID, chatMessage models.ChatMessage) (*models.ChatMessage, error) {
	return nil, nil
}

type fakeLock struct {
	key int64

	mu           sync.Mutex
	healthChecks []bool
	releaseCalls int
}

func (l *fakeLock) Key() int64 {
	return l.key
}

func (l *fakeLock) IsHealthy(ctx context.Context) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.healthChecks) == 0 {
		return true, nil
	}
	next := l.healthChecks[0]
	l.healthChecks = l.healthChecks[1:]
	return next, nil
}

func (l *fakeLock) Release(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.releaseCalls++
	return nil
}

type fakeScheduler struct {
	mu         sync.Mutex
	started    bool
	startCalls int
	stopCalls  int
}

func (s *fakeScheduler) Start(context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = true
	s.startCalls++
}

func (s *fakeScheduler) IsStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

func (s *fakeScheduler) ScheduleJob(*quartz.JobDetail, quartz.Trigger) error { return nil }
func (s *fakeScheduler) GetJobKeys(...quartz.Matcher[quartz.ScheduledJob]) ([]*quartz.JobKey, error) {
	return nil, nil
}
func (s *fakeScheduler) GetScheduledJob(*quartz.JobKey) (quartz.ScheduledJob, error) { return nil, nil }
func (s *fakeScheduler) DeleteJob(*quartz.JobKey) error                              { return nil }
func (s *fakeScheduler) PauseJob(*quartz.JobKey) error                               { return nil }
func (s *fakeScheduler) ResumeJob(*quartz.JobKey) error                              { return nil }
func (s *fakeScheduler) Clear() error                                                { return nil }
func (s *fakeScheduler) Wait(context.Context)                                        {}
func (s *fakeScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = false
	s.stopCalls++
}

func TestManagerRun_DistributedLockContentionStaysFollower(t *testing.T) {
	ds := &fakeDatastoreProvider{}
	m, err := NewManager(ds, nil, zap.NewNop(), Config{
		Distributed:       true,
		LockKey:           11,
		LockRetryInterval: 5 * time.Millisecond,
		LockRetryJitter:   0,
		InstanceID:        "test-node",
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	var schedulerFactoryCalls int
	m.schedulerFactory = func() (quartz.Scheduler, error) {
		schedulerFactoryCalls++
		return &fakeScheduler{}, nil
	}
	m.lockHealthInterval = 5 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	m.Run(ctx)

	if schedulerFactoryCalls != 0 {
		t.Fatalf("expected no scheduler start while lock contended, got %d", schedulerFactoryCalls)
	}
	if ds.acquireCalls == 0 {
		t.Fatalf("expected at least one lock attempt")
	}
}

func TestManagerRun_DistributedLockLossStopsLeaderAndRetries(t *testing.T) {
	lock := &fakeLock{
		key:          22,
		healthChecks: []bool{false},
	}
	ds := &fakeDatastoreProvider{
		acquireResults: []acquireResult{
			{lock: lock, acquired: true},
		},
	}
	m, err := NewManager(ds, nil, zap.NewNop(), Config{
		Distributed:       true,
		LockKey:           22,
		LockRetryInterval: 5 * time.Millisecond,
		LockRetryJitter:   0,
		InstanceID:        "test-node",
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	var schedulers []*fakeScheduler
	m.schedulerFactory = func() (quartz.Scheduler, error) {
		s := &fakeScheduler{}
		schedulers = append(schedulers, s)
		return s, nil
	}
	m.lockHealthInterval = 5 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	m.Run(ctx)

	if lock.releaseCalls == 0 {
		t.Fatalf("expected lock release after lock-loss demotion")
	}
	if ds.acquireCalls < 2 {
		t.Fatalf("expected lock reacquisition attempts after demotion, got %d", ds.acquireCalls)
	}
	if len(schedulers) == 0 {
		t.Fatalf("expected scheduler to start at least once")
	}
	if schedulers[0].stopCalls == 0 {
		t.Fatalf("expected leader scheduler stop on lock loss")
	}
}

func TestManagerRun_DistributedShutdownReleasesLock(t *testing.T) {
	lock := &fakeLock{
		key:          33,
		healthChecks: []bool{true, true, true, true},
	}
	ds := &fakeDatastoreProvider{
		acquireResults: []acquireResult{
			{lock: lock, acquired: true},
		},
	}
	m, err := NewManager(ds, nil, zap.NewNop(), Config{
		Distributed:       true,
		LockKey:           33,
		LockRetryInterval: 5 * time.Millisecond,
		LockRetryJitter:   0,
		InstanceID:        "test-node",
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	s := &fakeScheduler{}
	m.schedulerFactory = func() (quartz.Scheduler, error) { return s, nil }
	m.lockHealthInterval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.Run(ctx)
	}()

	deadline := time.Now().Add(250 * time.Millisecond)
	for !s.IsStarted() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !s.IsStarted() {
		t.Fatalf("expected scheduler to start as leader")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("run did not exit after cancel")
	}

	if lock.releaseCalls == 0 {
		t.Fatalf("expected lock release on shutdown")
	}
	if s.stopCalls == 0 {
		t.Fatalf("expected scheduler stop on shutdown")
	}
}
