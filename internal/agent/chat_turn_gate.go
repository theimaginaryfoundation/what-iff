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
// scheduler) waits, before it builds its context, until no OLDER turn job in the same chat is
// still pending or processing. Jobs are ordered by (created_at, id) as stored, so every API
// instance agrees on the order without coordinating: this is job-ordered single-flight. Nothing
// is held while a turn runs (no advisory lock, no pinned connection — a turn can take minutes and
// the pool is shared); waiting is a short indexed read polled with backoff, and a turn in this
// process reaching its reply wakes that chat's local waiters at once.
//
// The gate opens at inference_complete, not at the end of the turn: that is when the reply and the
// chat's response chain are saved (persistInferencePhase writes the chat first) and when the web
// client unlocks its composer. The earlier turn's expression, checkpoint and memory work then
// overlaps the next turn; the checkpoint's scratchpad write is conditional (scratchpad_commit.go).
//
// Every live turn heartbeats its job row (chatTurnHeartbeatInterval), queued or running, so a job
// whose worker died stops blocking after chatTurnStaleAfter. On shutdown the process finishes its
// in-flight turn jobs (FailInFlightTurns); the startup reaper and Stop clean up the rest. A turn
// that waits longer than chatTurnWaitTimeout fails with ErrChatTurnWaitTimeout rather than run
// concurrently.

// ErrChatTurnWaitTimeout is returned when a turn gave up waiting for earlier turns in its chat.
var ErrChatTurnWaitTimeout = errors.New("timed out waiting for an earlier turn in this chat to finish")

// errQueuedTurnCancelled is returned when a turn's job was cancelled (e.g. Stop on another
// instance) while it was queued. It wraps context.Canceled so callers record a cancellation.
var errQueuedTurnCancelled = fmt.Errorf("turn cancelled while queued: %w", context.Canceled)

var (
	// chatTurnHeartbeatInterval is how often a live turn refreshes its job's updated_at.
	chatTurnHeartbeatInterval = 30 * time.Second
	// chatTurnStaleAfter is how long a pending/processing job may go without a write before the
	// gate treats its worker as dead: four missed heartbeats.
	chatTurnStaleAfter = 2 * time.Minute
	// chatTurnWaitTimeout bounds how long a turn queues behind live earlier turns (a dead one stops
	// blocking after chatTurnStaleAfter). Queued turns heartbeat too, so this need not stay below
	// the stale bound; it only has to cover a few long inferences ahead in the queue.
	chatTurnWaitTimeout = 10 * time.Minute
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
	ListPendingTurnJobsForChat(ctx context.Context, userID, chatID, excludeJobID uuid.UUID) ([]*models.Job, error)
	CreateJob(ctx context.Context, userID uuid.UUID, job models.Job) (*models.Job, error)
	UpdateJobStatus(ctx context.Context, userID, id uuid.UUID, status models.JobStatus, errorMsg string) (*models.Job, error)
	JobStatus(ctx context.Context, userID, jobID uuid.UUID) (models.JobStatus, error)
	TouchJob(ctx context.Context, userID, id uuid.UUID) error
}

// blocksLaterTurns reports whether a turn job in this status holds up later turns in its chat.
func blocksLaterTurns(s models.JobStatus) bool {
	return s == models.JobStatusPending || s == models.JobStatusProcessing
}

// chatTurnTracker is this process's view of its live turns: per-chat wake channels for queued
// waiters, and the in-flight turn jobs (for shutdown). The zero value is ready to use.
type chatTurnTracker struct {
	mu       sync.Mutex
	wake     map[uuid.UUID]chan struct{} // chat id -> closed when a turn there moves on
	inFlight map[uuid.UUID]*inFlightTurn // job id -> turn
}

type inFlightTurn struct {
	userID, chatID uuid.UUID
	// replied is set once the job is past inference_complete (its reply is saved).
	replied bool
}

// waitCh returns a channel closed by the next wakeChat(chatID). Take it BEFORE checking the
// condition, so a wake between the check and the wait is not missed.
func (t *chatTurnTracker) waitCh(chatID uuid.UUID) <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.wake == nil {
		t.wake = make(map[uuid.UUID]chan struct{})
	}
	ch, ok := t.wake[chatID]
	if !ok {
		ch = make(chan struct{})
		t.wake[chatID] = ch
	}
	return ch
}

