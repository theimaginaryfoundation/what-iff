package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func advancedTestChatCtx() *chatContext {
	c := &chatContext{turnSettings: models.TurnSettings{AgentLoop: models.AgentLoopAdvanced}}
	c.setOfferedTools(map[string]struct{}{agenttools.UpdatePlanToolSpec.Name: {}})
	return c
}

func planUse(t *testing.T, id string, items ...models.PlanItem) provider.ToolUse {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"items": items})
	require.NoError(t, err)
	return provider.ToolUse{ID: id, Name: agenttools.UpdatePlanToolSpec.Name, Input: raw}
}

func unknownUse(id string) provider.ToolUse {
	return provider.ToolUse{ID: id, Name: "unknown_tool", Input: json.RawMessage(`{}`)}
}

func testAdvancedHooks(tr *advancedTranscript) advancedLoopHooks {
	return advancedLoopHooks{
		transcript: tr,
		configure: func(ad provider.AgentAdapter) {
			ad.SetTextDeltaHandler(tr.textDelta)
		},
		config: advancedLoopConfig{maxSteps: 10, maxDuration: time.Minute},
	}
}

func TestAdvancedLoop_SimpleAnswerFinishesInOneStep(t *testing.T) {
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{Text: "Just answering."},
	}})
	chatCtx := advancedTestChatCtx()
	tr := newAdvancedTranscript(nil, nil)
	a := &Agent{logger: zap.NewNop()}

	result, toolCalls, _, run, err := a.handleAdvancedLoop(context.Background(), chatCtx, adapter, testAdvancedHooks(tr))
	require.NoError(t, err)
	assert.Equal(t, "Just answering.", result.Text)
	assert.Equal(t, models.AgentRunEndDone, run.EndReason)
	assert.Empty(t, run.Steps, "the final reply is the message, not a note")
	assert.Empty(t, result.Reasoning, "the final reply is the message, not reasoning")
	assert.Empty(t, toolCalls)
	assert.Empty(t, adapter.Continuations())
	assert.Len(t, chatCtx.calls, 1)
}

func TestAdvancedLoop_ContinuesWhilePlanHasOpenItems(t *testing.T) {
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{Text: "Let me plan.", ToolUses: []provider.ToolUse{planUse(t, "p1",
			models.PlanItem{Content: "research", Status: models.PlanItemInProgress},
			models.PlanItem{Content: "write", Status: models.PlanItemPending})}},
		{Text: "Research done, writing next."}, // step 1 ends with open items
		{Text: "Wrapping up.", ToolUses: []provider.ToolUse{planUse(t, "p2",
			models.PlanItem{Content: "research", Status: models.PlanItemCompleted},
			models.PlanItem{Content: "write", Status: models.PlanItemCompleted})}},
		{Text: "Here is the final report."},
	}})
	chatCtx := advancedTestChatCtx()
	tr := newAdvancedTranscript(nil, nil)
	a := &Agent{logger: zap.NewNop()}

	var recorded []int
	var drafts []*models.AgentRunDraft
	var saved []*models.ToolCall
	hooks := testAdvancedHooks(tr)
	hooks.recordStep = func(_ context.Context, step int) { recorded = append(recorded, step) }
	hooks.saveDraft = func(d *models.AgentRunDraft) { drafts = append(drafts, d) }
	hooks.saveToolCall = func(tc *models.ToolCall) error { saved = append(saved, tc); return nil }
	result, toolCalls, _, run, err := a.handleAdvancedLoop(context.Background(), chatCtx, adapter, hooks)
	require.NoError(t, err)
	assert.Equal(t, "Here is the final report.", result.Text)
	require.Len(t, adapter.Continuations(), 1)
	assert.Contains(t, adapter.Continuations()[0], "[pending] write")
	require.Len(t, toolCalls, 2)
	assert.Equal(t, 1, toolCalls[0].Step)
	assert.Equal(t, 2, toolCalls[1].Step)
	assert.Empty(t, result.Reasoning, "an Advanced run's notes are its agent_run, not reasoning")
	assert.Equal(t, models.AgentRunEndDone, run.EndReason)
	assert.Equal(t, models.AgentRunVersion, run.Version)
	assert.Equal(t, []models.AgentRunStep{
		{Step: 1, Events: []models.AgentRunEvent{
			{Kind: models.AgentRunEventText, Text: "Let me plan."},
			{Kind: models.AgentRunEventToolCall, ToolCallID: toolCalls[0].ID.String()},
			{Kind: models.AgentRunEventText, Text: "Research done, writing next."},
		}},
		{Step: 2, Events: []models.AgentRunEvent{
			{Kind: models.AgentRunEventText, Text: "Wrapping up."},
			{Kind: models.AgentRunEventToolCall, ToolCallID: toolCalls[1].ID.String()},
		}},
	}, run.Steps, "notes and tool calls in order; the final reply is not repeated")
	require.Len(t, saved, 2, "each tool call is saved as a draft row as it finishes")
	assert.True(t, toolCalls[0].JobDraft, "the reply moves the saved row instead of creating one")
	require.Len(t, run.Plan, 2)
	assert.Equal(t, []int{1, 2}, recorded, "each step is metered as it finishes")
	require.Len(t, drafts, 1, "a draft is saved at each step boundary the run continues past")
	assert.Equal(t, "research", drafts[0].Run.Plan[0].Content)
	assert.Len(t, drafts[0].Run.Steps[0].Events, 3, "the draft refers to the tool call rather than copying it")
	assert.Len(t, chatCtx.calls, 4, "every model call is reported for metering")
}

