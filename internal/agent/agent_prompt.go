package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Agent prompts (ADR 0x042): a running turn stops to ask the person something (ask_user, or an MCP
// server's elicitation) and waits for the answer. The prompt is a row the worker polls, and it is
// shown live on the turn's job progress (ChatTurnProgress.Prompt), which the client already polls.
// The answer arrives through the API, maybe on another instance: AnswerPrompt writes the row and
// wakes a waiter in this process at once; a waiter elsewhere sees it on its next poll.

const (
	// defaultAgentPromptTimeout is how long a turn waits for an answer before it gives up
	// (AGENT_PROMPT_TIMEOUT overrides it). The turn then ends asking in its reply, so a person who
	// walked away answers later as a normal message instead of holding a worker.
	defaultAgentPromptTimeout = 10 * time.Minute
	maxAgentPromptTimeout     = time.Hour
	// agentPromptPollInterval is how often a waiter re-reads its prompt.
	agentPromptPollInterval = time.Second
	// agentPromptCloseTimeout bounds the write that closes a prompt after the turn's context ended.
	agentPromptCloseTimeout = 5 * time.Second
)

// agentPromptStore is the slice of the datastore prompts need (a test hook can replace it).
type agentPromptStore interface {
	CreateAgentPrompt(ctx context.Context, p *models.AgentPrompt) (*models.AgentPrompt, error)
	GetAgentPrompt(ctx context.Context, userID, id uuid.UUID) (*models.AgentPrompt, error)
	AnswerAgentPrompt(ctx context.Context, userID, chatID, id uuid.UUID, resp models.AgentPromptResponse) (*models.AgentPrompt, error)
	CloseAgentPrompt(ctx context.Context, userID, id uuid.UUID, status models.AgentPromptStatus) (bool, error)
}

func (a *Agent) promptStore() agentPromptStore {
	if a.testHooks.PromptStore != nil {
		return a.testHooks.PromptStore
	}
	if a.ds == nil {
		return nil
	}
	return a.ds
}

// agentPromptTimeout is the wait for one prompt: AGENT_PROMPT_TIMEOUT when it is a valid positive
// duration (clamped to maxAgentPromptTimeout), else the default.
func (a *Agent) agentPromptTimeout() time.Duration {
	if a.testHooks.PromptTimeout > 0 {
		return a.testHooks.PromptTimeout
	}
	raw := strings.TrimSpace(os.Getenv("AGENT_PROMPT_TIMEOUT"))
	if raw == "" {
		return defaultAgentPromptTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		a.logger.Warn("ignoring invalid AGENT_PROMPT_TIMEOUT; using the default", zap.String("value", raw))
		return defaultAgentPromptTimeout
	}
	return min(d, maxAgentPromptTimeout)
}

// promptWaiters wakes waiters in this process when their prompt is answered here.
type promptWaiters struct {
	mu      sync.Mutex
	waiters map[uuid.UUID]chan struct{}
}

func (w *promptWaiters) register(id uuid.UUID) (<-chan struct{}, func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.waiters == nil {
		w.waiters = map[uuid.UUID]chan struct{}{}
	}
	ch := make(chan struct{}, 1)
	w.waiters[id] = ch
	return ch, func() {
		w.mu.Lock()
		delete(w.waiters, id)
		w.mu.Unlock()
	}
}

func (w *promptWaiters) wake(id uuid.UUID) {
	w.mu.Lock()
	ch := w.waiters[id]
	w.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// errNoPromptTarget: the turn has nobody to ask (a relay, a scheduled run, a sub-agent).
var errNoPromptTarget = errors.New("nobody can answer a question in this conversation right now")

// promptOutcome is how a prompt ended.
type promptOutcome struct {
	status   models.AgentPromptStatus
	response *models.AgentPromptResponse
}

// answered reports whether the person answered (with any action).
func (o promptOutcome) answered() bool {
	return o.status == models.AgentPromptAnswered && o.response != nil
}

// awaitPrompt shows payload to the person on the turn's job and waits for the answer, the prompt's
// expiry or the end of the turn (ctx). The wait is added to chatCtx.promptWaited. An expired prompt
// sets chatCtx.awaitingUser, so the turn wraps up instead of carrying on without the answer.
//
// It returns ctx's error when the turn ended while waiting (the prompt is then cancelled).
func (a *Agent) awaitPrompt(ctx context.Context, chatCtx *chatContext, source models.AgentPromptSource, toolCallID string, payload models.AgentPromptPayload) (promptOutcome, error) {
	store := a.promptStore()
	if chatCtx == nil || chatCtx.promptJob == nil || chatCtx.chat == nil || store == nil {
		return promptOutcome{}, errNoPromptTarget
	}
	job := chatCtx.promptJob
	started := time.Now()
	defer func() { chatCtx.promptWaited += time.Since(started) }()

	prompt, err := store.CreateAgentPrompt(ctx, &models.AgentPrompt{
		UserID:     job.UserID,
		ChatID:     chatCtx.chat.ID,
		JobID:      job.ID,
		Source:     source,
		ToolCallID: toolCallID,
		Payload:    payload,
		ExpiresAt:  started.Add(a.agentPromptTimeout()),
	})
	if err != nil {
		return promptOutcome{}, fmt.Errorf("could not show the question: %w", err)
	}
	wake, unregister := a.prompts.register(prompt.ID)
	defer unregister()
	chatCtx.toolProgress.SetPrompt(&models.AgentPromptView{
		ID:         prompt.ID.String(),
		Source:     source,
		Payload:    payload,
		ExpiresAt:  prompt.ExpiresAt,
		ToolCallID: toolCallID,
	})
	defer chatCtx.toolProgress.SetPrompt(nil)

	a.logger.Info("agent prompt waiting for an answer",
		zap.String("prompt_id", prompt.ID.String()),
		zap.String("source", string(source)),
		zap.String("kind", string(payload.Kind)),
		zap.String("job_id", job.ID.String()))

	ticker := time.NewTicker(agentPromptPollInterval)
	defer ticker.Stop()
	deadline := time.NewTimer(time.Until(prompt.ExpiresAt))
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			a.closePrompt(job.UserID, prompt.ID, models.AgentPromptCancelled)
			return promptOutcome{status: models.AgentPromptCancelled}, ctx.Err()
		case <-deadline.C:
			// An answer can land between the last poll and the deadline; closing is conditional,
			// so read the row again when it was no longer pending.
			if closed := a.closePrompt(job.UserID, prompt.ID, models.AgentPromptExpired); !closed {
				if out, done := a.readPrompt(ctx, store, job.UserID, prompt.ID); done {
					return out, nil
				}
			}
			chatCtx.awaitingUser = true
			return promptOutcome{status: models.AgentPromptExpired}, nil
		case <-wake:
		case <-ticker.C:
		}
		if out, done := a.readPrompt(ctx, store, job.UserID, prompt.ID); done {
			if out.status == models.AgentPromptExpired {
				chatCtx.awaitingUser = true
			}
			return out, nil
		}
	}
}

