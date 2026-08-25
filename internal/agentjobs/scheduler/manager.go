package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/reugn/go-quartz/quartz"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	// schedulerMisfireBufferSize keeps a small burst buffer for delayed jobs.
	schedulerMisfireBufferSize = 64
	// schedulerWorkerLimit keeps concurrent executions conservative for the in-process MVP.
	schedulerWorkerLimit = 2
	// schedulerOutdatedThreshold treats jobs older than this as misfires.
	schedulerOutdatedThreshold = 30 * time.Minute
	// schedulerReconcileInterval controls how often DB state is reconciled to in-memory schedules.
	schedulerReconcileInterval = 30 * time.Second
	// schedulerLockHealthInterval controls how often lock connectivity is checked while leader.
	schedulerLockHealthInterval = 5 * time.Second
	// schedulerExecutionWindow is the sliding window used for per-user execution limiting.
	schedulerExecutionWindow = 30 * time.Minute
	// schedulerMaxExecutionsPerWindow is the max successful executions allowed per user per window.
	schedulerMaxExecutionsPerWindow = 10
	// schedulerShortRecurrenceThreshold skips immediately when over limit.
	schedulerShortRecurrenceThreshold = 10 * time.Minute
	// schedulerMediumRecurrenceThreshold allows one deferred retry when over limit.
	schedulerMediumRecurrenceThreshold = 20 * time.Minute
	// schedulerRetryDelay controls defer wait time before retry execution.
	schedulerRetryDelay = 5 * time.Minute
	// schedulerMediumRecurrenceRetryLimit allows one deferred retry (retry #1) for <=20m recurrences.
	schedulerMediumRecurrenceRetryLimit = 1
	// schedulerLongRecurrenceRetryLimit allows two deferred retries (retry #1 and #2) for >20m recurrences.
	schedulerLongRecurrenceRetryLimit = 2
	// schedulerRecurrenceSampleRuns bounds the cron sample size when deriving recurrence interval buckets.
	schedulerRecurrenceSampleRuns = 64
	// schedulerSkipMessage is sent when a run is skipped due to congestion/rate limiting.
	schedulerSkipMessage = "I skipped this scheduled run because you reached the limit of 10 scheduled runs in 30 minutes. Please revise your schedules to run less frequently."
	// schedulerSkipInfraMessage is sent when congestion guard infrastructure fails while over-limit.
	schedulerSkipInfraMessage = "I skipped this scheduled run because the scheduler has encountered an error. Please try again in a few minutes."
	// schedulerSystemGenerationPersonality tags hardcoded scheduler messages as system personality.
	schedulerSystemGenerationPersonality = "system"
	// schedulerSystemGenerationModel tags hardcoded scheduler messages as non-LLM generated.
	schedulerSystemGenerationModel = "none"
)

// lockStore contains only distributed leadership coordination operations.
type lockStore interface {
	TryAcquireSchedulerLeaderLock(ctx context.Context, lockKey int64) (datastore.SchedulerLeaderLock, bool, error)
}

// reconciliationStore contains methods used to project DB AgentJobs state into
// the in-memory quartz schedule.
type reconciliationStore interface {
	ListAgentJobsForScheduler(ctx context.Context) ([]*models.AgentJob, error)
}

// executionStore contains methods used while executing individual AgentJobs.
type executionStore interface {
	GetAgentJob(ctx context.Context, userID, id uuid.UUID) (*models.AgentJob, error)
	CountRecentSuccessfulAgentJobExecutions(ctx context.Context, userID uuid.UUID, since time.Time) (int, error)
	RecordAgentJobRun(ctx context.Context, userID, id uuid.UUID, lastRunAt time.Time, nextRunAt *time.Time, runError string, newStatus *models.AgentJobStatus) (*models.AgentJob, error)
	SetAgentJobChat(ctx context.Context, userID, id uuid.UUID, chatID *uuid.UUID) (*models.AgentJob, error)
	CreateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error)
	GetUserByID(ctx context.Context, userID uuid.UUID) (*models.UserResponse, error)
	CreateChatMessage(ctx context.Context, userID uuid.UUID, chatMessage models.ChatMessage) (*models.ChatMessage, error)
}

// schedulerStore is the narrow surface required by the scheduler package.
// Datastore satisfies this contract without exposing unrelated APIs here.
type schedulerStore interface {
	lockStore
	reconciliationStore
	executionStore
}

