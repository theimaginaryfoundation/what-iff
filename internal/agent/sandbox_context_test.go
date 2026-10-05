package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// Sandboxed-chat context assembly: persisted memory replay, tool-result replay, the scratchpad,
// the checkpoint plan, the manifest and merge confinement.

func memoryItem(id *uuid.UUID, content string) models.AdditionalContextItem {
	return models.AdditionalContextItem{Type: models.AdditionalContextTypeMemory, Content: content, MemoryID: id, Scope: "User"}
}

func testBuilderWithLookup(t *testing.T, history []*models.ChatMessage, lookup memoryIDLookup) *messageContextBuilder {
	t.Helper()
	b, err := newMessageContextBuilder(nil, &telemetry.Telemetry{Logger: zap.NewNop()}, nil,
		func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int, *time.Time, string) []*models.ChatMessage {
			return history
		}, nil)
	require.NoError(t, err)
	b.memoryIDLookup = lookup
	return b
}

func segmentText(mc *provider.ModelContext, kind provider.ModelContextSegmentKind) string {
	var parts []string
	for _, s := range mc.Segments {
		if s.Kind == kind {
			parts = append(parts, s.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func TestBuild_SandboxedChatRefiltersReplayedMemoriesToItsOwn(t *testing.T) {
	t.Parallel()
	ownID, outsideID := uuid.New(), uuid.New()
	history := []*models.ChatMessage{{
		ID: uuid.New(), Origin: models.MessageOriginUser, Message: "earlier question",
		AdditionalContext: []models.AdditionalContextItem{
			memoryItem(&ownID, "A fact learned in this thread"),
			memoryItem(&outsideID, "A private diagnosis loaded back before the chat was sandboxed"),
			memoryItem(nil, "A memory line whose id was never matched"),
			{Type: models.AdditionalContextTypeUserName, Content: "The user's name is Ada"},
			memoryItem(nil, "The user's name is Mallory [stored_at=2026-01-01T00:00:00Z age_days=3]"), // a memory that merely starts like the name line
			memoryItem(nil, "The user's name is Eve"),                                                 // a legacy MEMORY-typed item cannot be told from a memory by its text
			{Type: "FILE_HINT", Content: "not a memory"},
		},
	}}
	chat := &models.Chat{ID: uuid.New(), SystemPrompt: "p", Sandboxed: true}
	var asked []uuid.UUID
	var askedChat uuid.UUID
	b := testBuilderWithLookup(t, history, func(_ context.Context, _, chatID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
		asked, askedChat = ids, chatID
		return map[uuid.UUID]struct{}{ownID: {}}, nil // the datastore would drop the one created elsewhere
	})

	mc, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now"})
	require.NoError(t, err)

	mem := segmentText(mc, provider.SegmentKindMemoryContext)
	require.Contains(t, mem, "A fact learned in this thread")
	require.NotContains(t, mem, "Ada", "a sandboxed chat is never given the owner's name, even replayed")
	require.NotContains(t, mem, "Mallory", "a memory that starts with the name-line text is not the name line")
	require.NotContains(t, mem, "Eve", "legacy items fail closed")
	require.NotContains(t, mem, "private diagnosis", "a memory created outside this chat does not replay")
	require.NotContains(t, mem, "never matched", "an unclassifiable memory line fails closed")
	require.Equal(t, chat.ID, askedChat, "the lookup is for this chat's own memories")
	require.ElementsMatch(t, []uuid.UUID{ownID, outsideID}, asked, "one lookup covers every persisted id")
	require.Contains(t, segmentText(mc, provider.SegmentKindDeveloperContext), "not a memory", "non-memory context is untouched")
	for _, ref := range mc.MemoryRefs {
		require.NotEqual(t, outsideID.String(), ref.MemoryID)
	}

	// Control: same history for an ordinary chat, no lookup, everything replays, name included.
	asked = nil
	open := &models.Chat{ID: chat.ID, SystemPrompt: "p"}
	mc, err = b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: open, UserPrompt: "now"})
	require.NoError(t, err)
	mem = segmentText(mc, provider.SegmentKindMemoryContext)
	require.Contains(t, mem, "private diagnosis")
	require.Contains(t, mem, "never matched")
	require.Contains(t, mem, "Ada")
	require.Nil(t, asked, "an ordinary chat never pays for the lookup")
}

// A sandboxed chat never gets the owner's name line: not on its first turn, and not replayed from
// a turn persisted before it was sandboxed. An ordinary chat does.
func TestUserNameLine_NotGivenToASandboxedChat(t *testing.T) {
	t.Parallel()
	require.Empty(t, initialTurnContextLines("Ada", 0, true))
	require.Equal(t, []string{"The user's name is Ada"}, initialTurnContextLines("Ada", 0, false))
	require.Empty(t, initialTurnContextLines("Ada", 1, false), "only the first request carries it")
	require.False(t, userNameLineAllowed(true))
	require.True(t, userNameLineAllowed(false))
}

func TestBuild_SandboxedChatFailsClosedWhenTheLookupFails(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	history := []*models.ChatMessage{{ID: uuid.New(), Origin: models.MessageOriginUser, Message: "q",
		AdditionalContext: []models.AdditionalContextItem{memoryItem(&id, "Some memory")}}}
	b := testBuilderWithLookup(t, history, func(context.Context, uuid.UUID, uuid.UUID, []uuid.UUID) (map[uuid.UUID]struct{}, error) {
		return nil, errors.New("db down")
	})
	chat := &models.Chat{ID: uuid.New(), Sandboxed: true}
	mc, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now"})
	require.NoError(t, err, "a failed re-check must not fail the turn")
	require.NotContains(t, segmentText(mc, provider.SegmentKindMemoryContext), "Some memory")
}

func TestBuild_SandboxedChatCurrentTurnMemoriesAreNotRefiltered(t *testing.T) {
	t.Parallel()
	// This turn's prefetch was filtered in SQL when it was retrieved; the replay filter must not
	// drop it just because the line carries no persisted id.
	b := testBuilderWithLookup(t, nil, func(context.Context, uuid.UUID, uuid.UUID, []uuid.UUID) (map[uuid.UUID]struct{}, error) {
		return map[uuid.UUID]struct{}{}, nil
	})
	chat := &models.Chat{ID: uuid.New(), Sandboxed: true}
	mc, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now", Memories: []string{"fresh prefetched memory"}})
	require.NoError(t, err)
	require.Contains(t, segmentText(mc, provider.SegmentKindMemoryContext), "fresh prefetched memory")
}

// A sandboxed chat never gets the personality scratchpad, but its persisted tool results replay
// like any chat's (no re-fetching, no lost sub-agent output across turns).
func TestBuild_SandboxedChatGetsNoScratchpadButReplaysToolResults(t *testing.T) {
	t.Parallel()
	history := []*models.ChatMessage{
		{ID: uuid.New(), Origin: models.MessageOriginUser, Message: "look something up"},
		{
			ID: uuid.New(), Origin: models.MessageOriginAssistant, Message: "done",
			ToolCalls: []*models.ToolCall{
				{ToolName: agenttools.RecallToolSpec.Name, ToolOutput: "find_context result: a fact learned in this thread"},
				{ToolName: agenttools.GenerateImageToolSpec.Name, ToolOutput: "image generated: a fox"},
			},
		},
	}
	b := testBuilderWithLookup(t, history, nil)
	render := func(chat *models.Chat) string {
		mc, err := b.build(context.Background(), messageContextBuildRequest{UserID: uuid.New(), Chat: chat, UserPrompt: "now"})
		require.NoError(t, err)
		var all strings.Builder
		for _, s := range mc.Segments {
			all.WriteString(s.Content + "\n")
		}
		return all.String()
	}

	text := render(&models.Chat{ID: uuid.New(), SystemPrompt: "p", Scratchpad: "PERSONALITY SCRATCHPAD", Sandboxed: true})
	require.NotContains(t, text, "PERSONALITY SCRATCHPAD", "belt and braces: even a Chat that carries a scratchpad does not inject it")
	require.Contains(t, text, "a fact learned in this thread")
	require.Contains(t, text, "a fox")

	// Control: an ordinary chat gets the scratchpad too.
	text = render(&models.Chat{ID: uuid.New(), SystemPrompt: "p", Scratchpad: "PERSONALITY SCRATCHPAD"})
	require.Contains(t, text, "PERSONALITY SCRATCHPAD")
	require.Contains(t, text, "a fact learned in this thread")
}

func TestCheckpointSteps(t *testing.T) {
	t.Parallel()
	persona := uuid.New()
	cases := []struct {
		name                            string
		chat                            *models.Chat
		scratchpadWritten               bool
		wantScratchpad, wantExtractions bool
	}{
		{"ordinary with personality runs scratchpad, extracts after it succeeds", &models.Chat{PersonalityID: persona}, true, true, true},
		{"ordinary waits for the scratchpad delta", &models.Chat{PersonalityID: persona}, false, true, false},
		{"ordinary without a personality has neither step", &models.Chat{}, false, false, false},
		{"sandboxed skips the scratchpad but still extracts", &models.Chat{PersonalityID: persona, Sandboxed: true}, false, false, true},
		{"sandboxed without a personality still extracts", &models.Chat{Sandboxed: true}, false, false, true},
	}
	for _, tc := range cases {
		scratch, extract := checkpointSteps(tc.chat, tc.scratchpadWritten)
		assert.Equal(t, tc.wantScratchpad, scratch, tc.name)
		assert.Equal(t, tc.wantExtractions, extract, tc.name)
	}
}

func TestBuildTurnToolPolicy_SandboxedChatLosesTheScratchpadTool(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	policyFor := func(sandboxed bool) turnToolPolicy {
		chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), ToolsEnabled: true, Sandboxed: sandboxed}
		return a.buildTurnToolPolicy(context.Background(), &chatContext{chat: chat}, chat.UserID, &models.ChatMessage{})
	}
	require.True(t, policyFor(true).disabledTools[agenttools.UpdateScratchpadToolSpec.Name])
	require.False(t, policyFor(false).disabledTools[agenttools.UpdateScratchpadToolSpec.Name])
}

