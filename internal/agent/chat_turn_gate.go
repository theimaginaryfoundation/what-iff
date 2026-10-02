package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Per-chat turn serialization (issue #254).
//
// Every turn in a chat (a chat_message job, or an agent_job_run job from a webhook or the
// scheduler) waits, before it builds its context, until no OLDER non-terminal turn job exists for
// the same chat. Jobs are ordered by (created_at, id) as stored, so every API instance agrees on
// the order without coordinating: this is job-ordered single-flight. Nothing is held while a turn
// runs (no advisory lock, no pinned connection — a turn can take minutes and the pool is shared);
// waiting is a short read polled with backoff, and a turn finishing in this process wakes local
// waiters at once.
//
// An older job that has not been touched for chatTurnStaleAfter is treated as dead (its worker
// went away with a crashed instance) and does not block; the startup reaper and Stop clean such
// jobs up. A turn that waits longer than chatTurnWaitTimeout fails with ErrChatTurnWaitTimeout
// rather than running concurrently.

// ErrChatTurnWaitTimeout is returned when a turn gave up waiting for earlier turns in its chat.
var ErrChatTurnWaitTimeout = errors.New("timed out waiting for an earlier turn in this chat to finish")

var (
	// chatTurnWaitTimeout bounds how long a turn queues behind earlier ones. It must stay below
	// chatTurnStaleAfter, so a turn still legitimately queued never looks dead to the turns
	// queued behind it.
	chatTurnWaitTimeout = 15 * time.Minute
	// chatTurnStaleAfter matches the startup reaper's threshold for chat jobs (server.go): no
	// live turn goes this long without writing to its job row.
	chatTurnStaleAfter = 30 * time.Minute
	// Poll backoff while queued: starts fast for the common short wait, caps to keep the read
	// rate low across a long one.
	chatTurnPollInitial = 250 * time.Millisecond
	chatTurnPollMax     = 2 * time.Second
)

// turnStageTurnQueueWait times a turn that actually queued behind an earlier one, so its count is
// the queued-turn rate (like turnStageRehydrationWait).
const turnStageTurnQueueWait = "turn_queue_wait"

// chatTurnStore is the slice of the datastore the turn gate uses.
type chatTurnStore interface {
	ListActiveTurnJobsForChat(ctx context.Context, userID, chatID uuid.UUID) ([]*models.Job, error)
	CreateJob(ctx context.Context, userID uuid.UUID, job models.Job) (*models.Job, error)
	UpdateJobStatus(ctx context.Context, userID, id uuid.UUID, status models.JobStatus, errorMsg string) (*models.Job, error)
	JobStatus(ctx context.Context, userID, jobID uuid.UUID) (models.JobStatus, error)
}

// turnSignal wakes waiters when any turn in this process ends. The zero value is ready to use.
type turnSignal struct {
	mu sync.Mutex
	ch chan struct{}
}

// wait returns a channel closed by the next broadcast. Take it BEFORE checking the condition, so
// a broadcast between the check and the wait is not missed.
func (s *turnSignal) wait() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch == nil {
		s.ch = make(chan struct{})
	}
	return s.ch
}

func (s *turnSignal) broadcast() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch != nil {
		close(s.ch)
		s.ch = nil
	}
}

// chatTurnGate waits for a turn's predecessors. Its fields are the tunables, so tests can shrink
// them without touching package state.
type chatTurnGate struct {
	store       chatTurnStore
	logger      *zap.Logger
	signal      *turnSignal
	timeout     time.Duration
	staleAfter  time.Duration
	pollInitial time.Duration
	pollMax     time.Duration
}

// chatTurnGate returns the gate for this agent, or nil when there is no datastore to order turns
// by (unit tests built on a bare Agent).
func (a *Agent) chatTurnGate() *chatTurnGate {
	var store chatTurnStore
	switch {
	case a.testHooks.ChatTurnStore != nil:
		store = a.testHooks.ChatTurnStore
	case a.ds != nil:
		store = a.ds
	default:
		return nil
	}
	return &chatTurnGate{
		store:       store,
		logger:      a.logger,
		signal:      &a.turnEnded,
		timeout:     chatTurnWaitTimeout,
		staleAfter:  chatTurnStaleAfter,
		pollInitial: chatTurnPollInitial,
		pollMax:     chatTurnPollMax,
	}
}

// awaitChatTurn blocks until job is the oldest live turn in chatID. The returned release must be
// called once the turn has reached a terminal status; it wakes turns queued in this process.
func (a *Agent) awaitChatTurn(ctx context.Context, job *models.Job, chatID uuid.UUID) (release func(), err error) {
	g := a.chatTurnGate()
	if g == nil || job == nil {
		return func() {}, nil
	}
	waited, err := g.wait(ctx, job, chatID)
	if waited > 0 {
		a.recordTurnStage(ctx, turnStageTurnQueueWait, waited)
	}
	if err != nil {
		return nil, err
	}
	return g.signal.broadcast, nil
}

