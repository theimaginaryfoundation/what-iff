package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/reugn/go-quartz/quartz"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type agentJobQuartzJob struct {
	manager            *Manager
	agentJobID         uuid.UUID
	userID             uuid.UUID
	deferredRetryCount int
	recurrenceBucket   recurrenceBucket
}

type recurrenceBucket int

const (
	recurrenceBucketUnknown recurrenceBucket = iota
	recurrenceBucketShort
	recurrenceBucketMedium
	recurrenceBucketLong
)

type executionOptions struct {
	allowPaused        bool
	deferredRetryCount int
	recurrenceHint     recurrenceBucket
}

func (m *Manager) cleanupScheduledAgentJob(agentJobID uuid.UUID) {
	m.mu.RLock()
	s := m.sched
	m.mu.RUnlock()
	if s != nil {
		_ = s.DeleteJob(quartz.NewJobKey(agentJobID.String()))
	}
	m.mu.Lock()
	delete(m.fingerprints, agentJobID)
	m.mu.Unlock()
}

func (m *Manager) recordAgentJobRunAttempt(ctx context.Context, userID, agentJobID uuid.UUID, runAt time.Time, nextRunAt *time.Time, errText string, statusUpdate *models.AgentJobStatus) {
	if _, err := m.ds.RecordAgentJobRun(ctx, userID, agentJobID, runAt, nextRunAt, errText, statusUpdate); err != nil {
		m.logger.Error("failed to record agent job run",
			zap.String("agent_job_id", agentJobID.String()),
			zap.Error(err),
		)
	}
}

func (m *Manager) resolveAgentJobChat(ctx context.Context, userID, agentJobID uuid.UUID, job *models.AgentJob) (*uuid.UUID, *models.AgentJob, error) {
	if job == nil {
		return nil, job, nil
	}
	if job.ChatID != nil {
		return job.ChatID, job, nil
	}

	chatName := "[JOB]: New Chat"
	if job.Title != nil && strings.TrimSpace(*job.Title) != "" {
		chatName = "[JOB]: " + strings.TrimSpace(*job.Title)
	}
	newChat, err := m.ds.CreateChat(ctx, userID, models.Chat{
		Name: chatName,
	})
	if err != nil {
		return nil, job, fmt.Errorf("failed to create chat for agent job: %w", err)
	}

	updated, err := m.ds.SetAgentJobChat(ctx, userID, agentJobID, &newChat.ID)
	if err != nil {
		return nil, job, fmt.Errorf("failed to set agent job chat: %w", err)
	}
	return &newChat.ID, updated, nil
}

func deriveStatusAndErrorText(job *models.AgentJob, runErr error, scheduleErrText string) (*models.AgentJobStatus, string) {
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}

	var statusUpdate *models.AgentJobStatus
	if runErr != nil && job != nil && job.ScheduleType == models.AgentJobScheduleTypeAt {
		failed := models.AgentJobStatusFailed
		statusUpdate = &failed
	}
	if runErr == nil && job != nil && job.ScheduleType == models.AgentJobScheduleTypeAt {
		complete := models.AgentJobStatusComplete
		statusUpdate = &complete
	}

	// If we couldn't compute the next run for a recurring job, make it visible and stop further runs.
	if scheduleErrText != "" {
		if errText == "" {
			errText = scheduleErrText
		} else {
			errText = errText + "\n\n" + scheduleErrText
		}
		if job != nil && job.ScheduleType == models.AgentJobScheduleTypeCron {
			paused := models.AgentJobStatusPaused
			statusUpdate = &paused
		}
	}

	return statusUpdate, errText
}

func (j *agentJobQuartzJob) Execute(ctx context.Context) error {
	if j.manager == nil {
		return nil
	}
	j.manager.executeAgentJobWithOptions(ctx, j.userID, j.agentJobID, executionOptions{
		allowPaused:        false,
		deferredRetryCount: j.deferredRetryCount,
		recurrenceHint:     j.recurrenceBucket,
	})
	return nil
}