// wakeChat wakes the waiters queued in chatID (only that chat's).
func (t *chatTurnTracker) wakeChat(chatID uuid.UUID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ch, ok := t.wake[chatID]; ok {
		close(ch)
		delete(t.wake, chatID)
	}
}

func (t *chatTurnTracker) add(jobID, userID, chatID uuid.UUID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inFlight == nil {
		t.inFlight = make(map[uuid.UUID]*inFlightTurn)
	}
	t.inFlight[jobID] = &inFlightTurn{userID: userID, chatID: chatID}
}

func (t *chatTurnTracker) remove(jobID uuid.UUID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.inFlight, jobID)
}

// markReplied records that jobID is past inference_complete and returns its chat, if tracked.
func (t *chatTurnTracker) markReplied(jobID uuid.UUID) (uuid.UUID, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	turn, ok := t.inFlight[jobID]
	if !ok {
		return uuid.Nil, false
	}
	turn.replied = true
	return turn.chatID, true
}

func (t *chatTurnTracker) snapshot() map[uuid.UUID]inFlightTurn {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[uuid.UUID]inFlightTurn, len(t.inFlight))
	for id, turn := range t.inFlight {
		out[id] = *turn
	}
	return out
}

// chatTurnGate waits for a turn's predecessors. Its fields are the tunables, so tests can shrink
// them without touching package state.
type chatTurnGate struct {
	store             chatTurnStore
	logger            *zap.Logger
	tracker           *chatTurnTracker
	timeout           time.Duration
	staleAfter        time.Duration
	pollInitial       time.Duration
	pollMax           time.Duration
	heartbeatInterval time.Duration
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
		store:             store,
		logger:            a.logger,
		tracker:           &a.turns,
		timeout:           chatTurnWaitTimeout,
		staleAfter:        chatTurnStaleAfter,
		pollInitial:       chatTurnPollInitial,
		pollMax:           chatTurnPollMax,
		heartbeatInterval: chatTurnHeartbeatInterval,
	}
}

// awaitChatTurn blocks until no earlier turn in chatID is still before its reply. From the start
// of the wait until release, job is tracked as in flight and heartbeats. release must be called
// once the turn is over; it also wakes turns queued in this chat.
func (a *Agent) awaitChatTurn(ctx context.Context, job *models.Job, chatID uuid.UUID) (release func(), err error) {
	g := a.chatTurnGate()
	if g == nil || job == nil {
		return func() {}, nil
	}
	return g.acquire(ctx, job, chatID, func(waited time.Duration) {
		a.recordTurnStage(ctx, turnStageTurnQueueWait, waited)
	})
}

// acquire is awaitChatTurn on the gate; recordWait is called with the queued time when the turn
// had to wait.
func (g *chatTurnGate) acquire(ctx context.Context, job *models.Job, chatID uuid.UUID, recordWait func(time.Duration)) (func(), error) {
	g.tracker.add(job.ID, job.UserID, chatID)
	hbCtx, stopHeartbeat := context.WithCancel(context.WithoutCancel(ctx))
	go g.heartbeat(hbCtx, job)
	release := func() {
		stopHeartbeat()
		g.tracker.remove(job.ID)
		g.tracker.wakeChat(chatID)
	}

	waited, err := g.wait(ctx, job, chatID)
	if waited > 0 && recordWait != nil {
		recordWait(waited)
	}
	if err == nil && waited > 0 {
		// A queued job may have been cancelled from another instance, which has no way to stop a
		// worker here that is not yet running a turn.
		if st, serr := g.store.JobStatus(ctx, job.UserID, job.ID); serr == nil && st == models.JobStatusCancelled {
			err = errQueuedTurnCancelled
		}
	}
	if err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// heartbeat refreshes the job's updated_at until ctx ends, so other instances see a live turn.
func (g *chatTurnGate) heartbeat(ctx context.Context, job *models.Job) {
	if g.heartbeatInterval <= 0 {
		return
	}
	ticker := time.NewTicker(g.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := g.store.TouchJob(ctx, job.UserID, job.ID); err != nil && ctx.Err() == nil {
				g.logger.Warn("chat turn heartbeat failed", zap.String("job_id", job.ID.String()), zap.Error(err))
			}
		}
	}
}