// awaitUserChatTurn is awaitChatTurn for handleUserMessage, which owns its job's status. Its
// release also makes sure the job is terminal: a job the worker left non-terminal (a failed final
// status write) would otherwise hold up every later turn in the chat until it went stale.
func (a *Agent) awaitUserChatTurn(ctx context.Context, job *models.Job, chatID uuid.UUID) (func(), error) {
	wake, err := a.awaitChatTurn(ctx, job, chatID)
	if err != nil {
		return nil, err
	}
	return func() {
		a.finishAbandonedTurnJob(job)
		wake()
	}, nil
}

// finishAbandonedTurnJob marks job terminal when its turn ended without doing so: complete when
// it already produced its reply (ResultID set at inference_complete), failed otherwise. A job the
// database already shows terminal (e.g. cancelled from another instance) is left alone.
func (a *Agent) finishAbandonedTurnJob(job *models.Job) {
	g := a.chatTurnGate()
	if g == nil || job == nil || isTerminalJobStatus(job.Status) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), jobTerminalPersistTimeout)
	defer cancel()
	current, err := g.store.JobStatus(ctx, job.UserID, job.ID)
	if err != nil || isTerminalJobStatus(current) {
		return
	}
	status, msg := models.JobStatusFailed, "turn ended without a final status"
	if job.ResultID != nil {
		status, msg = models.JobStatusComplete, ""
	}
	updated, err := g.store.UpdateJobStatus(ctx, job.UserID, job.ID, status, msg)
	if err != nil {
		a.logger.Error("failed to finish chat job left non-terminal", zap.String("job_id", job.ID.String()), zap.Error(err))
		return
	}
	a.logger.Warn("chat job ended its turn non-terminal; finished it so later turns are not held up",
		zap.String("job_id", job.ID.String()), zap.String("was", string(current)), zap.String("status", string(status)))
	job.Status = updated.Status
}

// failChatTurnWait records why a chat_message turn never started: cancelled when its context
// ended (Stop), failed otherwise (the wait timed out), which also flags the user message so the
// thread offers a retry.
func (a *Agent) failChatTurnWait(ctx context.Context, job *models.Job, cause error) {
	if ctx.Err() != nil {
		persistCtx, cancel := context.WithTimeout(context.Background(), jobTerminalPersistTimeout)
		defer cancel()
		if _, err := a.ds.UpdateJobStatus(persistCtx, job.UserID, job.ID, models.JobStatusCancelled, ""); err != nil {
			a.logger.Error("failed to mark queued chat job cancelled", zap.String("job_id", job.ID.String()), zap.Error(err))
		}
		return
	}
	a.setJobStatusFailed(ctx, job, cause)
}