func TestAdvancedLoop_InnerCapEndsStepWithoutForcingFinal(t *testing.T) {
	script := make([]provider.MockScriptStep, 0, maxToolCallRounds+2)
	for i := 0; i < maxToolCallRounds+1; i++ {
		script = append(script, provider.MockScriptStep{ToolUses: []provider.ToolUse{unknownUse(fmt.Sprintf("u%d", i))}})
	}
	script = append(script, provider.MockScriptStep{Text: "done after many tools"})
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: script})
	chatCtx := advancedTestChatCtx()
	a := &Agent{logger: zap.NewNop()}

	result, toolCalls, _, _, err := a.handleAdvancedLoop(context.Background(), chatCtx, adapter, testAdvancedHooks(newAdvancedTranscript(nil, nil)))
	require.NoError(t, err)
	assert.Equal(t, "done after many tools", result.Text)
	assert.Len(t, toolCalls, maxToolCallRounds+1)
	assert.Equal(t, 1, toolCalls[maxToolCallRounds-1].Step)
	assert.Equal(t, 2, toolCalls[maxToolCallRounds].Step, "the round counter resets for the next step")
	assert.Empty(t, adapter.Continuations(), "pending tool results continue the conversation on their own")
}

func TestAdvancedLoop_StallGuardEndsRun(t *testing.T) {
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{ToolUses: []provider.ToolUse{planUse(t, "p1", models.PlanItem{Content: "a", Status: models.PlanItemPending})}},
		{Text: "first"},
		{Text: "second"},
		{Text: "third"},
		{Text: "never reached"},
	}})
	a := &Agent{logger: zap.NewNop()}

	result, _, _, _, err := a.handleAdvancedLoop(context.Background(), advancedTestChatCtx(), adapter, testAdvancedHooks(newAdvancedTranscript(nil, nil)))
	require.NoError(t, err)
	assert.Equal(t, "third", result.Text)
	assert.Len(t, adapter.Continuations(), 2)
}

func TestAdvancedLoop_StepLimitReturnsLatestReply(t *testing.T) {
	var script []provider.MockScriptStep
	script = append(script, provider.MockScriptStep{ToolUses: []provider.ToolUse{planUse(t, "p", models.PlanItem{Content: "a", Status: models.PlanItemPending})}})
	for i := 0; i < 3; i++ {
		script = append(script,
			provider.MockScriptStep{Text: fmt.Sprintf("reply %d", i)},
			provider.MockScriptStep{ToolUses: []provider.ToolUse{unknownUse(fmt.Sprintf("u%d", i))}})
	}
	script[len(script)-1] = provider.MockScriptStep{Text: "Done so far: a little. Still open: a."}
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: script})
	hooks := testAdvancedHooks(newAdvancedTranscript(nil, nil))
	hooks.config.maxSteps = 3
	a := &Agent{logger: zap.NewNop()}

	result, _, _, run, err := a.handleAdvancedLoop(context.Background(), advancedTestChatCtx(), adapter, hooks)
	require.NoError(t, err)
	assert.Equal(t, "Done so far: a little. Still open: a.", result.Text, "a limit stop asks for a summary reply")
	assert.Equal(t, models.AgentRunEndStepLimit, run.EndReason)
	conts := adapter.Continuations()
	require.NotEmpty(t, conts)
	assert.Contains(t, conts[len(conts)-1], "the step limit")
	assert.Contains(t, conts[len(conts)-1], "[pending] a")
	assert.Equal(t, models.PlanItemPending, run.Plan[0].Status, "the open plan is kept for the next turn")
	last := run.Steps[len(run.Steps)-1].Events
	assert.Equal(t, "reply 2", last[len(last)-1].Text, "the replaced reply stays as a note")
}

// The quota is re-checked before every call after the first, with the calls not yet metered: a
// refusal mid-step stops the run before the next call, with a fixed reply (another call would
// spend past the quota) and the step's work metered and kept.
func TestAdvancedLoop_QuotaDenialMidStepStopsBeforeNextCall(t *testing.T) {
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{Text: "Planning.", ToolUses: []provider.ToolUse{planUse(t, "p", models.PlanItem{Content: "a", Status: models.PlanItemPending})}},
		{Text: "never reached"},
	}})
	chatCtx := advancedTestChatCtx()
	hooks := testAdvancedHooks(newAdvancedTranscript(nil, nil))
	var pendingSeen [][]provider.CallUsage
	hooks.quotaAllowed = func(_ context.Context, pending []provider.CallUsage) (bool, error) {
		pendingSeen = append(pendingSeen, pending)
		return false, nil
	}
	var recorded []int
	hooks.recordStep = func(_ context.Context, step int) { recorded = append(recorded, step) }
	a := &Agent{logger: zap.NewNop()}

	result, toolCalls, _, run, err := a.handleAdvancedLoop(context.Background(), chatCtx, adapter, hooks)
	require.NoError(t, err)
	assert.Equal(t, advancedQuotaStopMessage, result.Text)
	assert.NotZero(t, result.CreatedAt)
	assert.Equal(t, models.AgentRunEndQuota, run.EndReason)
	require.Len(t, pendingSeen, 1, "the first call is covered by the turn's own check")
	assert.Len(t, pendingSeen[0], 1, "the check sees the unmetered call")
	assert.Len(t, chatCtx.calls, 1, "the refused call was never made")
	assert.Len(t, toolCalls, 1, "the step's tool call is kept")
	assert.Equal(t, []int{1}, recorded, "the step's calls are metered")
	assert.Equal(t, "Planning.", run.Steps[0].Events[0].Text)
	assert.Equal(t, models.PlanItemPending, run.Plan[0].Status, "the open plan is kept for the next turn")
}

