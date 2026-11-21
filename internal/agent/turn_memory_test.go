package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// Per-turn memory switches (turn_memory.go): recall off and save off, and that nil options, or
// both switches explicitly on, change nothing.

// countingMemoryHook is a memory-retrieval override that counts its calls: one call is the whole
// recall path (memory-query model call, embedding, lookup).
func countingMemoryHook(calls *int, mems []string) agentTestHooks {
	return agentTestHooks{GetMemoriesOverride: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) ([]string, error) {
		*calls++
		return mems, nil
	}}
}

func TestLoadTurnMemoriesFor_RecallSwitch(t *testing.T) {
	t.Parallel()
	chat := &models.Chat{ID: uuid.New(), PersonalityID: uuid.New()}
	tests := []struct {
		name       string
		opts       *models.TurnOptions
		wantRecall bool
	}{
		{"ordinary turn (nil options)", nil, true},
		{"options without memory switches", &models.TurnOptions{PostContext: "SCENE"}, true},
		{"recall explicitly on", &models.TurnOptions{MemoryRecall: ptrTo(true), MemorySave: ptrTo(true)}, true},
		{"recall off", &models.TurnOptions{MemoryRecall: ptrTo(false)}, false},
		{"save off leaves recall on", &models.TurnOptions{MemorySave: ptrTo(false)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			a := &Agent{logger: zap.NewNop(), testHooks: countingMemoryHook(&calls, []string{"User likes tea"})}
			msg := &models.ChatMessage{ChatID: chat.ID, Message: "hi", TurnOptions: tt.opts}
			memories, live, failed := a.loadTurnMemoriesFor(context.Background(), nil, uuid.New(), msg, chat)
			assert.False(t, failed)
			assert.Nil(t, live)
			if tt.wantRecall {
				assert.Equal(t, 1, calls)
				assert.Equal(t, []string{"User likes tea"}, memories)
				return
			}
			assert.Zero(t, calls, "recall off must not run retrieval (no memory-query call)")
			assert.Empty(t, memories)
		})
	}
}

// Recall off keeps the first message's user-name line: it is identity, not memory. The agent has
// no OpenAI provider, so a memory-query call would panic: none is made.
func TestRecallOff_KeepsUserNameLineWithoutAModelCall(t *testing.T) {
	a, _, _, userID, chat := newSkillsAgentFixture(t)
	require.Nil(t, a.OpenAIProvider)
	msg := &models.ChatMessage{ChatID: chat.ID, Message: "hi", TurnOptions: &models.TurnOptions{MemoryRecall: ptrTo(false)}}

	memories, live, failed := a.loadTurnMemoriesFor(context.Background(), nil, userID, msg, chat)
	require.False(t, failed)
	require.Nil(t, live)
	require.Len(t, memories, 1)
	assert.True(t, strings.HasPrefix(memories[0], userNameContextPrefix+"skills-"), memories[0])
	assert.Equal(t, memories[0], a.recognizeUserNameLine(context.Background(), userID, memories))

	// A sandboxed chat never gets the line, recall or not.
	sandboxed := *chat
	sandboxed.ContextScope = models.ContextScopeSandbox
	memories, _, _ = a.loadTurnMemoriesFor(context.Background(), nil, userID, msg, &sandboxed)
	assert.Empty(t, memories)

	// Mock/local mode adds nothing, as recall adds nothing there.
	a.mockLLM = true
	memories, _, _ = a.loadTurnMemoriesFor(context.Background(), nil, userID, msg, chat)
	assert.Empty(t, memories)
}

// Recall off also keeps out the memories earlier turns recalled and stored on their messages; the
// user-name line and other replayed context stay.
func TestBuild_RecallOffDropsReplayedMemories(t *testing.T) {
	t.Parallel()
	history := []*models.ChatMessage{
		{ID: uuid.New(), Origin: models.MessageOriginUser, Message: "earlier", AdditionalContext: []models.AdditionalContextItem{
			{Type: models.AdditionalContextTypeUserName, Content: userNameContextLine("ada")},
			memoryItem(nil, "OLD-MEMORY"),
			{Type: "LORE", Content: "OTHER-CONTEXT"},
		}},
		{ID: uuid.New(), Origin: models.MessageOriginAssistant, Message: "reply"},
	}
	b := testBuilderWithLookup(t, history, nil)
	chat := &models.Chat{ID: uuid.New()}

	ordinary, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now"})
	require.NoError(t, err)
	assert.Contains(t, segmentText(ordinary, provider.SegmentKindMemoryContext), "OLD-MEMORY")

	off, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now", WithoutMemoryRecall: true})
	require.NoError(t, err)
	memText := segmentText(off, provider.SegmentKindMemoryContext)
	assert.NotContains(t, memText, "OLD-MEMORY")
	assert.Contains(t, memText, userNameContextLine("ada"), "the user-name line is identity, not memory")
	assert.Contains(t, segmentText(off, provider.SegmentKindDeveloperContext), "OTHER-CONTEXT")
	assert.Empty(t, off.MemoryRefs)
}