func (j *agentJobQuartzJob) Description() string {
	return fmt.Sprintf("AgentJobQuartzJob|%s", j.agentJobID.String())
}

func (m *Manager) executeAgentJob(ctx context.Context, userID, agentJobID uuid.UUID) {
	m.executeAgentJobWithOptions(ctx, userID, agentJobID, executionOptions{
		allowPaused:        false,
		deferredRetryCount: 0,
		recurrenceHint:     recurrenceBucketUnknown,
	})
}

// RunAgentJobNow triggers an agent job execution asynchronously outside normal schedule recurrence.
func (m *Manager) RunAgentJobNow(ctx context.Context, userID, id uuid.UUID) error {
	m.mu.RLock()
	schedulerActive := m.sched != nil
	m.mu.RUnlock()
	if !schedulerActive {
		return ErrSchedulerNotActive
	}

	execCtx := context.WithoutCancel(ctx)
	go m.executeAgentJobWithOptions(execCtx, userID, id, executionOptions{
		allowPaused:        true,
		deferredRetryCount: 0,
		recurrenceHint:     recurrenceBucketUnknown,
	})
	return nil
}

func (m *Manager) executeAgentJobWithOptions(ctx context.Context, userID, agentJobID uuid.UUID, opts executionOptions) {
	m.mu.Lock()
	if m.inFlight[agentJobID] {
		m.mu.Unlock()
		m.logger.Info("skipping overlapping agent job execution", zap.String("agent_job_id", agentJobID.String()))
		return
	}
	m.inFlight[agentJobID] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.inFlight, agentJobID)
		m.mu.Unlock()
	}()

	job, err := m.ds.GetAgentJob(ctx, userID, agentJobID)
	if err != nil {
		if err == datastore.ErrAgentJobNotFound {
			return
		}
		m.logger.Error("failed to load agent job for execution",
			zap.String("agent_job_id", agentJobID.String()),
			zap.Error(err),
		)
		return
	}
	if job == nil || !isExecutableAgentJobStatus(job.Status, opts.allowPaused) {
		return
	}

	runAt := time.Now().UTC()
	nextRunAt, nextErr := computeNextRunAt(*job, runAt)
	scheduleErrText := ""
	if nextErr != nil {
		scheduleErrText = fmt.Sprintf("failed to compute next_run_at: %s", nextErr.Error())
		m.logger.Warn("failed to compute next run time",
			zap.String("agent_job_id", agentJobID.String()),
			zap.String("schedule_type", string(job.ScheduleType)),
			zap.String("timezone", job.Timezone),
			zap.String("schedule", func() string {
				if job.Schedule == nil {
					return ""
				}
				return strings.TrimSpace(*job.Schedule)
			}()),
			zap.Error(nextErr),
		)
		nextRunAt = nil
	}

	chatID, updatedJob, chatErr := m.resolveAgentJobChat(ctx, userID, agentJobID, job)
	if updatedJob != nil {
		job = updatedJob
	}

	// If we couldn't resolve the chat, record a failed run so history reflects the attempt.
	if chatErr != nil || chatID == nil {
		m.logger.Error("failed to resolve chat for agent job execution",
			zap.String("agent_job_id", agentJobID.String()),
			zap.Error(chatErr),
		)
		statusUpdate, errText := deriveStatusAndErrorText(job, chatErr, scheduleErrText)

		// One-off jobs do not have a next run.
		if job.ScheduleType == models.AgentJobScheduleTypeAt {
			nextRunAt = nil
		}
		m.recordAgentJobRunAttempt(ctx, userID, agentJobID, runAt, nextRunAt, errText, statusUpdate)

		// If we paused the job due to schedule misconfiguration, stop the in-memory schedule immediately.
		if scheduleErrText != "" && job.ScheduleType == models.AgentJobScheduleTypeCron {
			m.cleanupScheduledAgentJob(agentJobID)
		}
		// For terminal one-off outcomes, clear from scheduler quickly (reconcile will also handle).
		if statusUpdate != nil && (*statusUpdate == models.AgentJobStatusComplete || *statusUpdate == models.AgentJobStatusFailed) {
			m.cleanupScheduledAgentJob(agentJobID)
		}
		return
	}

	blocked := m.handleSchedulerCongestion(ctx, userID, agentJobID, *chatID, job, runAt, opts)
	if blocked {
		return
	}

	execCtx := apicontext.WithUserID(ctx, userID)
	execCtx = context.WithValue(execCtx, middleware.UserIDKey, userID)

	// Use the user's saved timezone for execution context (LLM "now", timestamps, etc.).
	// Fall back to the job's stored timezone, then UTC.
	userTZ := ""
	if u, err := m.ds.GetUserByID(ctx, userID); err == nil && u != nil {
		userTZ = strings.TrimSpace(u.Timezone)
	}
	if userTZ == "" {
		userTZ = strings.TrimSpace(job.Timezone)
	}
	if userTZ == "" {
		userTZ = "UTC"
	}
	execCtx = context.WithValue(execCtx, middleware.ClientTimezoneKey, userTZ)

	ritualIDs := make([]uuid.UUID, 0, len(job.Rituals))
	for _, r := range job.Rituals {
		if r != nil {
			ritualIDs = append(ritualIDs, r.ID)
		}
	}
	_, runErr := m.agent.HandleAgentJobPrompt(execCtx, *chatID, job.Prompt, job.ModelID, job.PersonalityID, ritualIDs, nil)
	statusUpdate, errText := deriveStatusAndErrorText(job, runErr, scheduleErrText)

	if runErr != nil {
		// Ensure at least one assistant message exists in the job chat.
		_, _ = m.ds.CreateChatMessage(execCtx, userID, models.ChatMessage{
			ChatID:  *chatID,
			Origin:  models.MessageOriginAssistant,
			Message: fmt.Sprintf("I tried to run your scheduled job, but it failed:\n\n%s", runErr.Error()),
		})
	}

	// One-off jobs do not have a next run.
	if job.ScheduleType == models.AgentJobScheduleTypeAt {
		nextRunAt = nil
	}

	m.recordAgentJobRunAttempt(ctx, userID, agentJobID, runAt, nextRunAt, errText, statusUpdate)

	// If we paused the job due to schedule misconfiguration, stop the in-memory schedule immediately.
	if scheduleErrText != "" && job.ScheduleType == models.AgentJobScheduleTypeCron {
		m.cleanupScheduledAgentJob(agentJobID)
	}

	// For terminal one-off outcomes, clear from scheduler quickly (reconcile will also handle).
	if statusUpdate != nil && (*statusUpdate == models.AgentJobStatusComplete || *statusUpdate == models.AgentJobStatusFailed) {
		m.cleanupScheduledAgentJob(agentJobID)
	}
}