// readPrompt reports the prompt's outcome once it is no longer pending.
func (a *Agent) readPrompt(ctx context.Context, store agentPromptStore, userID, id uuid.UUID) (promptOutcome, bool) {
	p, err := store.GetAgentPrompt(ctx, userID, id)
	if err != nil {
		if ctx.Err() == nil {
			a.logger.Warn("failed to read agent prompt", zap.String("prompt_id", id.String()), zap.Error(err))
		}
		return promptOutcome{}, false
	}
	if p.Status == models.AgentPromptPending {
		return promptOutcome{}, false
	}
	return promptOutcome{status: p.Status, response: p.Response}, true
}

// closePrompt ends a pending prompt on a context of its own (the turn's may be cancelled), and
// reports whether it did (false: it was no longer pending).
func (a *Agent) closePrompt(userID, id uuid.UUID, status models.AgentPromptStatus) bool {
	store := a.promptStore()
	if store == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.lifecycleCtx), agentPromptCloseTimeout)
	defer cancel()
	closed, err := store.CloseAgentPrompt(ctx, userID, id, status)
	if err != nil {
		a.logger.Warn("failed to close agent prompt", zap.String("prompt_id", id.String()), zap.Error(err))
		return true
	}
	return closed
}

// AnswerPrompt records the person's answer to a prompt in their chat and wakes its turn when it
// waits in this process. The response is checked against what the prompt asked
// (validatePromptResponse); datastore.ErrAgentPromptNotFound and ErrAgentPromptClosed pass
// through for the handler to map.
func (a *Agent) AnswerPrompt(ctx context.Context, userID, chatID, promptID uuid.UUID, resp models.AgentPromptResponse) (*models.AgentPrompt, error) {
	store := a.promptStore()
	if store == nil {
		return nil, fmt.Errorf("prompts are not available")
	}
	existing, err := store.GetAgentPrompt(ctx, userID, promptID)
	if err != nil {
		return nil, err
	}
	if existing.ChatID != chatID {
		return nil, datastore.ErrAgentPromptNotFound
	}
	if existing.Status.Terminal() {
		return existing, datastore.ErrAgentPromptClosed
	}
	clean, err := validatePromptResponse(existing.Payload, resp)
	if err != nil {
		return nil, err
	}
	answered, err := store.AnswerAgentPrompt(ctx, userID, chatID, promptID, clean)
	if err != nil {
		return answered, err
	}
	a.prompts.wake(promptID)
	return answered, nil
}