func TestBuildTurn_ReportsReplayedExcludedTurns(t *testing.T) {
	t.Parallel()
	chat := &models.Chat{ID: uuid.New()}
	plain := []*models.ChatMessage{
		{ID: uuid.New(), Origin: models.MessageOriginUser, Message: "a"},
		{ID: uuid.New(), Origin: models.MessageOriginAssistant, Message: "b"},
	}
	_, built, err := testBuilderWithLookup(t, plain, nil).buildTurn(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now"})
	require.NoError(t, err)
	assert.False(t, built.replaysExcludedTurn)

	flagged := append(plain, &models.ChatMessage{ID: uuid.New(), Origin: models.MessageOriginUser, Message: "c", ExcludeFromMemory: true})
	_, built, err = testBuilderWithLookup(t, flagged, nil).buildTurn(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now"})
	require.NoError(t, err)
	assert.True(t, built.replaysExcludedTurn)

	// Carry-over from before the checkpoint counts too.
	assert.True(t, replaysExcludedTurn([][2]*models.ChatMessage{{plain[0], {ExcludeFromMemory: true}}}, nil))
	assert.False(t, replaysExcludedTurn([][2]*models.ChatMessage{{plain[0], nil}}, plain))
}

func TestBuildTurnToolPolicy_SaveOffRemovesCreateMemory(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	policyFor := func(saveOff bool) turnToolPolicy {
		chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), ToolsEnabled: true}
		return a.buildTurnToolPolicy(context.Background(), &chatContext{chat: chat, memorySaveOff: saveOff}, chat.UserID, &models.ChatMessage{})
	}
	off := policyFor(true)
	require.True(t, off.disabledTools[agenttools.CreateMemoryToolSpec.Name])
	_, offered := off.offeredAgentToolNames()[agenttools.CreateMemoryToolSpec.Name]
	assert.False(t, offered)

	on := policyFor(false)
	require.False(t, on.disabledTools[agenttools.CreateMemoryToolSpec.Name])
	_, offered = on.offeredAgentToolNames()[agenttools.CreateMemoryToolSpec.Name]
	assert.True(t, offered, "an ordinary turn keeps create_memory")
}

func TestCheckpointExtractsMemories(t *testing.T) {
	t.Parallel()
	assert.True(t, (*chatContext)(nil).checkpointExtractsMemories())
	assert.True(t, (&chatContext{}).checkpointExtractsMemories())
	assert.False(t, (&chatContext{memorySaveOff: true}).checkpointExtractsMemories())
	assert.False(t, (&chatContext{historyExcludedFromMemory: true}).checkpointExtractsMemories())
}

func TestCheckpointMayExtractMemories_NilSafe(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	assert.NotPanics(t, func() { assert.True(t, a.checkpointMayExtractMemories(nil, uuid.New())) })
	assert.False(t, a.checkpointMayExtractMemories(&chatContext{memorySaveOff: true}, uuid.New()))
}

// The nil-safe accessors the switches are read through.
func TestTurnOptionsMemoryAccessors(t *testing.T) {
	t.Parallel()
	assert.True(t, (*models.TurnOptions)(nil).SavesMemory())
	assert.True(t, (*models.TurnOptions)(nil).RecallsMemory())
	assert.True(t, (&models.TurnOptions{}).SavesMemory())
	assert.True(t, (&models.TurnOptions{MemorySave: ptrTo(true)}).SavesMemory())
	assert.False(t, (&models.TurnOptions{MemorySave: ptrTo(false)}).SavesMemory())
	assert.True(t, (&models.TurnOptions{MemoryRecall: ptrTo(true)}).RecallsMemory())
	assert.False(t, (&models.TurnOptions{MemoryRecall: ptrTo(false)}).RecallsMemory())
}