// A refusal at the first call of a new step keeps the previous step's answer as the reply.
func TestAdvancedLoop_QuotaDenialBetweenStepsKeepsLatestAnswer(t *testing.T) {
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{ToolUses: []provider.ToolUse{planUse(t, "p", models.PlanItem{Content: "a", Status: models.PlanItemPending})}},
		{Text: "partial answer"},
		{Text: "never reached"},
	}})
	chatCtx := advancedTestChatCtx()
	hooks := testAdvancedHooks(newAdvancedTranscript(nil, nil))
	hooks.recordStep = func(context.Context, int) { chatCtx.takeUnrecordedCalls() }
	// Refuse only once nothing is pending: the step was recorded and the next one is starting.
	hooks.quotaAllowed = func(_ context.Context, pending []provider.CallUsage) (bool, error) { return len(pending) > 0, nil }
	a := &Agent{logger: zap.NewNop()}

	result, _, _, run, err := a.handleAdvancedLoop(context.Background(), chatCtx, adapter, hooks)
	require.NoError(t, err)
	assert.Equal(t, "partial answer", result.Text)
	assert.Equal(t, models.AgentRunEndQuota, run.EndReason)
	assert.Len(t, chatCtx.calls, 2)
}

func TestAdvancedLoop_CompactsIntoFreshAdapter(t *testing.T) {
	first := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, EchoText: strings.Repeat("x", 400), Script: []provider.MockScriptStep{
		{ToolUses: []provider.ToolUse{planUse(t, "p", models.PlanItem{Content: "look things up", Status: models.PlanItemInProgress})}},
		{Text: "found part of it"},
	}})
	second := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{ToolUses: []provider.ToolUse{planUse(t, "p2", models.PlanItem{Content: "look things up", Status: models.PlanItemCompleted})}},
		{Text: "all done"},
	}})
	var rebuiltWith *provider.ModelContext
	tr := newAdvancedTranscript(nil, nil)
	hooks := testAdvancedHooks(tr)
	hooks.config.compactTokens = 10
	hooks.modelContext = &provider.ModelContext{}
	hooks.modelContext.AppendUserMessage(provider.RoleUser, "do the research", nil, false)
	hooks.rebuild = func(mc *provider.ModelContext) (provider.AgentAdapter, error) {
		rebuiltWith = mc
		return second, nil
	}
	chatCtx := advancedTestChatCtx()
	a := &Agent{logger: zap.NewNop()}

	result, _, _, _, err := a.handleAdvancedLoop(context.Background(), chatCtx, first, hooks)
	require.NoError(t, err)
	assert.Equal(t, "all done", result.Text)
	require.NotNil(t, rebuiltWith)
	var workLog string
	for _, seg := range rebuiltWith.Segments {
		if seg.Kind == provider.SegmentKindDeveloperContext {
			workLog = seg.Content
		}
	}
	assert.Contains(t, workLog, "[in_progress] look things up")
	assert.Contains(t, workLog, "found part of it")
	assert.Contains(t, workLog, "update_plan(")
	assert.Equal(t, provider.SegmentKindUserMessage, rebuiltWith.Segments[len(rebuiltWith.Segments)-1].Kind)
	assert.Empty(t, first.Continuations(), "a compacted run starts fresh instead of continuing the old adapter")
	assert.Len(t, chatCtx.calls, 4, "calls from the replaced adapter still count")
}

func TestAdvancedTranscript_DiscardDropsOnlyCurrentCallReasoning(t *testing.T) {
	var live strings.Builder
	resets := 0
	tr := newAdvancedTranscript(func(d string) { live.WriteString(d) }, func() { resets++; live.Reset() })
	tr.beginCall()
	tr.nativeDelta("thinking one")
	tr.textDelta("note one")
	tr.beginCall()
	tr.nativeDelta("doomed attempt")
	tr.discardCallNative()
	tr.nativeDelta("thinking two")
	tr.textDelta("final reply")

	assert.Equal(t, 1, resets)
	assert.Equal(t, "thinking one\n\nnote one\n\nthinking two\n\nfinal reply", live.String())
	assert.Equal(t, []models.AgentRunStep{{Step: 1, Events: []models.AgentRunEvent{
		{Kind: models.AgentRunEventReasoning, Text: "thinking one"},
		{Kind: models.AgentRunEventText, Text: "note one"},
		{Kind: models.AgentRunEventReasoning, Text: "thinking two"},
	}}}, tr.run(2))
}