// handleSchedulerCongestion enforces per-user sliding-window execution limits.
//
// Behavior when the window limit is hit:
//   - short recurrence (<=10m): skip immediately and post a system skip message.
//   - medium recurrence (>10m && <=20m): allow one deferred retry (+5m), then skip.
//   - long recurrence (>20m): allow two deferred retries (+5m each), then skip.
//
// It returns true when the current execution should stop (either deferred or skipped).
func (m *Manager) handleSchedulerCongestion(
	ctx context.Context,
	userID, agentJobID, chatID uuid.UUID,
	job *models.AgentJob,
	runAt time.Time,
	opts executionOptions,
) bool {
	since := runAt.Add(-schedulerExecutionWindow)
	executions, err := m.ds.CountRecentSuccessfulAgentJobExecutions(ctx, userID, since)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			m.logger.Debug("scheduler congestion check cancelled", zap.String("user_id", userID.String()))
			return true
		}
		m.logger.Warn("failed to evaluate scheduler congestion window",
			zap.String("user_id", userID.String()),
			zap.Error(err),
		)
		m.postSchedulerMessage(ctx, userID, chatID, schedulerSkipInfraMessage)
		return true
	}
	if executions < schedulerMaxExecutionsPerWindow {
		return false
	}

	bucket := opts.recurrenceHint
	if bucket == recurrenceBucketUnknown {
		derived, deriveErr := classifyRecurrenceBucket(job, runAt)
		if deriveErr != nil {
			m.logger.Warn("failed to classify recurrence bucket; defaulting to short bucket",
				zap.String("agent_job_id", agentJobID.String()),
				zap.Error(deriveErr),
			)
			bucket = recurrenceBucketShort
		} else {
			bucket = derived
		}
	}

	retryLimit := retryLimitForRecurrenceBucket(bucket)
	if opts.deferredRetryCount < retryLimit {
		nextRetryCount := opts.deferredRetryCount + 1
		if err := m.scheduleDeferredRetry(agentJobID, userID, nextRetryCount, bucket); err != nil {
			m.logger.Warn("failed to schedule deferred retry; skipping current run",
				zap.String("agent_job_id", agentJobID.String()),
				zap.Int("retry_count", nextRetryCount),
				zap.Error(err),
			)
			m.postSchedulerMessage(ctx, userID, chatID, schedulerSkipInfraMessage)
			return true
		} else {
			m.logger.Info("deferred agent job execution due to scheduler congestion",
				zap.String("agent_job_id", agentJobID.String()),
				zap.Int("retry_count", nextRetryCount),
				zap.Int("executions_in_window", executions),
			)
			return true
		}
	}

	m.postSchedulerMessage(ctx, userID, chatID, schedulerSkipMessage)
	return true
}

