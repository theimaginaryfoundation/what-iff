package agent

import (
	"context"
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
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// queuedSteerStore hands out its queued messages once, on the claim after `after` earlier claims.
type queuedSteerStore struct {
	mu     sync.Mutex
	queued []datastore.SteerMessage
	after  int
	calls  int
	err    error
}

func (s *queuedSteerStore) ClaimSteerMessages(_ context.Context, _, _, _ uuid.UUID, _ time.Time) ([]datastore.SteerMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if s.calls <= s.after {
		return nil, nil
	}
	out := s.queued
	s.queued = nil
	return out, nil
}

func steerTestAgent(store steerStore) *Agent {
	return &Agent{logger: zap.NewNop(), lifecycleCtx: context.Background(), testHooks: agentTestHooks{SteerStore: store}}
}

func TestTakeSteering_FramesClaimedMessages(t *testing.T) {
	store := &queuedSteerStore{queued: []datastore.SteerMessage{{Message: "use the blue one"}, {Message: "  "}, {Message: "and hurry"}}}
	a := steerTestAgent(store)
	chatCtx := promptTestChatCtx()

	note := a.takeSteering(context.Background(), chatCtx)
	assert.Contains(t, note, "sent these 2 messages while you were working")
	assert.Contains(t, note, "<user_message>\nuse the blue one\n</user_message>")
	assert.Equal(t, []string{"use the blue one", "and hurry"}, chatCtx.steerNotes)
	assert.Empty(t, a.takeSteering(context.Background(), chatCtx), "claimed once")
}

func TestTakeSteering_OnlyOnInteractiveTurnsAndSurvivesErrors(t *testing.T) {
	store := &queuedSteerStore{queued: []datastore.SteerMessage{{Message: "hi"}}}
	a := steerTestAgent(store)
	chatCtx := promptTestChatCtx()
	chatCtx.promptJob = nil
	assert.Empty(t, a.takeSteering(context.Background(), chatCtx))
	assert.Zero(t, store.calls, "no claim without a web turn to deliver to")

	store.err = errors.New("db down")
	assert.Empty(t, a.takeSteering(context.Background(), promptTestChatCtx()))
}

func TestWithSteerNote(t *testing.T) {
	results := []provider.ToolResult{{ID: "a", Output: "first"}, {ID: "b", Output: "second"}}
	withSteerNote(results, "NOTE")
	assert.Equal(t, "first", results[0].Output)
	assert.Equal(t, "second\n\nNOTE", results[1].Output)
	withSteerNote(nil, "NOTE") // no panic
}

// A follow-up sent mid-round reaches the next call on that round's tool results, while the saved
// tool call keeps the tool's own output.
func TestToolRounds_SteeringRidesOnToolResults(t *testing.T) {
	store := &queuedSteerStore{queued: []datastore.SteerMessage{{Message: "actually, French please"}}}
	a := steerTestAgent(store)
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{Text: "Looking.", ToolUses: []provider.ToolUse{unknownUse("t1")}},
		{Text: "Voilà."},
	}})
	chatCtx := promptTestChatCtx()
	chatCtx.setOfferedTools(map[string]struct{}{})

	res, calls, _, err := a.handleAgentLoop(context.Background(), chatCtx, adapter)
	require.NoError(t, err)
	assert.Equal(t, "Voilà.", res.Text)
	batches := adapter.AppendedBatches()
	require.Len(t, batches, 1)
	assert.Contains(t, batches[0][0].Output, "actually, French please")
	require.Len(t, calls, 1)
	assert.NotContains(t, calls[0].ToolError+calls[0].ToolOutput, "French", "the saved call keeps the tool's output")
}

// An Advanced run that would be done goes on when a follow-up arrives with its answer.
func TestAdvancedLoop_SteeringAfterAnswerContinuesTheRun(t *testing.T) {
	store := &queuedSteerStore{queued: []datastore.SteerMessage{{Message: "also add a summary"}}}
	a := steerTestAgent(store)
	adapter := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeScripted, Script: []provider.MockScriptStep{
		{Text: "Here is the report."},
		{Text: "Here is the report, with a summary."},
	}})
	chatCtx := promptTestChatCtx()
	chatCtx.turnSettings = models.TurnSettings{AgentLoop: models.AgentLoopAdvanced}
	chatCtx.setOfferedTools(map[string]struct{}{agenttools.UpdatePlanToolSpec.Name: {}})
	tr := newAdvancedTranscript(nil, nil)

	result, _, _, run, err := a.handleAdvancedLoop(context.Background(), chatCtx, adapter, testAdvancedHooks(tr))
	require.NoError(t, err)
	assert.Equal(t, "Here is the report, with a summary.", result.Text)
	assert.Equal(t, models.AgentRunEndDone, run.EndReason)
	require.Len(t, adapter.Continuations(), 1)
	assert.Contains(t, adapter.Continuations()[0], "also add a summary")
}

func TestAdvancedRun_WorkLogKeepsSteering(t *testing.T) {
	r := &advancedRun{chatCtx: &chatContext{steerNotes: []string{"skip the appendix"}}}
	assert.Contains(t, r.workLog(3), "- skip the appendix")
}