func TestWithTurnSettingsUsage(t *testing.T) {
	chatCtx := &chatContext{
		turnSettings:    models.TurnSettings{ReasoningEffort: models.ReasoningEffortHigh, ReasoningEffortSource: models.SettingSourceThread, AgentLoop: models.AgentLoopAdvanced, AgentLoopSource: models.SettingSourceMode},
		reasoningEffort: models.ReasoningEffortHigh,
		calls:           []provider.CallUsage{{InputTokens: 10}, {InputTokens: 20, CachedInputTokens: 5, OutputTokens: 7}},
		recordedCalls:   1,
		turnID:          "turn-1",
	}
	u := withTurnSettingsUsage(chatCtx, metering.Usage{})
	assert.Equal(t, "high", u.ReasoningEffort)
	assert.Equal(t, "advanced", u.AgentLoop)
	assert.Equal(t, []provider.CallUsage{{InputTokens: 20, CachedInputTokens: 5, OutputTokens: 7}}, u.Calls, "only calls no step record has taken")
	assert.Equal(t, "turn-1", u.TurnID)

	// No selection: GLM's always-on default reports no effort, so it is priced as before.
	plain := withTurnSettingsUsage(&chatContext{reasoningEffort: ""}, metering.Usage{})
	assert.Empty(t, plain.ReasoningEffort)
	assert.Equal(t, "standard", plain.AgentLoop)
	assert.Empty(t, plain.Calls)
}

// Pins the Advanced path's wiring through runGeneration: it runs the Advanced loop (the mock
// implements provider.Continuer) and still reaches the shared empty-turn guard in
// finishGeneration before anything is saved, so it needs no datastore.
func TestRunGeneration_AdvancedTurnUsesSharedGuard(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	chatCtx := advancedTestChatCtx()
	chatCtx.chat = &models.Chat{ID: uuid.New(), UserID: uuid.New()}
	chatCtx.model = "mock-model"
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeFixed, FixedResponses: []string{""}})

	agentMessage, result, err := a.runGeneration(context.Background(), chatCtx.chat.UserID, nil,
		&models.ChatMessage{ID: uuid.New(), ChatID: chatCtx.chat.ID}, chatCtx, adapter, generationOptions{provider: "mock"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
	assert.Nil(t, agentMessage)
	assert.Nil(t, result)
	assert.Len(t, chatCtx.calls, 1, "the Advanced loop ran and recorded its call")
}

func TestAdvancedLoopConfig_DefaultsOverridesAndInvalidValues(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }

	cfg := advancedLoopConfigFrom(getenv, zap.NewNop())
	assert.Equal(t, advancedLoopConfig{maxSteps: defaultAdvancedMaxSteps, maxDuration: defaultAdvancedMaxDuration, compactTokens: defaultAdvancedCompactTokens}, cfg)

	env = map[string]string{"AGENT_ADVANCED_MAX_STEPS": "7", "AGENT_ADVANCED_MAX_DURATION": "5m", "AGENT_ADVANCED_COMPACT_TOKENS": "9000"}
	cfg = advancedLoopConfigFrom(getenv, zap.NewNop())
	assert.Equal(t, advancedLoopConfig{maxSteps: 7, maxDuration: 5 * time.Minute, compactTokens: 9000}, cfg)

	core, logs := observer.New(zap.WarnLevel)
	env = map[string]string{"AGENT_ADVANCED_MAX_STEPS": "-1", "AGENT_ADVANCED_MAX_DURATION": "soon", "AGENT_ADVANCED_COMPACT_TOKENS": "lots"}
	cfg = advancedLoopConfigFrom(getenv, zap.New(core))
	assert.Equal(t, defaultAdvancedMaxSteps, cfg.maxSteps)
	assert.Equal(t, defaultAdvancedMaxDuration, cfg.maxDuration)
	assert.Equal(t, int64(defaultAdvancedCompactTokens), cfg.compactTokens)
	assert.Equal(t, 3, logs.Len(), "each invalid limit is logged")

	assert.Equal(t, defaultAdvancedMaxSteps, advancedLoopConfigFromEnv(nil).maxSteps)

	core, logs = observer.New(zap.WarnLevel)
	env = map[string]string{"AGENT_ADVANCED_MAX_STEPS": "1000000", "AGENT_ADVANCED_MAX_DURATION": "720h", "AGENT_ADVANCED_COMPACT_TOKENS": "10000000000"}
	cfg = advancedLoopConfigFrom(getenv, zap.New(core))
	assert.Equal(t, advancedLoopConfig{maxSteps: maxAdvancedMaxSteps, maxDuration: maxAdvancedMaxDuration, compactTokens: maxAdvancedCompactTokens}, cfg)
	assert.Equal(t, 3, logs.FilterMessageSnippet("clamping").Len(), "each clamped limit is logged")
}

// A run that hits its time limit with tool results pending forces one final answer.
func TestAdvancedLoop_DurationLimitForcesFinalWhenToolsPending(t *testing.T) {
	script := make([]provider.MockScriptStep, 0, maxToolCallRounds)
	for i := 0; i < maxToolCallRounds; i++ {
		script = append(script, provider.MockScriptStep{ToolUses: []provider.ToolUse{unknownUse(fmt.Sprintf("u%d", i))}})
	}
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: script})
	hooks := testAdvancedHooks(newAdvancedTranscript(nil, nil))
	hooks.config.maxDuration = -time.Second // already past, whatever the clock resolution (Windows)
	chatCtx := advancedTestChatCtx()
	a := &Agent{logger: zap.NewNop()}

	result, toolCalls, _, _, err := a.handleAdvancedLoop(context.Background(), chatCtx, adapter, hooks)
	require.NoError(t, err)
	assert.Equal(t, "Mock adapter forced final response.", result.Text)
	assert.Len(t, toolCalls, maxToolCallRounds)
	assert.Len(t, chatCtx.calls, maxToolCallRounds+1, "the forced call counts too")
}

