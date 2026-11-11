package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// memPromptStore is an in-memory agentPromptStore.
type memPromptStore struct {
	mu      sync.Mutex
	prompts map[uuid.UUID]*models.AgentPrompt
	created chan *models.AgentPrompt
}

func newMemPromptStore() *memPromptStore {
	return &memPromptStore{prompts: map[uuid.UUID]*models.AgentPrompt{}, created: make(chan *models.AgentPrompt, 8)}
}

func (s *memPromptStore) CreateAgentPrompt(_ context.Context, p *models.AgentPrompt) (*models.AgentPrompt, error) {
	s.mu.Lock()
	cp := *p
	cp.ID = uuid.New()
	cp.Status = models.AgentPromptPending
	cp.CreatedAt = time.Now()
	s.prompts[cp.ID] = &cp
	s.mu.Unlock()
	out := cp
	s.created <- &out
	return &out, nil
}

func (s *memPromptStore) GetAgentPrompt(_ context.Context, userID, id uuid.UUID) (*models.AgentPrompt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prompts[id]
	if !ok || p.UserID != userID {
		return nil, datastore.ErrAgentPromptNotFound
	}
	cp := *p
	return &cp, nil
}

func (s *memPromptStore) AnswerAgentPrompt(_ context.Context, userID, chatID, id uuid.UUID, resp models.AgentPromptResponse) (*models.AgentPrompt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prompts[id]
	if !ok || p.UserID != userID || p.ChatID != chatID {
		return nil, datastore.ErrAgentPromptNotFound
	}
	if p.Status != models.AgentPromptPending || time.Now().After(p.ExpiresAt) {
		return p, datastore.ErrAgentPromptClosed
	}
	p.Status = models.AgentPromptAnswered
	p.Response = &resp
	cp := *p
	return &cp, nil
}

func (s *memPromptStore) CloseAgentPrompt(_ context.Context, userID, id uuid.UUID, status models.AgentPromptStatus) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prompts[id]
	if !ok || p.UserID != userID || p.Status != models.AgentPromptPending {
		return false, nil
	}
	p.Status = status
	return true, nil
}

func promptTestAgent(store *memPromptStore, timeout time.Duration) *Agent {
	return &Agent{
		logger:       zap.NewNop(),
		lifecycleCtx: context.Background(),
		testHooks:    agentTestHooks{PromptStore: store, PromptTimeout: timeout},
	}
}

func promptTestChatCtx() *chatContext {
	userID := uuid.New()
	return &chatContext{
		userID:    userID,
		chat:      &models.Chat{ID: uuid.New(), UserID: userID},
		promptJob: &models.Job{ID: uuid.New(), UserID: userID, JobType: JobTypeChatMessage},
	}
}