func (m *Manager) scheduleDeferredRetry(agentJobID, userID uuid.UUID, deferredRetryCount int, bucket recurrenceBucket) error {
	m.mu.RLock()
	s := m.sched
	m.mu.RUnlock()
	if s == nil {
		return ErrSchedulerNotActive
	}

	jobKey := deferredRetryJobKey(agentJobID, deferredRetryCount)
	_ = s.DeleteJob(jobKey)

	qJob := &agentJobQuartzJob{
		manager:            m,
		agentJobID:         agentJobID,
		userID:             userID,
		deferredRetryCount: deferredRetryCount,
		recurrenceBucket:   bucket,
	}
	trigger := quartz.NewRunOnceTrigger(schedulerRetryDelay)
	return s.ScheduleJob(quartz.NewJobDetail(qJob, jobKey), trigger)
}

func deferredRetryJobKey(agentJobID uuid.UUID, deferredRetryCount int) *quartz.JobKey {
	return quartz.NewJobKey(fmt.Sprintf("agent-job-deferred-retry:%s:%d", agentJobID.String(), deferredRetryCount))
}

func retryLimitForRecurrenceBucket(bucket recurrenceBucket) int {
	switch bucket {
	case recurrenceBucketMedium:
		return schedulerMediumRecurrenceRetryLimit
	case recurrenceBucketLong:
		return schedulerLongRecurrenceRetryLimit
	default:
		return 0
	}
}

func classifyRecurrenceBucket(job *models.AgentJob, after time.Time) (recurrenceBucket, error) {
	if job == nil {
		return recurrenceBucketShort, nil
	}
	if job.ScheduleType != models.AgentJobScheduleTypeCron {
		return recurrenceBucketShort, nil
	}

	trigger, err := buildTrigger(*job, after)
	if err != nil {
		return recurrenceBucketShort, err
	}
	interval, err := minimumTriggerInterval(trigger, after, schedulerRecurrenceSampleRuns)
	if err != nil {
		return recurrenceBucketShort, err
	}

	switch {
	case interval <= schedulerShortRecurrenceThreshold:
		return recurrenceBucketShort, nil
	case interval <= schedulerMediumRecurrenceThreshold:
		return recurrenceBucketMedium, nil
	default:
		return recurrenceBucketLong, nil
	}
}