func TestAdvancedLoop_CancelledBetweenSteps(t *testing.T) {
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{ToolUses: []provider.ToolUse{planUse(t, "p", models.PlanItem{Content: "a", Status: models.PlanItemPending})}},
		{Text: "partial"},
	}})
	ctx, cancel := context.WithCancel(context.Background())
	hooks := testAdvancedHooks(newAdvancedTranscript(nil, nil))
	// Cancel as the first step is recorded: the loop's cancellation check before the next step
	// then ends the run.
	hooks.recordStep = func(context.Context, int) { cancel() }
	a := &Agent{logger: zap.NewNop()}
	var final *models.AgentRunDraft
	hooks.saveDraft = func(d *models.AgentRunDraft) { final = d }
	_, _, _, run, err := a.handleAdvancedLoop(ctx, advancedTestChatCtx(), adapter, hooks)
	require.Error(t, err)
	assert.Equal(t, models.AgentRunEndCancelled, run.EndReason)
	require.NotNil(t, final, "the run so far is saved for the partial message")
	assert.Equal(t, models.AgentRunEndCancelled, final.Run.EndReason)
	ev := final.Run.Steps[0].Events
	assert.Equal(t, "partial", ev[len(ev)-1].Text, "with no final reply every note is kept")
}

func TestAdvancedLoop_CompactionFailureContinuesUncompacted(t *testing.T) {
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, EchoText: strings.Repeat("x", 400), Script: []provider.MockScriptStep{
		{ToolUses: []provider.ToolUse{planUse(t, "p", models.PlanItem{Content: "a", Status: models.PlanItemPending})}},
		{Text: "first"},
		{ToolUses: []provider.ToolUse{planUse(t, "p2", models.PlanItem{Content: "a", Status: models.PlanItemCompleted})}},
		{Text: "done"},
	}})
	core, logs := observer.New(zap.WarnLevel)
	hooks := testAdvancedHooks(newAdvancedTranscript(nil, nil))
	hooks.config.compactTokens = 10
	hooks.modelContext = &provider.ModelContext{}
	hooks.rebuild = func(*provider.ModelContext) (provider.AgentAdapter, error) { return nil, errors.New("no provider") }
	a := &Agent{logger: zap.New(core)}

	result, _, _, _, err := a.handleAdvancedLoop(context.Background(), advancedTestChatCtx(), adapter, hooks)
	require.NoError(t, err)
	assert.Equal(t, "done", result.Text)
	assert.Len(t, adapter.Continuations(), 1, "without compaction the run continues on the same adapter")
	assert.Equal(t, 1, logs.FilterMessageSnippet("compaction failed").Len())
}

// A rebuilt adapter that cannot continue is refused rather than swapped in.
func TestAdvancedRun_CompactRejectsAdapterWithoutContinuer(t *testing.T) {
	run := &advancedRun{a: &Agent{logger: zap.NewNop()}, chatCtx: advancedTestChatCtx(), hooks: advancedLoopHooks{
		modelContext: &provider.ModelContext{},
		rebuild:      func(*provider.ModelContext) (provider.AgentAdapter, error) { return &scriptedAdapter{}, nil },
	}}
	require.Error(t, run.compact(context.Background(), 2))
}

// The work log keeps the newest tool calls within its budget and says how many were omitted.
func TestAdvancedRun_WorkLogDropsOldestCalls(t *testing.T) {
	chatCtx := advancedTestChatCtx()
	chatCtx.plan = []models.PlanItem{{Content: "research", Status: models.PlanItemInProgress}}
	run := &advancedRun{chatCtx: chatCtx, hooks: advancedLoopHooks{transcript: newAdvancedTranscript(nil, nil)}}
	big := strings.Repeat("y", 600)
	for i := 0; i < 200; i++ {
		run.toolCalls = append(run.toolCalls, &models.ToolCall{ToolName: fmt.Sprintf("tool%d", i), ToolInput: big, ToolOutput: big, Step: 1})
	}
	run.toolCalls[199].ToolError = "boom"
	log := run.workLog(3)
	assert.LessOrEqual(t, len(log), advancedWorkLogMaxChars+1000)
	assert.Contains(t, log, "older ones omitted")
	assert.Contains(t, log, "tool199(")
	assert.Contains(t, log, "error: boom")
	assert.NotContains(t, log, "tool0(")
	assert.Contains(t, log, "[in_progress] research")
}