// beginEphemeralChatTurn takes this agent-job/webhook turn's place in its chat's queue and waits
// for it. A tracked turn (async webhook) queues as its own job, whose status the caller owns. An
// untracked one (a scheduled run) has no job row, so a ticket job is created to hold its place
// and is finished by end. end must be called with the turn's outcome once the turn is over.
func (a *Agent) beginEphemeralChatTurn(ctx context.Context, userID, chatID uuid.UUID, trackingJob *models.Job) (end func(error), err error) {
	if trackingJob != nil {
		release, err := a.awaitChatTurn(ctx, trackingJob, chatID)
		if err != nil {
			return nil, err
		}
		return func(error) { release() }, nil
	}
	g := a.chatTurnGate()
	if g == nil {
		return func(error) {}, nil
	}
	ticket, err := g.store.CreateJob(ctx, userID, models.Job{
		JobType:     JobTypeAgentJobRun,
		Reference:   chatID.String(),
		Status:      models.JobStatusPending,
		DraftDeltas: []string{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to queue turn: %w", err)
	}
	finish := func(outcome error) {
		status, msg := models.JobStatusComplete, ""
		switch {
		case outcome == nil:
		case errors.Is(outcome, context.Canceled):
			status = models.JobStatusCancelled
		default:
			status, msg = models.JobStatusFailed, outcome.Error()
		}
		persistCtx, cancel := context.WithTimeout(context.Background(), jobTerminalPersistTimeout)
		defer cancel()
		if _, uerr := g.store.UpdateJobStatus(persistCtx, userID, ticket.ID, status, msg); uerr != nil {
			a.logger.Error("failed to finish turn ticket job", zap.String("job_id", ticket.ID.String()), zap.Error(uerr))
		}
	}
	release, err := a.awaitChatTurn(ctx, ticket, chatID)
	if err != nil {
		finish(err)
		return nil, err
	}
	if _, uerr := g.store.UpdateJobStatus(ctx, userID, ticket.ID, models.JobStatusProcessing, ""); uerr != nil {
		a.logger.Warn("failed to mark turn ticket job processing", zap.String("job_id", ticket.ID.String()), zap.Error(uerr))
	}
	return func(outcome error) {
		finish(outcome)
		release()
	}, nil
}

// wait polls until no older live turn job remains for chatID, returning how long it queued
// (zero when it never had to).
func (g *chatTurnGate) wait(ctx context.Context, self *models.Job, chatID uuid.UUID) (time.Duration, error) {
	start := time.Now()
	deadline := start.Add(g.timeout)
	delay := g.pollInitial
	queued := false
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return queuedFor(queued, start), err
		}
		wake := g.signal.wait()
		jobs, err := g.store.ListActiveTurnJobsForChat(ctx, self.UserID, chatID)
		if err != nil {
			if ctx.Err() != nil {
				return queuedFor(queued, start), ctx.Err()
			}
			lastErr = err
			g.logger.Warn("chat turn gate: failed to list active turns; retrying",
				zap.String("job_id", self.ID.String()), zap.String("chat_id", chatID.String()), zap.Error(err))
		} else {
			lastErr = nil
			blockers := g.blockingTurnJobs(jobs, self, time.Now())
			if len(blockers) == 0 {
				if queued {
					g.logger.Info("chat turn gate: earlier turns finished; proceeding",
						zap.String("job_id", self.ID.String()), zap.String("chat_id", chatID.String()),
						zap.Duration("waited", time.Since(start)))
				}
				return queuedFor(queued, start), nil
			}
			if !queued {
				queued = true
				g.logger.Info("chat turn gate: queued behind earlier turns in this chat",
					zap.String("job_id", self.ID.String()), zap.String("chat_id", chatID.String()),
					zap.Int("ahead", len(blockers)), zap.String("next_job_id", blockers[0].ID.String()))
			}
		}
		if !time.Now().Before(deadline) {
			err := fmt.Errorf("%w (waited %s)", ErrChatTurnWaitTimeout, g.timeout)
			if lastErr != nil {
				err = fmt.Errorf("%w; last error: %w", err, lastErr)
			}
			g.logger.Warn("chat turn gate: gave up waiting for earlier turns",
				zap.String("job_id", self.ID.String()), zap.String("chat_id", chatID.String()))
			return queuedFor(queued, start), err
		}
		timer := time.NewTimer(min(delay, time.Until(deadline)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return queuedFor(queued, start), ctx.Err()
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
		delay = min(delay*2, g.pollMax)
	}
}

func queuedFor(queued bool, start time.Time) time.Duration {
	if !queued {
		return 0
	}
	return time.Since(start)
}

// blockingTurnJobs returns the live jobs in jobs that self must wait for: non-terminal, ordered
// before self, and not stale. self's own row in jobs (when present) supplies its stored
// created_at, so both sides of the comparison use the database's precision.
func (g *chatTurnGate) blockingTurnJobs(jobs []*models.Job, self *models.Job, now time.Time) []*models.Job {
	selfCreated := self.CreatedAt
	for _, j := range jobs {
		if j != nil && j.ID == self.ID {
			selfCreated = j.CreatedAt
			break
		}
	}
	var out []*models.Job
	for _, j := range jobs {
		if j == nil || j.ID == self.ID || isTerminalJobStatus(j.Status) {
			continue
		}
		if !turnJobBefore(j.CreatedAt, j.ID, selfCreated, self.ID) {
			continue
		}
		if g.staleAfter > 0 && now.Sub(j.UpdatedAt) > g.staleAfter {
			g.logger.Debug("chat turn gate: ignoring stale earlier turn",
				zap.String("job_id", self.ID.String()), zap.String("stale_job_id", j.ID.String()),
				zap.Time("updated_at", j.UpdatedAt))
			continue
		}
		out = append(out, j)
	}
	return out
}

// turnJobBefore reports whether job a precedes job b in turn order: created first, ties broken
// by id.
func turnJobBefore(aCreated time.Time, aID uuid.UUID, bCreated time.Time, bID uuid.UUID) bool {
	if !aCreated.Equal(bCreated) {
		return aCreated.Before(bCreated)
	}
	return aID.String() < bID.String()
}

func isTerminalJobStatus(s models.JobStatus) bool {
	return s == models.JobStatusComplete || s == models.JobStatusCancelled || s == models.JobStatusFailed
}