// ErrInvalidPromptResponse wraps a response that does not fit its prompt.
var ErrInvalidPromptResponse = errors.New("invalid answer")

const maxPromptAnswerChars = 4000

// validatePromptResponse checks resp against the prompt and returns it trimmed to what the prompt
// asked for: answers only for its questions, selections only among their options, text bounded.
// A decline or cancel carries no answers or content.
func validatePromptResponse(payload models.AgentPromptPayload, resp models.AgentPromptResponse) (models.AgentPromptResponse, error) {
	if !resp.Action.Valid() {
		return resp, fmt.Errorf("%w: action must be accept, decline or cancel", ErrInvalidPromptResponse)
	}
	out := models.AgentPromptResponse{Action: resp.Action, Note: truncatePromptText(resp.Note)}
	if resp.Action != models.AgentPromptAccept {
		return out, nil
	}
	switch payload.Kind {
	case models.AgentPromptKindQuestions:
		out.Answers = map[string]models.AgentPromptAnswer{}
		for _, q := range payload.Questions {
			ans, ok := resp.Answers[q.ID]
			if !ok {
				continue
			}
			clean := models.AgentPromptAnswer{Text: truncatePromptText(ans.Text)}
			for _, sel := range ans.Selected {
				if !questionHasOption(q, sel) {
					return resp, fmt.Errorf("%w: %q is not an option of question %s", ErrInvalidPromptResponse, sel, q.ID)
				}
				clean.Selected = append(clean.Selected, sel)
			}
			if !q.MultiSelect && len(clean.Selected) > 1 {
				return resp, fmt.Errorf("%w: question %s takes one option", ErrInvalidPromptResponse, q.ID)
			}
			if len(clean.Selected) == 0 && strings.TrimSpace(clean.Text) == "" {
				continue
			}
			out.Answers[q.ID] = clean
		}
		if len(out.Answers) == 0 && !allQuestionsOptional(payload.Questions) {
			return resp, fmt.Errorf("%w: answer at least one question", ErrInvalidPromptResponse)
		}
	case models.AgentPromptKindForm:
		content, err := validateElicitationContent(payload.Schema, resp.Content)
		if err != nil {
			return resp, fmt.Errorf("%w: %w", ErrInvalidPromptResponse, err)
		}
		out.Content = content
	}
	return out, nil
}

func allQuestionsOptional(questions []models.AgentPromptQuestion) bool {
	for _, q := range questions {
		if !q.Optional {
			return false
		}
	}
	return true
}

func questionHasOption(q models.AgentPromptQuestion, label string) bool {
	for _, o := range q.Options {
		if o.Label == label {
			return true
		}
	}
	return false
}

func truncatePromptText(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxPromptAnswerChars {
		return string(r[:maxPromptAnswerChars])
	}
	return s
}

type toolUseIDKey struct{}

// withToolUseID records the provider id of the tool call a handler is running for, so a prompt it
// raises can be placed next to that call.
func withToolUseID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, toolUseIDKey{}, id)
}

func toolUseIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(toolUseIDKey{}).(string)
	return id
}

// interactivePromptJob is the job a person can answer this turn's prompts on: a web chat turn's
// own chat_message job. Relay threads (Discord, Buzz, Slack) answer elsewhere, and agent-job runs
// (scheduled, webhook, welcome) have nobody waiting, so they get nil and ask_user is not offered.
func interactivePromptJob(job *models.Job, chatCtx *chatContext) *models.Job {
	if job == nil || job.JobType != JobTypeChatMessage || chatCtx == nil || chatCtx.relayPlatform != "" {
		return nil
	}
	return job
}