// prepareUserTurnContext's save-off decision, and the reply it leads to: the options, or the flag
// stored on a retried message (RetryUserChatMessage reloads the message, so a retry runs without
// options). Mock mode saves a real reply through saveAgentResponse.
func TestPrepareUserTurnContext_SaveOffFlagsTheReply(t *testing.T) {
	tests := []struct {
		name   string
		stored bool
		opts   *models.TurnOptions
		want   bool
	}{
		{"ordinary turn", false, nil, false},
		{"save off by the options", false, &models.TurnOptions{MemorySave: ptrTo(false)}, true},
		{"retry of a flagged message (stored flag, no options)", true, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, ds, client, userID, chat := newSkillsAgentFixture(t)
			a.mockLLM = true
			ctx := context.Background()
			model, err := client.Model.Create().SetName("gpt-5.1").SetDisplayName("GPT-5.1").SetDescription("").Save(ctx)
			require.NoError(t, err)
			require.NoError(t, client.Chat.UpdateOneID(chat.ID).SetModelID(model.ID).Exec(ctx))
			created, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{ChatID: chat.ID, Message: "hello", Origin: models.MessageOriginUser, ExcludeFromMemory: tt.stored})
			require.NoError(t, err)
			msg, err := ds.GetChatMessage(ctx, userID, created.ID)
			require.NoError(t, err)
			msg.TurnOptions = tt.opts

			chatCtx, err := a.prepareUserTurnContext(ctx, userID, msg, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.want, chatCtx.memorySaveOff)

			reply, _, err := a.generateAssistantForMessage(ctx, userID, &models.Job{UserID: userID}, msg, chatCtx, &provider.ModelContext{})
			require.NoError(t, err)
			saved, err := ds.GetChatMessage(ctx, userID, reply.ID)
			require.NoError(t, err)
			assert.Equal(t, models.MessageOriginAssistant, saved.Origin)
			assert.Equal(t, tt.want, saved.ExcludeFromMemory, "the reply is flagged like its turn")
		})
	}
}

// The context build's finding reaches the turn: buildModelContextForChatMessage sets
// historyExcludedFromMemory, which gates the checkpoint at the turn's end.
func TestBuildModelContextForChatMessage_RecordsReplayedExcludedTurn(t *testing.T) {
	t.Parallel()
	for _, flagged := range []bool{false, true} {
		history := makeHistory()
		history[1].ExcludeFromMemory = flagged
		a := testAgentWithHistoryOverride(history)
		chatCtx := makeChatContext("gpt-5.1")
		current := &models.ChatMessage{ID: uuid.New(), ChatID: chatCtx.chat.ID, Message: "now", Origin: models.MessageOriginUser}

		_, err := a.buildModelContextForChatMessage(context.Background(), uuid.New(), current, chatCtx, nil)
		require.NoError(t, err)
		assert.Equal(t, flagged, chatCtx.historyExcludedFromMemory)
		assert.Equal(t, !flagged, chatCtx.checkpointExtractsMemories())
	}
}

// A message with turn options always runs as a turn of its own: queued as a steer, a running turn
// would fold it in and ignore its options. Save off flags it; a message without options is left alone.
func TestTurnSendRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		opts        *models.TurnOptions
		steer       bool
		wantSteer   bool
		wantExclude bool
	}{
		{"ordinary message", nil, false, false, false},
		{"ordinary steer stays a steer", nil, true, true, false},
		{"options without memory switches are not a steer", &models.TurnOptions{PostContext: "SCENE"}, true, false, false},
		{"save off is flagged and not a steer", &models.TurnOptions{MemorySave: ptrTo(false)}, true, false, true},
		{"recall off is not a steer", &models.TurnOptions{MemoryRecall: ptrTo(false)}, true, false, false},
		{"save off without steer", &models.TurnOptions{MemorySave: ptrTo(false)}, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := models.ChatMessage{Message: "hi", Steer: tt.steer, TurnOptions: tt.opts}
			turnSendRequest(&req)
			assert.Equal(t, tt.wantSteer, req.Steer)
			assert.Equal(t, tt.wantExclude, req.ExcludeFromMemory)
		})
	}
}