func askInput(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

func TestAskUserTool_ReturnsTheAnswer(t *testing.T) {
	store := newMemPromptStore()
	a := promptTestAgent(store, time.Minute)
	chatCtx := promptTestChatCtx()
	input := askInput(t, map[string]any{"questions": []map[string]any{{
		"question": "Which format?",
		"options":  []map[string]any{{"label": "PDF"}, {"label": "Markdown"}},
	}}})

	done := make(chan string, 1)
	go func() {
		out, err := a.askUserTool(withToolUseID(context.Background(), "call_1"), chatCtx, input)
		assert.NoError(t, err)
		done <- out
	}()
	p := <-store.created
	time.Sleep(20 * time.Millisecond) // so the wait is measurable on coarse clocks
	assert.Equal(t, "call_1", p.ToolCallID)
	assert.Equal(t, models.AgentPromptSourceAgent, p.Source)
	_, err := a.AnswerPrompt(context.Background(), chatCtx.userID, chatCtx.chat.ID, p.ID, models.AgentPromptResponse{
		Action:  models.AgentPromptAccept,
		Answers: map[string]models.AgentPromptAnswer{"q1": {Selected: []string{"Markdown"}}},
	})
	require.NoError(t, err)

	select {
	case out := <-done:
		assert.JSONEq(t, `{"status":"answered","answers":[{"question":"Which format?","answer":"Markdown"}]}`, out)
	case <-time.After(5 * time.Second):
		t.Fatal("ask_user did not return after the answer")
	}
	assert.False(t, chatCtx.awaitingUser)
	assert.Positive(t, chatCtx.promptWaited)
}

func TestAskUserTool_ExpiryAsksTheModelToEndItsTurn(t *testing.T) {
	store := newMemPromptStore()
	a := promptTestAgent(store, 50*time.Millisecond)
	chatCtx := promptTestChatCtx()

	out, err := a.askUserTool(context.Background(), chatCtx, askInput(t, map[string]any{"approval": map[string]any{"action": "Delete the old drafts"}}))
	require.NoError(t, err)
	assert.Contains(t, out, `"status":"no_answer"`)
	assert.True(t, chatCtx.awaitingUser)
	p := <-store.created
	got, _ := store.GetAgentPrompt(context.Background(), chatCtx.userID, p.ID)
	assert.Equal(t, models.AgentPromptExpired, got.Status)
}

func TestAskUserTool_StopCancelsThePrompt(t *testing.T) {
	store := newMemPromptStore()
	a := promptTestAgent(store, time.Minute)
	chatCtx := promptTestChatCtx()
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := a.askUserTool(ctx, chatCtx, askInput(t, map[string]any{"approval": map[string]any{"action": "Post it"}}))
		errCh <- err
	}()
	p := <-store.created
	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
	got, _ := store.GetAgentPrompt(context.Background(), chatCtx.userID, p.ID)
	assert.Equal(t, models.AgentPromptCancelled, got.Status)

	_, err := a.AnswerPrompt(context.Background(), chatCtx.userID, chatCtx.chat.ID, p.ID, models.AgentPromptResponse{Action: models.AgentPromptAccept})
	assert.ErrorIs(t, err, datastore.ErrAgentPromptClosed, "a cancelled prompt takes no answer")
}

func TestAskUserTool_NoPromptJobRefuses(t *testing.T) {
	a := promptTestAgent(newMemPromptStore(), time.Minute)
	chatCtx := promptTestChatCtx()
	chatCtx.promptJob = nil
	_, err := a.askUserTool(context.Background(), chatCtx, askInput(t, map[string]any{"approval": map[string]any{"action": "x"}}))
	assert.ErrorIs(t, err, errNoPromptTarget)
}

func TestAnswerPrompt_WrongChatIsNotFound(t *testing.T) {
	store := newMemPromptStore()
	a := promptTestAgent(store, time.Minute)
	chatCtx := promptTestChatCtx()
	go func() {
		_, _ = a.awaitPrompt(context.Background(), chatCtx, models.AgentPromptSourceAgent, "", models.AgentPromptPayload{Kind: models.AgentPromptKindApproval, Title: "x"})
	}()
	p := <-store.created
	_, err := a.AnswerPrompt(context.Background(), chatCtx.userID, uuid.New(), p.ID, models.AgentPromptResponse{Action: models.AgentPromptAccept})
	assert.ErrorIs(t, err, datastore.ErrAgentPromptNotFound)
	_, err = a.AnswerPrompt(context.Background(), uuid.New(), chatCtx.chat.ID, p.ID, models.AgentPromptResponse{Action: models.AgentPromptAccept})
	assert.ErrorIs(t, err, datastore.ErrAgentPromptNotFound, "another account cannot answer")
	_, _ = a.AnswerPrompt(context.Background(), chatCtx.userID, chatCtx.chat.ID, p.ID, models.AgentPromptResponse{Action: models.AgentPromptCancel})
}