type Config struct {
	Distributed       bool
	LockKey           int64
	LockRetryInterval time.Duration
	LockRetryJitter   time.Duration
	InstanceID        string
}

var ErrSchedulerNotActive = errors.New("scheduler is not active")

type leaderLifecycle struct {
	isLeader        bool
	leaderLock      datastore.SchedulerLeaderLock
	reconcileCancel context.CancelFunc
	reconcileDone   chan struct{}
}

type Manager struct {
	ds     schedulerStore
	agent  *agent.Agent
	logger *zap.Logger

	cfg Config

	mu           sync.RWMutex
	fingerprints map[uuid.UUID]string
	inFlight     map[uuid.UUID]bool

	sched quartz.Scheduler

	// lifecycleMu protects all leader lifecycle fields in lifecycle.
	//
	// Locking invariants:
	//   - lifecycle fields are read/written only while lifecycleMu is held.
	//   - startLeader/stopLeader require lifecycleMu to be held by caller.
	//   - mu protects scheduler runtime state (sched/fingerprints/inFlight).
	//   - lock ordering rule: lifecycleMu -> mu (never reverse).
	lifecycleMu sync.Mutex
	lifecycle   leaderLifecycle
	rng         *rand.Rand

	schedulerFactory   func() (quartz.Scheduler, error)
	lockHealthInterval time.Duration
}

func NewManager(ds schedulerStore, a *agent.Agent, logger *zap.Logger, cfg Config) (*Manager, error) {
	if cfg.LockRetryInterval <= 0 {
		cfg.LockRetryInterval = 2 * time.Second
	}
	if cfg.LockRetryJitter < 0 {
		cfg.LockRetryJitter = 0
	}
	if cfg.InstanceID == "" {
		cfg.InstanceID = "unknown"
	}

	m := &Manager{
		ds:                 ds,
		agent:              a,
		logger:             logger,
		cfg:                cfg,
		fingerprints:       map[uuid.UUID]string{},
		inFlight:           map[uuid.UUID]bool{},
		rng:                rand.New(rand.NewSource(time.Now().UnixNano())),
		lockHealthInterval: schedulerLockHealthInterval,
	}
	m.schedulerFactory = m.newQuartzScheduler
	return m, nil
}

func (m *Manager) newQuartzScheduler() (quartz.Scheduler, error) {
	misfires := make(chan quartz.ScheduledJob, schedulerMisfireBufferSize)
	s, err := quartz.NewStdScheduler(
		quartz.WithWorkerLimit(schedulerWorkerLimit),
		quartz.WithOutdatedThreshold(schedulerOutdatedThreshold),
		quartz.WithMisfiredChan(misfires),
		quartz.WithJobMetadata(),
	)
	if err != nil {
		return nil, fmt.Errorf("create scheduler: %w", err)
	}
	go m.logMisfires(misfires)
	return s, nil
}

func (m *Manager) Start(ctx context.Context) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.cfg.Distributed {
		m.logger.Warn("Start called in distributed mode; use Run instead")
		return
	}
	if m.lifecycle.isLeader {
		return
	}
	if err := m.startLeader(ctx, nil); err != nil {
		m.logger.Error("agent job scheduler start failed", zap.Error(err))
	}
}

func (m *Manager) Stop(ctx context.Context) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.stopLeader(ctx, "stop-called")
}