// noteTurnJobStatus is called after a turn job's status changes. Once the job no longer blocks
// later turns (it reached inference_complete or beyond), it wakes the turns queued in its chat in
// this process instead of leaving them to their next poll.
func (a *Agent) noteTurnJobStatus(job *models.Job) {
	if job == nil || blocksLaterTurns(job.Status) {
		return
	}
	if chatID, ok := a.turns.markReplied(job.ID); ok {
		a.turns.wakeChat(chatID)
	}
}

// awaitUserChatTurn is awaitChatTurn for handleUserMessage, which owns its job's status. Its
// release also makes sure the job is terminal: a job the worker left non-terminal (a failed final
// status write) is otherwise left for Stop or the startup reaper.
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
	a.logger.Warn("chat job ended its turn non-terminal; finished it",
		zap.String("job_id", job.ID.String()), zap.String("was", string(current)), zap.String("status", string(status)))
	job.Status = updated.Status
}

// FailInFlightTurns finishes the turn jobs this process is running or queueing, for a shutdown
// that will not let them complete: a turn that already saved its reply is marked complete, any
// other failed, so neither the client nor later turns in the chat wait on a worker that is gone.
// Jobs the database already shows terminal are left alone. Best effort: errors are logged.
func (a *Agent) FailInFlightTurns(ctx context.Context) {
	g := a.chatTurnGate()
	if g == nil {
		return
	}
	turns := g.tracker.snapshot()
	finished := 0
	for jobID, turn := range turns {
		current, err := g.store.JobStatus(ctx, turn.userID, jobID)
		if err != nil || isTerminalJobStatus(current) {
			continue
		}
		status, msg := models.JobStatusFailed, "Interrupted by a server shutdown"
		if turn.replied || !blocksLaterTurns(current) {
			status, msg = models.JobStatusComplete, ""
		}
		if _, err := g.store.UpdateJobStatus(ctx, turn.userID, jobID, status, msg); err != nil {
			a.logger.Warn("shutdown: failed to finish in-flight turn job", zap.String("job_id", jobID.String()), zap.Error(err))
			continue
		}
		finished++
	}
	if finished > 0 {
		a.logger.Info("shutdown: finished in-flight turn jobs", zap.Int("count", finished))
	}
}

// failChatTurnWait records why a chat_message turn never started: cancelled when it was stopped
// (its context ended, or its job was cancelled while queued), failed otherwise (the wait timed
// out), which also flags the user message so the thread offers a retry.
func (a *Agent) failChatTurnWait(ctx context.Context, job *models.Job, cause error) {
	if errors.Is(cause, context.Canceled) || ctx.Err() != nil {
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

// wait polls until no earlier live turn remains in chatID, returning how long it queued (zero
// when it never had to).
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
		wake := g.tracker.waitCh(chatID)
		jobs, err := g.store.ListPendingTurnJobsForChat(ctx, self.UserID, chatID, self.ID)
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
					g.logger.Info("chat turn gate: earlier turns replied; proceeding",
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

// blockingTurnJobs returns the jobs in jobs that self must wait for: still before their reply,
// ordered before self, and not stale.
func (g *chatTurnGate) blockingTurnJobs(jobs []*models.Job, self *models.Job, now time.Time) []*models.Job {
	var out []*models.Job
	for _, j := range jobs {
		if j == nil || j.ID == self.ID || !blocksLaterTurns(j.Status) {
			continue
		}
		if !turnJobBefore(j.CreatedAt, j.ID, self.CreatedAt, self.ID) {
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
// by id. Times compare at microseconds, the database's precision, so a job's in-memory
// created_at (nanoseconds) orders the same way as its stored one.
func turnJobBefore(aCreated time.Time, aID uuid.UUID, bCreated time.Time, bID uuid.UUID) bool {
	aCreated, bCreated = aCreated.Round(time.Microsecond), bCreated.Round(time.Microsecond)
	if !aCreated.Equal(bCreated) {
		return aCreated.Before(bCreated)
	}
	return aID.String() < bID.String()
}

func isTerminalJobStatus(s models.JobStatus) bool {
	return s == models.JobStatusComplete || s == models.JobStatusCancelled || s == models.JobStatusFailed
}