func TestValidatePromptResponse(t *testing.T) {
	questions := models.AgentPromptPayload{Kind: models.AgentPromptKindQuestions, Questions: []models.AgentPromptQuestion{
		{ID: "q1", Question: "Pick one", Options: []models.AgentPromptOption{{Label: "A"}, {Label: "B"}}},
		{ID: "q2", Question: "Pick many", Options: []models.AgentPromptOption{{Label: "X"}, {Label: "Y"}}, MultiSelect: true},
		{ID: "q3", Question: "Say anything"},
	}}
	tests := []struct {
		name    string
		payload models.AgentPromptPayload
		resp    models.AgentPromptResponse
		wantErr bool
		check   func(t *testing.T, got models.AgentPromptResponse)
	}{
		{name: "bad action", payload: questions, resp: models.AgentPromptResponse{Action: "maybe"}, wantErr: true},
		{name: "unknown option", payload: questions, resp: models.AgentPromptResponse{Action: models.AgentPromptAccept,
			Answers: map[string]models.AgentPromptAnswer{"q1": {Selected: []string{"C"}}}}, wantErr: true},
		{name: "two options on single select", payload: questions, resp: models.AgentPromptResponse{Action: models.AgentPromptAccept,
			Answers: map[string]models.AgentPromptAnswer{"q1": {Selected: []string{"A", "B"}}}}, wantErr: true},
		{name: "nothing answered", payload: questions, resp: models.AgentPromptResponse{Action: models.AgentPromptAccept}, wantErr: true},
		{name: "nothing answered when every question is optional", payload: models.AgentPromptPayload{Kind: models.AgentPromptKindQuestions,
			Questions: []models.AgentPromptQuestion{{ID: "q1", Question: "Anything else?", Optional: true}}},
			resp: models.AgentPromptResponse{Action: models.AgentPromptAccept},
			check: func(t *testing.T, got models.AgentPromptResponse) {
				assert.Empty(t, got.Answers)
			}},
		{name: "answers kept, unknown ids dropped", payload: questions, resp: models.AgentPromptResponse{Action: models.AgentPromptAccept,
			Answers: map[string]models.AgentPromptAnswer{
				"q2":    {Selected: []string{"X", "Y"}},
				"q3":    {Text: "  hello  "},
				"bogus": {Text: "x"},
			}},
			check: func(t *testing.T, got models.AgentPromptResponse) {
				assert.Len(t, got.Answers, 2)
				assert.Equal(t, "hello", got.Answers["q3"].Text)
			}},
		{name: "decline drops answers", payload: questions, resp: models.AgentPromptResponse{Action: models.AgentPromptDecline,
			Answers: map[string]models.AgentPromptAnswer{"q3": {Text: "x"}}, Note: "later"},
			check: func(t *testing.T, got models.AgentPromptResponse) {
				assert.Nil(t, got.Answers)
				assert.Equal(t, "later", got.Note)
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validatePromptResponse(tt.payload, tt.resp)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrInvalidPromptResponse)
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestAskUserResult(t *testing.T) {
	approval := models.AgentPromptPayload{Kind: models.AgentPromptKindApproval, Title: "Send it"}
	res := askUserResult(approval, promptOutcome{status: models.AgentPromptAnswered, response: &models.AgentPromptResponse{Action: models.AgentPromptDecline, Note: "not yet"}})
	assert.Equal(t, false, res["approved"])
	assert.Equal(t, "not yet", res["note"])

	q := models.AgentPromptPayload{Kind: models.AgentPromptKindQuestions, Questions: []models.AgentPromptQuestion{
		{ID: "q1", Question: "Which?", Options: []models.AgentPromptOption{{Label: "A"}}},
		{ID: "q2", Question: "Tags?", Options: []models.AgentPromptOption{{Label: "x"}, {Label: "y"}}, MultiSelect: true},
	}}
	res = askUserResult(q, promptOutcome{status: models.AgentPromptAnswered, response: &models.AgentPromptResponse{Action: models.AgentPromptAccept,
		Answers: map[string]models.AgentPromptAnswer{"q1": {Text: "something else"}, "q2": {Selected: []string{"x", "y"}}}}})
	answers := res["answers"].([]map[string]any)
	assert.Equal(t, "something else", answers[0]["answer"])
	assert.Equal(t, true, answers[0]["own_answer"])
	assert.Equal(t, []string{"x", "y"}, answers[1]["answer"])
}

func TestTurnOnlyToolSpecs_AskUserOnlyWithPromptJob(t *testing.T) {
	names := func(c *chatContext) []string {
		var out []string
		for _, s := range turnOnlyToolSpecs(c) {
			out = append(out, s.Name)
		}
		return out
	}
	assert.Empty(t, names(&chatContext{}))
	assert.Equal(t, []string{agenttools.AskUserToolSpec.Name}, names(promptTestChatCtx()))
	adv := promptTestChatCtx()
	adv.turnSettings = models.TurnSettings{AgentLoop: models.AgentLoopAdvanced}
	assert.Equal(t, []string{agenttools.UpdatePlanToolSpec.Name, agenttools.AskUserToolSpec.Name}, names(adv))
}

func TestInteractivePromptJob(t *testing.T) {
	ctx := context.Background()
	app := metering.WithTurnSource(ctx, metering.TurnSourceApp)
	job := &models.Job{ID: uuid.New(), JobType: JobTypeChatMessage}
	assert.Same(t, job, interactivePromptJob(ctx, job, &chatContext{}))
	assert.Same(t, job, interactivePromptJob(app, job, &chatContext{}))
	assert.Nil(t, interactivePromptJob(app, &models.Job{JobType: "agent_job_run"}, &chatContext{}))
	assert.Nil(t, interactivePromptJob(ctx, nil, &chatContext{}))

	// In a relay thread, only a turn sent from the app can be answered.
	relay := &chatContext{relayPlatform: "Discord"}
	assert.Same(t, job, interactivePromptJob(app, job, relay))
	assert.Nil(t, interactivePromptJob(metering.WithTurnSource(ctx, "discord"), job, relay))
	assert.Nil(t, interactivePromptJob(ctx, job, relay), "a turn that didn't say where it came from")
}

// An unanswered ask_user ends an Advanced run as needs_input, with one forced final answer (the
// question's tool result is pending), instead of continuing on the plan's open items.
func TestAdvancedLoop_UnansweredQuestionEndsRunAsNeedsInput(t *testing.T) {
	store := newMemPromptStore()
	ask := provider.ToolUse{ID: "a1", Name: agenttools.AskUserToolSpec.Name, Input: json.RawMessage(`{"approval":{"action":"Delete the folder"}}`)}
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{Text: "Planning.", ToolUses: []provider.ToolUse{planUse(t, "p1", models.PlanItem{Content: "clean up", Status: models.PlanItemInProgress})}},
		{Text: "Checking first.", ToolUses: []provider.ToolUse{ask}},
	}})
	chatCtx := promptTestChatCtx()
	chatCtx.turnSettings = models.TurnSettings{AgentLoop: models.AgentLoopAdvanced}
	chatCtx.setOfferedTools(map[string]struct{}{agenttools.UpdatePlanToolSpec.Name: {}, agenttools.AskUserToolSpec.Name: {}})
	a := promptTestAgent(store, 20*time.Millisecond)
	tr := newAdvancedTranscript(nil, nil)

	result, _, _, run, err := a.handleAdvancedLoop(context.Background(), chatCtx, adapter, testAdvancedHooks(tr))
	require.NoError(t, err)
	assert.Equal(t, models.AgentRunEndNeedsInput, run.EndReason)
	assert.Equal(t, "Mock adapter forced final response.", result.Text, "the reply is one forced answer with tools off")
	assert.Empty(t, adapter.Continuations(), "no continuation nudge after an unanswered question")
	assert.True(t, run.EndReason.Unfinished(), "the open plan carries into the next turn")
}

func TestAdvancedLoop_PromptWaitDoesNotCountTowardDuration(t *testing.T) {
	r := &advancedRun{chatCtx: &chatContext{promptWaited: time.Hour}, hooks: advancedLoopHooks{config: advancedLoopConfig{maxSteps: 10, maxDuration: time.Minute}}}
	r.start = time.Now().Add(-30 * time.Minute)
	assert.Equal(t, models.AgentRunEnd(""), r.limitReached(1))
	r.chatCtx.promptWaited = 0
	assert.Equal(t, models.AgentRunEndDuration, r.limitReached(1))
}

func TestPromptWaiters_WakeIsNonBlocking(t *testing.T) {
	var w promptWaiters
	id := uuid.New()
	ch, done := w.register(id)
	w.wake(id)
	w.wake(id) // a second wake with one pending must not block
	select {
	case <-ch:
	default:
		t.Fatal("expected a wake")
	}
	done()
	w.wake(id) // after unregister: no-op
	assert.False(t, errors.Is(nil, context.Canceled))
}