// A recall-off turn retrieves nothing, so its reply records no "Load Memory" call for the
// user-name line alone; a recall-on turn with the same line still records one.
func TestMemoryToolCalls_RecallOffUserNameLineOnly(t *testing.T) {
	t.Parallel()
	nameLine := userNameContextLine("ada")
	recallOff := &models.TurnOptions{MemoryRecall: ptrTo(false)}

	assert.Empty(t, memoryToolCallsForChatContext(&chatContext{turnOptions: recallOff, memories: []string{nameLine}, userNameLine: nameLine}))
	assert.Empty(t, memoryToolCallsForChatContext(&chatContext{turnOptions: recallOff}))

	on := memoryToolCallsForChatContext(&chatContext{memories: []string{nameLine}, userNameLine: nameLine})
	require.Len(t, on, 1)
	assert.Equal(t, memoryEnrichmentToolCallName, on[0].ToolName)
	// Anything beyond the user-name line is still recorded.
	assert.Len(t, memoryToolCallsForChatContext(&chatContext{turnOptions: recallOff, memories: []string{nameLine, "other"}, userNameLine: nameLine}), 1)
}

// The flag is stored on the message and read back, so a later turn's context build sees it in the
// replayed history; an ordinary message stores nothing (NULL) and reads back unflagged.
func TestExcludeFromMemory_RoundTripsThroughTheDatastore(t *testing.T) {
	_, ds, client, userID, chat := newSkillsAgentFixture(t)
	ctx := context.Background()

	excluded, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{ChatID: chat.ID, Message: "secret", Origin: models.MessageOriginUser, ExcludeFromMemory: true})
	require.NoError(t, err)
	ordinary, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{ChatID: chat.ID, Message: "plain", Origin: models.MessageOriginUser})
	require.NoError(t, err)

	got, err := ds.GetChatMessage(ctx, userID, excluded.ID)
	require.NoError(t, err)
	assert.True(t, got.ExcludeFromMemory)
	got, err = ds.GetChatMessage(ctx, userID, ordinary.ID)
	require.NoError(t, err)
	assert.False(t, got.ExcludeFromMemory)
	row, err := client.ChatMessage.Get(ctx, ordinary.ID)
	require.NoError(t, err)
	assert.Nil(t, row.ExcludeFromMemory, "an ordinary message leaves the column NULL")

	b, err := newMessageContextBuilder(ds, &telemetry.Telemetry{Logger: zap.NewNop()}, nil, nil, nil)
	require.NoError(t, err)
	_, built, err := b.buildTurn(ctx, messageContextBuildRequest{UserID: userID, Chat: chat, UserPrompt: "now"})
	require.NoError(t, err)
	assert.True(t, built.replaysExcludedTurn)
}

// checkpointRequests is a fake provider server that records each request body and answers every
// request with reply(n), n counting requests from 1.
type checkpointRequests struct {
	mu     sync.Mutex
	bodies []string
}

func (c *checkpointRequests) server(t *testing.T, reply func(n int) string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies = append(c.bodies, string(raw))
		n := len(c.bodies)
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply(n)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// openAICheckpointReply answers an OpenAI Responses request with the checkpoint text.
func openAICheckpointReply(n int) string {
	return responseTextJSONBody("resp_ck_"+string(rune('a'+n)), "CHECKPOINT-TEXT")
}

// claudeCheckpointReply answers an Anthropic Messages request with the checkpoint text.
func claudeCheckpointReply(n int) string {
	return claudeMessageTextJSONBody("msg_ck_"+string(rune('a'+n)), "CHECKPOINT-TEXT")
}

// count returns how many recorded requests contain marker.
func (c *checkpointRequests) count(marker string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, b := range c.bodies {
		if strings.Contains(b, marker) {
			n++
		}
	}
	return n
}

// A checkpoint at the end of a save-off turn runs the scratchpad and the summary but extracts no
// memories; the same checkpoint on an ordinary turn extracts them.
func TestCheckpoint_SaveOffSkipsOnlyMemoryExtraction(t *testing.T) {
	for _, saveOff := range []bool{false, true} {
		t.Run(map[bool]string{false: "save on", true: "save off"}[saveOff], func(t *testing.T) {
			a, ds, client, userID, chat := newSkillsAgentFixture(t)
			ctx := context.Background()
			p, err := ds.CreatePersonality(ctx, userID, models.Personality{Name: "Ada", SystemPrompt: "You are Ada."})
			require.NoError(t, err)
			require.NoError(t, client.Chat.UpdateOneID(chat.ID).SetPersonalityID(p.ID).Exec(ctx))
			chat.PersonalityID = p.ID

			reqs := &checkpointRequests{}
			a.OpenAIProvider = newHTTPTestOpenAIProvider(reqs.server(t, openAICheckpointReply).URL)

			userMsg, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{ChatID: chat.ID, Message: "my secret", Origin: models.MessageOriginUser, ExcludeFromMemory: saveOff})
			require.NoError(t, err)
			respID := "resp_turn"
			reply, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{ChatID: chat.ID, Message: "noted", Origin: models.MessageOriginAssistant, ResponseID: &respID, ExcludeFromMemory: saveOff})
			require.NoError(t, err)

			chatCtx := &chatContext{chat: chat, memorySaveOff: saveOff}
			a.runCheckpointOpenAI(ctx, userID, userMsg, reply, chatCtx, 1, &provider.ModelContext{}, "test")

			assert.Equal(t, 1, reqs.count(scratchpadUpdateDeveloperMessage[:40]), "the scratchpad still updates")
			assert.Equal(t, 1, reqs.count(checkpointConversationSummaryInstructions[:40]), "the summary still runs")
			wantExtraction := 1
			if saveOff {
				wantExtraction = 0
			}
			assert.Equal(t, wantExtraction, reqs.count(`"MemoryExtraction"`))

			saved, err := ds.GetChat(ctx, userID, chat.ID)
			require.NoError(t, err)
			assert.Equal(t, "CHECKPOINT-TEXT", saved.CheckpointSummary, "the checkpoint completes")
			require.NotNil(t, saved.LastCheckpointAt)
			require.WithinDuration(t, time.Now(), *saved.LastCheckpointAt, time.Minute)
		})
	}
}