func TestUpdatePlanTool(t *testing.T) {
	chatCtx := advancedTestChatCtx()
	out, err := updatePlanTool(chatCtx, []byte(`{"items":[{"content":"a","status":"completed"},{"content":"b","status":"pending"}]}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"success":true,"open_items":1,"total":2}`, out)
	assert.Equal(t, 1, chatCtx.planVersion)

	_, err = updatePlanTool(chatCtx, []byte(`{"items":[{"content":"","status":"pending"}]}`))
	assert.Error(t, err)
	assert.Equal(t, 1, chatCtx.planVersion, "a rejected plan changes nothing")
	assert.Nil(t, turnOnlyToolSpecs(&chatContext{}))
	assert.Nil(t, turnOnlyToolSpecs(nil))
}

type usageOnlyAdapter struct {
	scriptedAdapter
	usage []provider.CallUsage
}

func (u *usageOnlyAdapter) CallUsage() []provider.CallUsage { return u.usage }

// Compaction follows the run's growth, and the model's window, not the thread's absolute size.
func TestAdvancedRun_ShouldCompact(t *testing.T) {
	check := func(window int64, usage ...int64) bool {
		calls := make([]provider.CallUsage, len(usage))
		for i, in := range usage {
			calls[i] = provider.CallUsage{InputTokens: in}
		}
		run := &advancedRun{adapter: &usageOnlyAdapter{usage: calls}, hooks: advancedLoopHooks{
			rebuild:       func(*provider.ModelContext) (provider.AgentAdapter, error) { return nil, nil },
			modelContext:  &provider.ModelContext{},
			contextWindow: window,
			config:        advancedLoopConfig{compactTokens: 100_000},
		}}
		return run.shouldCompact()
	}
	assert.False(t, check(0, 150_000), "one call has no growth to remove")
	assert.False(t, check(1_000_000, 150_000, 200_000), "a long thread on a large window is not compacted for its size")
	assert.True(t, check(1_000_000, 150_000, 260_000), "growth past compactTokens")
	assert.True(t, check(200_000, 100_000, 130_000), "past 60% of a small window with real growth")
	assert.False(t, check(200_000, 125_000, 130_000), "past 60% of the window, but too little growth to help")
	assert.False(t, check(0, 100_000, 190_000), "unknown window: growth rule only")
}

// A run that stopped before its work was done hands its open items to the next Advanced turn.
func TestUnfinishedRunPlan(t *testing.T) {
	plan := []models.PlanItem{{Content: "a", Status: models.PlanItemCompleted}, {Content: "b", Status: models.PlanItemPending}}
	open, reason := unfinishedRunPlan(&models.ChatMessage{AgentRun: &models.AgentRun{Plan: plan, EndReason: models.AgentRunEndStepLimit}})
	assert.Equal(t, []models.PlanItem{plan[1]}, open)
	assert.Equal(t, models.AgentRunEndStepLimit, reason)
	msg := openPlanContext(open, reason)
	assert.Contains(t, msg, "step_limit")
	assert.Contains(t, msg, "> - [pending] b", "items are quoted from the earlier run")

	old := &models.ChatMessage{SentAt: time.Now().Add(-advancedCarryOverMaxAge - time.Hour), AgentRun: &models.AgentRun{Plan: plan, EndReason: models.AgentRunEndStepLimit}}
	stale, _ := unfinishedRunPlan(old)
	assert.Empty(t, stale, "a run older than the cutoff carries nothing over")

	open, _ = unfinishedRunPlan(&models.ChatMessage{AgentRun: &models.AgentRun{Plan: plan, EndReason: models.AgentRunEndDone}})
	assert.Empty(t, open, "a finished run carries nothing over")
	open, _ = unfinishedRunPlan(&models.ChatMessage{})
	assert.Empty(t, open, "a Standard reply has no run")
	open, _ = unfinishedRunPlan(nil)
	assert.Empty(t, open)
	open, _ = (&Agent{}).previousRunOpenPlan(context.Background(), uuid.New(), uuid.New())
	assert.Empty(t, open, "no datastore, nothing to carry")
}

// Re-sending the same plan is not progress, so it does not reset the stall guard.
func TestUpdatePlanTool_IdenticalPlanIsNotProgress(t *testing.T) {
	chatCtx := advancedTestChatCtx()
	in := []byte(`{"items":[{"content":"a","status":"pending"}]}`)
	_, err := updatePlanTool(chatCtx, in)
	require.NoError(t, err)
	_, err = updatePlanTool(chatCtx, in)
	require.NoError(t, err)
	assert.Equal(t, 1, chatCtx.planVersion)
}

func TestTurnContextTokens(t *testing.T) {
	result := &provider.GenerateResponse{InputTokens: 90_000}
	assert.Equal(t, int64(90_000), turnContextTokens(nil, result))
	standard := &chatContext{calls: []provider.CallUsage{{InputTokens: 10_000}}}
	assert.Equal(t, int64(90_000), turnContextTokens(standard, result))
	advanced := advancedTestChatCtx()
	advanced.calls = []provider.CallUsage{{InputTokens: 10_000}, {InputTokens: 90_000}}
	assert.Equal(t, int64(10_000), turnContextTokens(advanced, result), "an Advanced run's growth is not the thread's")
}

func TestSubagentCalls(t *testing.T) {
	final := &provider.GenerateResponse{InputTokens: 7, OutputTokens: 3}
	assert.Equal(t, []provider.CallUsage{{InputTokens: 7, OutputTokens: 3}}, subagentCalls(nil, final))
	assert.Nil(t, subagentCalls(nil, nil))
	reporter := &usageOnlyAdapter{usage: []provider.CallUsage{{InputTokens: 1}, {InputTokens: 2}}}
	assert.Len(t, subagentCalls(reporter, final), 2)
}

func TestSumWebSearches(t *testing.T) {
	assert.Equal(t, 0, sumWebSearches([]*provider.MockAdapter{provider.NewMockAdapter(provider.MockAdapterConfig{}), provider.NewMockAdapter(provider.MockAdapterConfig{})}))
	assert.Equal(t, 0, sumWebSearches[*provider.MockAdapter](nil))
}

// pendingMeter captures what each Check saw on the context.
type pendingMeter struct {
	metering.NoopMeter
	pending []metering.Usage
	allow   bool
	err     error
}

func (m *pendingMeter) Check(ctx context.Context, _ uuid.UUID, _, _ string) metering.Decision {
	if u, ok := metering.PendingUsageFromContext(ctx); ok {
		m.pending = append(m.pending, u)
	}
	return metering.Decision{Allowed: m.allow && m.err == nil, Err: m.err}
}

// The mid-run check hands the meter the unmetered calls, priced as they will be recorded.
func TestAdvancedQuotaAllowed_PassesPendingUsage(t *testing.T) {
	m := &pendingMeter{allow: true}
	a := &Agent{logger: zap.NewNop(), meter: m}
	chatCtx := advancedTestChatCtx()
	chatCtx.model = "gpt-5.1"
	chatCtx.turnID = "turn-1"
	chatCtx.turnSettings.ReasoningEffortSource = models.SettingSourceThread
	chatCtx.reasoningEffort = models.ReasoningEffortHigh
	pending := []provider.CallUsage{{InputTokens: 9_000, CachedInputTokens: 8_000, OutputTokens: 300}}

	allowed, err := a.advancedQuotaAllowed(context.Background(), uuid.New(), chatCtx, pending)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Len(t, m.pending, 1)
	got := m.pending[0]
	assert.Equal(t, "advanced", got.AgentLoop)
	assert.Equal(t, "gpt-5.1", got.Model)
	assert.Equal(t, "turn-1", got.TurnID)
	assert.Equal(t, "high", got.ReasoningEffort)
	assert.Equal(t, pending, got.Calls)

	assert.True(t, got.CallsReported)

	m.allow = false
	allowed, err = a.advancedQuotaAllowed(context.Background(), uuid.New(), chatCtx, pending)
	require.NoError(t, err)
	assert.False(t, allowed)

	// A meter that cannot decide is retried once, then reported as an error, not a refusal.
	m.err = errors.New("db down")
	_, err = a.advancedQuotaAllowed(context.Background(), uuid.New(), chatCtx, pending)
	require.Error(t, err)
	assert.Equal(t, 4, len(m.pending), "the failing call was checked twice: once and its retry")
}

// N2: Stop while the step's tools run reaches the next beforeCall with a cancelled context; the
// run ends cancelled, never as out of quota, and the meter is not asked.
func TestAdvancedLoop_StopMidStepIsCancelledNotQuota(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{ToolUses: []provider.ToolUse{planUse(t, "p", models.PlanItem{Content: "a", Status: models.PlanItemPending})}},
		{Text: "never reached"},
	}})
	chatCtx := advancedTestChatCtx()
	chatCtx.toolProgress = nil
	hooks := testAdvancedHooks(newAdvancedTranscript(nil, nil))
	asked := 0
	hooks.quotaAllowed = func(context.Context, []provider.CallUsage) (bool, error) { asked++; return false, nil }
	hooks.configure = func(ad provider.AgentAdapter) {
		ad.SetTextDeltaHandler(func(string) {})
		cancel() // Stop: the mock does not check ctx on a tool-only reply, so the step reaches its tool and the next beforeCall
	}
	a := &Agent{logger: zap.NewNop()}

	_, _, _, run, err := a.handleAdvancedLoop(ctx, chatCtx, adapter, hooks)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, models.AgentRunEndCancelled, run.EndReason)
	assert.Zero(t, asked)
}