func TestContextInputs_RecordsThatTheChatWasSandboxed(t *testing.T) {
	t.Parallel()
	// A sandboxed chat always records, even with nothing else to report, so it is on file that it was one.
	in := contextInputs(&chatContext{chat: &models.Chat{Sandboxed: true}}, nil)
	require.NotNil(t, in)
	assert.True(t, in.Sandboxed)

	mem := &models.Memory{ID: uuid.New(), Scope: "Chat"}
	in = contextInputs(&chatContext{chat: &models.Chat{Sandboxed: true}, liveMemories: []*models.Memory{mem}, prefetchedMemoryCount: 1}, nil)
	require.NotNil(t, in)
	assert.True(t, in.Sandboxed)

	// Ordinary chats record the manifest when it has something to say, and stay nil otherwise.
	in = contextInputs(&chatContext{chat: &models.Chat{}, liveMemories: []*models.Memory{mem}, prefetchedMemoryCount: 1}, nil)
	require.NotNil(t, in)
	assert.False(t, in.Sandboxed)
	assert.Nil(t, contextInputs(&chatContext{chat: &models.Chat{}}, nil))

	raw, err := json.Marshal(contextInputs(&chatContext{chat: &models.Chat{Sandboxed: true}}, nil))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"sandboxed":true`)
}

func TestMemoryExtractionDeveloperMessage_SandboxedChatHasNoScratchpadDelta(t *testing.T) {
	t.Parallel()
	sandboxed := &chatContext{chat: &models.Chat{Sandboxed: true}}
	open := &chatContext{chat: &models.Chat{}}
	require.NotContains(t, memoryExtractionDeveloperMessageFor(sandboxed), "scratchpad")
	require.Contains(t, memoryExtractionDeveloperMessageFor(open), "scratchpad")
	require.Equal(t, memoryExtractionDeveloperMessage, memoryExtractionDeveloperMessageFor(nil))
}

func TestSubagentScratchpad_SandboxedChatSendsNone(t *testing.T) {
	t.Parallel()
	require.Equal(t, "notes", subagentScratchpad(&models.Chat{}, "notes"))
	require.Empty(t, subagentScratchpad(&models.Chat{Sandboxed: true}, "notes"))
	// The context built from it then carries no scratchpad segment.
	mc := buildSubagentModelContext("prompt", subagentScratchpad(&models.Chat{Sandboxed: true}, "notes"), "task")
	require.Empty(t, segmentText(mc, provider.SegmentKindScratchpad))
}

// A sandboxed chat's checkpoint may not fold, rewrite or retire memories it did not create.
func TestSandboxedCompactionLiveMemories_KeepsOnlyThisChatsMemories(t *testing.T) {
	t.Parallel()
	chatID, otherChat := uuid.New(), uuid.New()
	mine := &models.Memory{ID: uuid.New(), Content: "learned here", Scope: "Chat", ChatID: chatID}
	owners := &models.Memory{ID: uuid.New(), Content: "owner's fact", Scope: "User"}
	elsewhere := &models.Memory{ID: uuid.New(), Content: "other chat", Scope: "Chat", ChatID: otherChat}
	extracted := []models.ExtractedMemory{
		{Content: "the user prefers metric units", Scope: "User", Confidence: models.MemoryConfidenceHigh},
		{Content: "likes tea", Scope: "Chat", Confidence: models.MemoryConfidenceLow},
	}

	gotLive := sandboxedCompactionLiveMemories(chatID, []*models.Memory{mine, owners, nil, elsewhere})
	require.Equal(t, []*models.Memory{mine}, gotLive)

	// What the planner then sees: only this chat's memory and the new extractions, never the owner's.
	candidates := buildMemoryMergeCandidates(nil, gotLive, memoryutil.CollapseExtractedMemories(extracted))
	for _, c := range candidates {
		require.NotContains(t, c.Content, "owner's fact")
		require.NotContains(t, c.Content, "other chat")
	}
	require.Len(t, candidates, 3)

	// A chat with no id keeps nothing (fail closed).
	require.Empty(t, sandboxedCompactionLiveMemories(uuid.Nil, []*models.Memory{{ID: uuid.New()}}))
}

// A sandboxed chat's fold and link groups are confined to the chat in the datastore; an ordinary
// chat's are not.
func TestMergeOptions_AreOnlyForSandboxedChats(t *testing.T) {
	t.Parallel()
	require.Len(t, mergeOptions(true), 1)
	require.Nil(t, mergeOptions(false))
}
