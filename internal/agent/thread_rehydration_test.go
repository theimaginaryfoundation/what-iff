package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func turnMsgs(n int) []models.ChatMessage {
	// n user+assistant pairs in chronological order.
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	var msgs []models.ChatMessage
	for i := 0; i < n; i++ {
		msgs = append(msgs,
			models.ChatMessage{Message: "u", Origin: models.MessageOriginUser, SentAt: base.Add(time.Duration(2*i) * time.Minute)},
			models.ChatMessage{Message: "a", Origin: models.MessageOriginAssistant, SentAt: base.Add(time.Duration(2*i+1) * time.Minute)},
		)
	}
	return msgs
}

func TestSplitForRehydration_ShortThreadNeedsNoSummary(t *testing.T) {
	t.Parallel()
	// 5 user turns with keepTurns=5 -> not more than keep -> no summary needed.
	_, _, needs := splitForRehydration(turnMsgs(5), 5)
	require.False(t, needs)
}

func TestSplitForRehydration_LongThreadSplitsAtNMinus5(t *testing.T) {
	t.Parallel()
	msgs := turnMsgs(12) // 12 user turns
	summarize, windowStartIdx, needs := splitForRehydration(msgs, 5)
	require.True(t, needs)

	// Window starts at the 8th user message (index of user turn 8 = (12-5)=7th turn, 0-based user #7 -> msg index 14).
	require.Equal(t, models.MessageOriginUser, msgs[windowStartIdx].Origin)
	// Everything before the window is summarized: 7 full turns = 14 messages.
	require.Len(t, summarize, 14)
	// The kept window is the last 5 turns = 10 messages.
	require.Len(t, msgs[windowStartIdx:], 10)
}

func TestSplitForRehydration_CountsAssistantMessages(t *testing.T) {
	t.Parallel()
	msgs := turnMsgs(10)
	summarize, _, needs := splitForRehydration(msgs, 5)
	require.True(t, needs)
	// 10 turns - 5 kept = 5 summarized turns -> 5 assistant messages.
	require.Equal(t, 5, countAssistantMessages(summarize))
}

func TestChunkMessagesByChars_GroupsWithinBudget(t *testing.T) {
	t.Parallel()
	msgs := []models.ChatMessage{
		{Message: strings.Repeat("x", 100), Origin: models.MessageOriginUser},
		{Message: strings.Repeat("y", 100), Origin: models.MessageOriginAssistant},
		{Message: strings.Repeat("z", 100), Origin: models.MessageOriginUser},
	}
	chunks := chunkMessagesByChars(msgs, 150)
	// Each message ~116 chars; budget 150 forces one message per chunk.
	require.Len(t, chunks, 3)
}

func TestChunkMessagesByChars_SingleChunkWhenUnderBudget(t *testing.T) {
	t.Parallel()
	msgs := turnMsgs(3)
	chunks := chunkMessagesByChars(msgs, 1_000_000)
	require.Len(t, chunks, 1)
}

func TestNormalizeExtractedMemories_TrimsDropsBlanksAndCaps(t *testing.T) {
	t.Parallel()
	in := []models.ExtractedMemory{
		{Content: "  likes Go  ", Scope: "User"},
		{Content: "   ", Scope: "Chat"},       // dropped (blank)
		{Content: "uses Postgres", Scope: ""}, // scope coerced to Chat
		{Content: "weird scope", Scope: "Galaxy"},
		{Content: "extra one", Scope: "User"},
	}
	out := memoryutil.NormalizeExtractedMemories(in, 3)
	require.Len(t, out, 3)
	require.Equal(t, "likes Go", out[0].Content)
	require.Equal(t, "User", out[0].Scope)
	require.Equal(t, "uses Postgres", out[1].Content)
	require.Equal(t, "Chat", out[1].Scope) // empty coerced
	require.Equal(t, "Chat", out[2].Scope) // unknown coerced
}

func TestNormalizeExtractedMemories_NoCapWhenMaxNonPositive(t *testing.T) {
	t.Parallel()
	in := []models.ExtractedMemory{
		{Content: "a", Scope: "User"},
		{Content: "b", Scope: "Chat"},
	}
	out := memoryutil.NormalizeExtractedMemories(in, 0)
	require.Len(t, out, 2)
}

func TestBuildTranscript_RoleTaggedAndSkipsEmpty(t *testing.T) {
	t.Parallel()
	msgs := []models.ChatMessage{
		{Message: "hello", Origin: models.MessageOriginUser},
		{Message: "  ", Origin: models.MessageOriginAssistant}, // skipped (blank)
		{Message: "world", Origin: models.MessageOriginAssistant},
	}
	out := buildTranscript(msgs)
	require.Contains(t, out, "User: hello")
	require.Contains(t, out, "Assistant: world")
	require.NotContains(t, out, "Assistant:  ")
}