// N2: a meter error mid-run fails the run (a partial is saved) instead of ending it as quota.
func TestAdvancedLoop_MeterErrorFailsRunNotQuota(t *testing.T) {
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{ToolUses: []provider.ToolUse{planUse(t, "p", models.PlanItem{Content: "a", Status: models.PlanItemPending})}},
		{Text: "never reached"},
	}})
	hooks := testAdvancedHooks(newAdvancedTranscript(nil, nil))
	hooks.quotaAllowed = func(context.Context, []provider.CallUsage) (bool, error) { return false, errors.New("db down") }
	a := &Agent{logger: zap.NewNop()}

	_, _, _, run, err := a.handleAdvancedLoop(context.Background(), advancedTestChatCtx(), adapter, hooks)
	require.ErrorIs(t, err, errAdvancedQuotaCheck)
	assert.Equal(t, models.AgentRunEndFailed, run.EndReason)
}

// N3: a call that fails mid-step (Stop, a provider error) keeps the tool calls the step already
// ran in the saved draft.
func TestAdvancedLoop_FailureMidStepKeepsStepToolCalls(t *testing.T) {
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{ToolUses: []provider.ToolUse{unknownUse("u0")}},
		{ToolUses: []provider.ToolUse{unknownUse("u1")}},
		// the third call fails: the script is exhausted
	}})
	hooks := testAdvancedHooks(newAdvancedTranscript(nil, nil))
	var final *models.AgentRunDraft
	hooks.saveDraft = func(d *models.AgentRunDraft) { final = d }
	a := &Agent{logger: zap.NewNop()}

	_, _, _, run, err := a.handleAdvancedLoop(context.Background(), advancedTestChatCtx(), adapter, hooks)
	require.Error(t, err)
	assert.Equal(t, models.AgentRunEndFailed, run.EndReason)
	require.NotNil(t, final)
	var refs int
	for _, ev := range final.Run.Steps[0].Events {
		if ev.Kind == models.AgentRunEventToolCall {
			refs++
		}
	}
	assert.Equal(t, 2, refs, "both tool calls of the failed step are in the draft")
}

// overflowAdapter fails its failAt-th call (1-based) with a context-overflow error.
type overflowAdapter struct {
	*provider.MockAdapter
	failAt, n int
}