// Run executes distributed leader-election mode. In non-distributed mode it behaves
// like Start and blocks until context cancellation.
func (m *Manager) Run(ctx context.Context) {
	if !m.cfg.Distributed {
		m.Start(ctx)
		<-ctx.Done()
		return
	}

	m.logger.Info("agent job scheduler distributed mode started",
		zap.String("instance_id", m.cfg.InstanceID),
		zap.Int64("lock_key", m.cfg.LockKey),
	)

	for {
		select {
		case <-ctx.Done():
			m.Stop(context.Background())
			m.logger.Info("agent job scheduler distributed mode stopped",
				zap.String("instance_id", m.cfg.InstanceID),
			)
			return
		default:
		}

		lock, acquired, err := m.ds.TryAcquireSchedulerLeaderLock(ctx, m.cfg.LockKey)
		if err != nil {
			m.logger.Warn("agent job scheduler lock acquisition failed",
				zap.String("instance_id", m.cfg.InstanceID),
				zap.Int64("lock_key", m.cfg.LockKey),
				zap.Error(err),
			)
			m.sleepLockRetry(ctx)
			continue
		}

		if !acquired {
			m.logger.Debug("agent job scheduler follower waiting",
				zap.String("instance_id", m.cfg.InstanceID),
				zap.Int64("lock_key", m.cfg.LockKey),
			)
			m.sleepLockRetry(ctx)
			continue
		}

		leaderCtx, leaderCancel := context.WithCancel(ctx)

		m.lifecycleMu.Lock()
		if err := m.startLeader(leaderCtx, lock); err != nil {
			m.lifecycleMu.Unlock()
			m.logger.Error("agent job scheduler failed to start as leader",
				zap.String("instance_id", m.cfg.InstanceID),
				zap.Error(err),
			)
			leaderCancel()
			if releaseErr := lock.Release(context.Background()); releaseErr != nil {
				m.logger.Warn("failed to release scheduler leader lock after start failure",
					zap.String("instance_id", m.cfg.InstanceID),
					zap.Int64("lock_key", lock.Key()),
					zap.Error(releaseErr),
				)
			}
			m.sleepLockRetry(ctx)
			continue
		}
		m.lifecycleMu.Unlock()

		m.monitorLeaderLock(leaderCtx, leaderCancel)
		leaderCancel()

		m.lifecycleMu.Lock()
		m.stopLeader(context.Background(), "leadership-lost")
		m.lifecycleMu.Unlock()
		m.sleepLockRetry(ctx)
	}
}

func (m *Manager) monitorLeaderLock(ctx context.Context, cancelLeader context.CancelFunc) {
	interval := schedulerLockHealthInterval
	if m.lockHealthInterval > 0 {
		interval = m.lockHealthInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.lifecycleMu.Lock()
			lock := m.lifecycle.leaderLock
			m.lifecycleMu.Unlock()
			if lock == nil {
				return
			}

			healthy, err := lock.IsHealthy(ctx)
			if err != nil || !healthy {
				if cancelLeader != nil {
					// Stop leader session immediately to shrink the lock-loss race window.
					cancelLeader()
				}
				m.logger.Warn("agent job scheduler leader lock unhealthy",
					zap.String("instance_id", m.cfg.InstanceID),
					zap.Int64("lock_key", lock.Key()),
					zap.Error(err),
				)
				return
			}
		}
	}
}

func (m *Manager) sleepLockRetry(ctx context.Context) {
	delay := m.cfg.LockRetryInterval
	if m.cfg.LockRetryJitter > 0 {
		jitter := time.Duration(m.rng.Int63n(int64(m.cfg.LockRetryJitter) + 1))
		delay += jitter
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (m *Manager) startLeader(ctx context.Context, lock datastore.SchedulerLeaderLock) error {
	if m.lifecycle.isLeader {
		return nil
	}

	s, err := m.schedulerFactory()
	if err != nil {
		return err
	}

	reconcileCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	m.mu.Lock()
	m.sched = s
	m.fingerprints = map[uuid.UUID]string{}
	m.inFlight = map[uuid.UUID]bool{}
	m.mu.Unlock()

	m.lifecycle.reconcileCancel = cancel
	m.lifecycle.reconcileDone = done
	m.lifecycle.leaderLock = lock
	m.lifecycle.isLeader = true

	m.sched.Start(ctx)
	go func() {
		defer close(done)
		m.reconcileLoop(reconcileCtx, schedulerReconcileInterval)
	}()

	fields := []zap.Field{
		zap.String("instance_id", m.cfg.InstanceID),
	}
	if lock != nil {
		fields = append(fields, zap.Int64("lock_key", lock.Key()))
	}
	m.logger.Info("agent job scheduler leader acquired", fields...)
	return nil
}

func (m *Manager) stopLeader(ctx context.Context, reason string) {
	if !m.lifecycle.isLeader {
		return
	}

	if m.lifecycle.reconcileCancel != nil {
		m.lifecycle.reconcileCancel()
	}
	if m.lifecycle.reconcileDone != nil {
		select {
		case <-m.lifecycle.reconcileDone:
		case <-ctx.Done():
		}
	}

	m.mu.RLock()
	s := m.sched
	m.mu.RUnlock()
	if s != nil {
		s.Stop()
		s.Wait(ctx)
	}

	if m.lifecycle.leaderLock != nil {
		if err := m.lifecycle.leaderLock.Release(ctx); err != nil {
			m.logger.Warn("failed to release scheduler leader lock", zap.Error(err))
		}
	}

	m.mu.Lock()
	m.sched = nil
	m.fingerprints = map[uuid.UUID]string{}
	m.inFlight = map[uuid.UUID]bool{}
	m.mu.Unlock()

	m.lifecycle.reconcileCancel = nil
	m.lifecycle.reconcileDone = nil
	m.lifecycle.leaderLock = nil
	m.lifecycle.isLeader = false

	m.logger.Info("agent job scheduler leader stopped",
		zap.String("instance_id", m.cfg.InstanceID),
		zap.String("reason", reason),
	)
}

func (m *Manager) reconcileLoop(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()

	m.reconcileOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.reconcileOnce(ctx)
		}
	}
}

