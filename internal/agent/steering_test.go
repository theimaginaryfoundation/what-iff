package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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

	texts := a.takeSteering(context.Background(), chatCtx)
	assert.Equal(t, []string{"use the blue one", "and hurry"}, texts, "blank messages are dropped")
	assert.Equal(t, []string{"use the blue one", "and hurry"}, chatCtx.steerNotes)
	assert.Empty(t, a.takeSteering(context.Background(), chatCtx), "claimed once")
}

func TestTakeSteering_KeepsTheDocumentCommentAMessageWasSentFrom(t *testing.T) {
	store := &queuedSteerStore{queued: []datastore.SteerMessage{{Message: "@Iris tighten this", FileComments: []string{"A comment on plan.md, on: \"major features\""}}}}
	texts := steerTestAgent(store).takeSteering(context.Background(), promptTestChatCtx())
	assert.Equal(t, []string{"@Iris tighten this\n\nA comment on plan.md, on: \"major features\""}, texts)
}

func TestSteering_ALongThreadDoesNotPushOutTheUsersWords(t *testing.T) {
	thread := "A comment on plan.md, on \"major features\".\n" + strings.Repeat("- Pat: an earlier comment\n", 3000) + "- Pat: the latest"
	store := &queuedSteerStore{queued: []datastore.SteerMessage{{Message: "@Iris tighten this", FileComments: []string{thread}}}}
	texts := steerTestAgent(store).takeSteering(context.Background(), promptTestChatCtx())
	for _, note := range []string{steerUserTurn(texts), steerToolNote(texts)} {
		assert.LessOrEqual(t, utf8.RuneCountInString(note), maxSteerNoteChars)
		assert.Contains(t, note, "@Iris tighten this", "the user's words")
		assert.Contains(t, note, "on plan.md", "what the comment is on")
		assert.Contains(t, note, "the latest", "the latest comment")
	}
	assert.True(t, strings.HasPrefix(steerUserTurn(texts), steerMidRunTag), "the mid-run tag")
	assert.Contains(t, steerToolNote(texts), "comes from them, not from a tool")

	// Many long messages: the framing stays, the oldest go.
	long := []string{strings.Repeat("a", 6000), strings.Repeat("b", 6000)}
	assert.True(t, strings.HasPrefix(steerUserTurn(long), steerMidRunTag))
	assert.True(t, strings.HasPrefix(steerToolNote(long), "[The user sent these 2 messages"))
	assert.True(t, strings.HasSuffix(steerToolNote(long), "answer it in your reply."))
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

func TestSteerUserTurn_TagsTheMessagesAsMidRun(t *testing.T) {
	turn := steerUserTurn([]string{"use the blue one", "and hurry"})
	assert.Equal(t, steerMidRunTag+"\n\nuse the blue one\n\nand hurry", turn)

	note := steerToolNote([]string{"use the blue one", "and hurry"})
	assert.Contains(t, note, "sent these 2 messages mid-run")
	assert.Contains(t, note, "<user_message>\nuse the blue one\n</user_message>")
}

// toolOnlyAdapter hides MockAdapter's AppendUserNote, standing in for an adapter without it.
type toolOnlyAdapter struct{ inner *provider.MockAdapter }

func (t toolOnlyAdapter) Call(ctx context.Context) (*provider.GenerateResponse, []provider.ToolUse, error) {
	return t.inner.Call(ctx)
}
func (t toolOnlyAdapter) AppendToolResults(r []provider.ToolResult) { t.inner.AppendToolResults(r) }
func (t toolOnlyAdapter) ForceFinalResponse(ctx context.Context) (*provider.GenerateResponse, error) {
	return t.inner.ForceFinalResponse(ctx)
}
func (t toolOnlyAdapter) SetTextDeltaHandler(h func(string)) { t.inner.SetTextDeltaHandler(h) }
func (t toolOnlyAdapter) WebSearchCompletedCount() int       { return 0 }

func TestAppendToolResultsWithSteering_FallsBackToTheToolResult(t *testing.T) {
	mock := provider.NewMockAdapter(provider.MockAdapterConfig{Mode: provider.MockModeEcho})
	appendToolResultsWithSteering(toolOnlyAdapter{mock}, []provider.ToolResult{{ID: "a", Output: "out"}}, []string{"stop after this"})
	require.Len(t, mock.AppendedBatches(), 1)
	assert.Contains(t, mock.AppendedBatches()[0][0].Output, "stop after this")
	assert.Contains(t, mock.AppendedBatches()[0][0].Output, "not from a tool")
	assert.Empty(t, mock.UserNotes())
}

func TestWithSteerNote(t *testing.T) {
	results := []provider.ToolResult{{ID: "a", Output: "first"}, {ID: "b", Output: "second"}}
	withSteerNote(results, "NOTE")
	assert.Equal(t, "first", results[0].Output)
	assert.Equal(t, "second\n\nNOTE", results[1].Output)
	withSteerNote(nil, "NOTE") // no panic
}

// A follow-up sent mid-round reaches the next call as a user turn of its own, tagged as mid-run,
// after that round's tool results, which (like the saved tool call) keep the tool's own output.
func TestToolRounds_SteeringIsAUserTurnAfterTheToolResults(t *testing.T) {
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
	assert.NotContains(t, batches[0][0].Output, "French", "the tool result stays the tool's")
	require.Equal(t, []string{steerMidRunTag + "\n\nactually, French please"}, adapter.UserNotes())
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