func (o *overflowAdapter) Call(ctx context.Context) (*provider.GenerateResponse, []provider.ToolUse, error) {
	o.n++
	if o.n == o.failAt {
		return nil, nil, errors.New("400: This model's maximum context length is 128000 tokens")
	}
	return o.MockAdapter.Call(ctx)
}

// N5: a call refused mid-step because the input outgrew the window compacts the run and carries
// on; a second overflow straight after fails it.
func TestAdvancedLoop_ContextOverflowMidStepCompacts(t *testing.T) {
	first := &overflowAdapter{failAt: 2, MockAdapter: provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{ToolUses: []provider.ToolUse{unknownUse("u0")}},
	}})}
	second := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{Text: "done after compacting"},
	}})
	hooks := testAdvancedHooks(newAdvancedTranscript(nil, nil))
	hooks.modelContext = &provider.ModelContext{}
	hooks.modelContext.AppendUserMessage(provider.RoleUser, "research", nil, false)
	hooks.rebuild = func(*provider.ModelContext) (provider.AgentAdapter, error) { return second, nil }
	a := &Agent{logger: zap.NewNop()}

	result, toolCalls, _, run, err := a.handleAdvancedLoop(context.Background(), advancedTestChatCtx(), first, hooks)
	require.NoError(t, err)
	assert.Equal(t, "done after compacting", result.Text)
	assert.Len(t, toolCalls, 1, "the step's tool call survives the compaction")
	assert.Equal(t, models.AgentRunEndDone, run.EndReason)

	again := &overflowAdapter{failAt: 1, MockAdapter: provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted})}
	hooks.rebuild = func(*provider.ModelContext) (provider.AgentAdapter, error) { return again, nil }
	first = &overflowAdapter{failAt: 2, MockAdapter: provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{ToolUses: []provider.ToolUse{unknownUse("u0")}},
	}})}
	_, _, _, run, err = a.handleAdvancedLoop(context.Background(), advancedTestChatCtx(), first, hooks)
	require.Error(t, err, "an overflow right after compacting is not retried again")
	assert.Equal(t, models.AgentRunEndFailed, run.EndReason)
}

func TestIsContextOverflowError(t *testing.T) {
	assert.True(t, provider.IsContextOverflowError(errors.New("prompt is too long: 210000 tokens > 200000 maximum")))
	assert.True(t, provider.IsContextOverflowError(fmt.Errorf("wrapped: %w", errors.New("context_length_exceeded"))))
	assert.False(t, provider.IsContextOverflowError(errors.New("rate limited")))
	assert.False(t, provider.IsContextOverflowError(nil))
}

// N5: the compacted work log keeps the newest notes, not the first ones.
func TestTruncateRunesKeepTail(t *testing.T) {
	assert.Equal(t, "short", truncateRunesKeepTail("short", 10))
	got := truncateRunesKeepTail("first notes ... latest notes", 12)
	assert.True(t, strings.HasSuffix(got, "latest notes"))
	assert.True(t, strings.HasPrefix(got, "…[earlier notes omitted]"))
}

type limitingMeter struct {
	metering.NoopMeter
	limits metering.AdvancedLimits
}

func (m limitingMeter) AdvancedLimits(context.Context, uuid.UUID) metering.AdvancedLimits {
	return m.limits
}

// A deployment's meter can set Advanced limits per account (metering.AdvancedLimiter); unset
// fields keep the environment's, and every value stays within the hard bounds.
func TestAdvancedLoopConfigFor_MeterLimits(t *testing.T) {
	a := &Agent{logger: zap.NewNop(), meter: metering.NoopMeter{}}
	assert.Equal(t, advancedLoopConfigFromEnv(zap.NewNop()), a.advancedLoopConfigFor(context.Background(), uuid.New()), "no limiter: the environment")

	a.meter = limitingMeter{limits: metering.AdvancedLimits{MaxSteps: 12, MaxDuration: 100 * time.Hour}}
	cfg := a.advancedLoopConfigFor(context.Background(), uuid.New())
	assert.Equal(t, 12, cfg.maxSteps)
	assert.Equal(t, maxAdvancedMaxDuration, cfg.maxDuration, "clamped to the hard bound")
	assert.Equal(t, int64(defaultAdvancedCompactTokens), cfg.compactTokens, "unset keeps the default")
}

// When the limit summary calls a tool instead of answering, the run's latest answer is the reply,
// and that answer's text is not also kept as a note.
func TestAdvancedLoop_LimitSummaryFallbackIsNotDuplicated(t *testing.T) {
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{ToolUses: []provider.ToolUse{planUse(t, "p", models.PlanItem{Content: "a", Status: models.PlanItemPending})}},
		{Text: "latest answer"},
		{ToolUses: []provider.ToolUse{unknownUse("summary-tool")}}, // the summary call misbehaves
	}})
	hooks := testAdvancedHooks(newAdvancedTranscript(nil, nil))
	hooks.config.maxSteps = 1
	a := &Agent{logger: zap.NewNop()}

	result, _, _, run, err := a.handleAdvancedLoop(context.Background(), advancedTestChatCtx(), adapter, hooks)
	require.NoError(t, err)
	assert.Equal(t, "latest answer", result.Text)
	for _, st := range run.Steps {
		for _, ev := range st.Events {
			assert.NotEqual(t, "latest answer", ev.Text, "the reply is not repeated as a note")
		}
	}
}