func (m *Manager) reconcileOnce(ctx context.Context) {
	jobs, err := m.ds.ListAgentJobsForScheduler(ctx)
	if err != nil {
		m.logger.Error("agent job scheduler reconcile: failed to list jobs", zap.Error(err))
		return
	}

	active := make(map[uuid.UUID]models.AgentJob, len(jobs))
	for _, j := range jobs {
		if j == nil {
			continue
		}
		if j.Status != models.AgentJobStatusActive {
			continue
		}
		active[j.ID] = *j
	}

	// Unschedule any jobs that are no longer active.
	inactiveIDs := make([]uuid.UUID, 0)
	m.mu.RLock()
	for id := range m.fingerprints {
		if _, ok := active[id]; ok {
			continue
		}
		inactiveIDs = append(inactiveIDs, id)
	}
	s := m.sched
	m.mu.RUnlock()

	for _, id := range inactiveIDs {
		if s != nil {
			_ = s.DeleteJob(quartz.NewJobKey(id.String()))
		}
	}

	m.mu.Lock()
	for _, id := range inactiveIDs {
		delete(m.fingerprints, id)
	}
	m.mu.Unlock()

	// Ensure active jobs are scheduled with the latest schedule fields.
	for _, j := range active {
		fp := fingerprint(j)
		m.mu.Lock()
		prev, ok := m.fingerprints[j.ID]
		m.mu.Unlock()

		if ok && prev == fp {
			continue
		}

		if err := m.scheduleOrReschedule(ctx, j); err != nil {
			if errors.Is(err, ErrSchedulerNotActive) {
				// Leadership can be lost during reconciliation; treat as transient and
				// let the next active leader reconcile again.
				m.logger.Debug("agent job scheduler reconcile aborted: scheduler not active")
				return
			}
			m.logger.Error("agent job scheduler reconcile: failed to schedule job",
				zap.String("agent_job_id", j.ID.String()),
				zap.Error(err),
			)
			continue
		}

		m.mu.Lock()
		m.fingerprints[j.ID] = fp
		m.mu.Unlock()
	}
}

func (m *Manager) scheduleOrReschedule(ctx context.Context, j models.AgentJob) error {
	jobKey := quartz.NewJobKey(j.ID.String())
	m.mu.RLock()
	s := m.sched
	m.mu.RUnlock()
	if s == nil {
		return ErrSchedulerNotActive
	}
	_ = s.DeleteJob(jobKey)

	trigger, err := buildTrigger(j, time.Now())
	if err != nil {
		return err
	}

	qJob := &agentJobQuartzJob{
		manager:    m,
		agentJobID: j.ID,
		userID:     j.UserID,
	}
	if err := s.ScheduleJob(quartz.NewJobDetail(qJob, jobKey), trigger); err != nil {
		return err
	}

	return nil
}

func (m *Manager) logMisfires(ch chan quartz.ScheduledJob) {
	for sj := range ch {
		if sj == nil || sj.JobDetail() == nil || sj.JobDetail().JobKey() == nil {
			continue
		}
		m.logger.Warn("agent job misfire",
			zap.String("job_key", sj.JobDetail().JobKey().String()),
			zap.Int64("next_run_time", sj.NextRunTime()),
		)
	}
}

func fingerprint(j models.AgentJob) string {
	sched := ""
	if j.Schedule != nil {
		sched = *j.Schedule
	}
	runAt := ""
	if j.RunAt != nil {
		runAt = j.RunAt.UTC().Format(time.RFC3339Nano)
	}
	return fmt.Sprintf("%s|%s|%s|%s", j.ScheduleType, sched, runAt, j.Timezone)
}