func minimumTriggerInterval(trigger quartz.Trigger, after time.Time, samples int) (time.Duration, error) {
	if trigger == nil {
		return 0, fmt.Errorf("nil trigger")
	}
	if samples < 2 {
		samples = 2
	}

	cursor := after.UTC().UnixNano()
	var prevFire int64
	minGap := time.Duration(0)
	for i := 0; i < samples; i++ {
		nextFire, err := trigger.NextFireTime(cursor)
		if err != nil {
			return 0, err
		}
		if prevFire != 0 {
			gap := time.Duration(nextFire - prevFire)
			if minGap == 0 || gap < minGap {
				minGap = gap
			}
		}
		cursor = nextFire
		prevFire = nextFire
	}

	if minGap == 0 {
		return 0, fmt.Errorf("failed to compute trigger interval")
	}
	return minGap, nil
}

func (m *Manager) postSchedulerMessage(ctx context.Context, userID, chatID uuid.UUID, message string) {
	_, err := m.ds.CreateChatMessage(ctx, userID, models.ChatMessage{
		ChatID:                chatID,
		Origin:                models.MessageOriginAssistant,
		Message:               message,
		GenerationPersonality: schedulerSystemGenerationPersonality,
		GenerationModel:       schedulerSystemGenerationModel,
	})
	if err != nil {
		m.logger.Warn("failed to post scheduler message",
			zap.String("chat_id", chatID.String()),
			zap.Error(err),
		)
	}
}

func isExecutableAgentJobStatus(status models.AgentJobStatus, allowPaused bool) bool {
	if status == models.AgentJobStatusActive {
		return true
	}
	return allowPaused && status == models.AgentJobStatusPaused
}

func buildTrigger(j models.AgentJob, now time.Time) (quartz.Trigger, error) {
	switch j.ScheduleType {
	case models.AgentJobScheduleTypeAt:
		if j.RunAt == nil {
			return nil, fmt.Errorf("run_at is required for schedule_type=at")
		}
		delay := j.RunAt.Sub(now)
		if delay < 0 {
			delay = 0
		}
		return quartz.NewRunOnceTrigger(delay), nil

	case models.AgentJobScheduleTypeCron:
		if j.Schedule == nil || strings.TrimSpace(*j.Schedule) == "" {
			return nil, fmt.Errorf("schedule is required for schedule_type=cron")
		}

		loc := time.UTC
		if tz := strings.TrimSpace(j.Timezone); tz != "" {
			if loaded, err := time.LoadLocation(tz); err == nil && loaded != nil {
				loc = loaded
			}
		}
		return quartz.NewCronTriggerWithLoc(strings.TrimSpace(*j.Schedule), loc)

	default:
		return nil, fmt.Errorf("unsupported schedule_type %q", j.ScheduleType)
	}
}

func computeNextRunAt(j models.AgentJob, after time.Time) (*time.Time, error) {
	if j.Status != models.AgentJobStatusActive {
		return nil, nil
	}

	switch j.ScheduleType {
	case models.AgentJobScheduleTypeAt:
		return nil, nil
	case models.AgentJobScheduleTypeCron:
		if j.Schedule == nil || strings.TrimSpace(*j.Schedule) == "" {
			return nil, fmt.Errorf("schedule is required for schedule_type=cron")
		}
		loc := time.UTC
		if tz := strings.TrimSpace(j.Timezone); tz != "" {
			if loaded, err := time.LoadLocation(tz); err == nil && loaded != nil {
				loc = loaded
			}
		}
		trigger, err := quartz.NewCronTriggerWithLoc(strings.TrimSpace(*j.Schedule), loc)
		if err != nil {
			return nil, err
		}
		nextNs, err := trigger.NextFireTime(after.UTC().UnixNano())
		if err != nil {
			return nil, err
		}
		next := time.Unix(0, nextNs).UTC()
		return &next, nil
	default:
		return nil, fmt.Errorf("unsupported schedule_type %q", j.ScheduleType)
	}
}