// The Claude checkpoint path (Claude, and every provider that takes runCheckpointClaude) has the
// same gate: the scratchpad (a Claude call) and the summary (an OpenAI call) run either way, and
// the extraction call (the Claude request with a JSON-schema output) only on a save-on turn.
func TestCheckpointClaude_SaveOffSkipsOnlyMemoryExtraction(t *testing.T) {
	for _, saveOff := range []bool{false, true} {
		t.Run(map[bool]string{false: "save on", true: "save off"}[saveOff], func(t *testing.T) {
			a, ds, client, userID, chat := newSkillsAgentFixture(t)
			ctx := context.Background()
			p, err := ds.CreatePersonality(ctx, userID, models.Personality{Name: "Ada", SystemPrompt: "You are Ada."})
			require.NoError(t, err)
			require.NoError(t, client.Chat.UpdateOneID(chat.ID).SetPersonalityID(p.ID).Exec(ctx))
			chat.PersonalityID = p.ID

			claudeReqs, openAIReqs := &checkpointRequests{}, &checkpointRequests{}
			a.ClaudeProvider = newHTTPTestClaudeProvider(claudeReqs.server(t, claudeCheckpointReply).URL)
			a.OpenAIProvider = newHTTPTestOpenAIProvider(openAIReqs.server(t, openAICheckpointReply).URL)

			userMsg, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{ChatID: chat.ID, Message: "my secret", Origin: models.MessageOriginUser, ExcludeFromMemory: saveOff})
			require.NoError(t, err)
			reply, err := ds.CreateChatMessage(ctx, userID, models.ChatMessage{ChatID: chat.ID, Message: "noted", Origin: models.MessageOriginAssistant, ExcludeFromMemory: saveOff})
			require.NoError(t, err)

			chatCtx := &chatContext{chat: chat, memorySaveOff: saveOff}
			a.runCheckpointClaude(ctx, userID, userMsg, reply, chatCtx, 1, &provider.ModelContext{}, "test")

			wantExtraction := 1
			if saveOff {
				wantExtraction = 0
			}
			assert.Equal(t, 1+wantExtraction, len(claudeReqs.bodies), "the scratchpad call, plus extraction on a save-on turn")
			assert.Equal(t, wantExtraction, claudeReqs.count(`"output_config"`), "the extraction call")
			assert.Equal(t, 1, openAIReqs.count(checkpointConversationSummaryInstructions[:40]), "the summary still runs")

			saved, err := ds.GetChat(ctx, userID, chat.ID)
			require.NoError(t, err)
			assert.Equal(t, "CHECKPOINT-TEXT", saved.CheckpointSummary, "the checkpoint completes")
			personality, err := ds.GetPersonality(ctx, userID, p.ID)
			require.NoError(t, err)
			assert.Equal(t, "CHECKPOINT-TEXT", personality.Scratchpad, "the scratchpad still updates")
		})
	}
}
