package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// turnContextFixture is a turn's context as the builder lays it out with per-turn options: the
// system material, then pre_context, the history, the turn's own developer context, post_context,
// and the user message.
func turnContextFixture() *ModelContext {
	mc := &ModelContext{}
	mc.Append(SegmentKindSystemPrompt, RoleDeveloper, "SYS", true)
	mc.Append(SegmentKindCheckpointSummary, RoleDeveloper, "SUMMARY", true)
	mc.Append(SegmentKindScratchpad, RoleDeveloper, "SCRATCH", true)
	mc.Append(SegmentKindPreContext, RoleDeveloper, "PRE-LORE", true)
	mc.AppendHistoryTurn(RoleUser, "HIST-USER", nil, true)
	mc.AppendHistoryTurn(RoleAssistant, "HIST-ASSISTANT", nil, true)
	mc.Append(SegmentKindMemoryContext, RoleDeveloper, "MEMORIES", false)
	mc.Append(SegmentKindDeveloperContext, RoleDeveloper, "TOOL-NOTES", false)
	mc.Append(SegmentKindPostContext, RoleDeveloper, "POST-SCENE", false)
	mc.AppendUserMessage(RoleUser, "NEW-TURN", nil, false)
	return mc
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// requireOrder asserts the markers appear in s in the given order.
func requireOrder(t *testing.T, s string, markers ...string) {
	t.Helper()
	last := -1
	for _, m := range markers {
		i := strings.Index(s, m)
		require.GreaterOrEqual(t, i, 0, "missing %q", m)
		require.Greater(t, i, last, "%q is out of order", m)
		last = i
	}
}

func TestTurnContext_ClaudePlacement(t *testing.T) {
	t.Parallel()
	system, messages := renderClaudeContext(turnContextFixture())

	// pre_context is the last system block, after the scratchpad and summary.
	require.Len(t, system, 4)
	assert.Equal(t, "PRE-LORE", system[3].Text)
	requireOrder(t, mustJSON(t, system), "SYS", "SUMMARY", "SCRATCH", "PRE-LORE")
	assert.NotContains(t, mustJSON(t, messages), "PRE-LORE")

	// post_context is a user message (no developer role) right before the new turn.
	require.GreaterOrEqual(t, len(messages), 2)
	post := mustJSON(t, messages[len(messages)-2])
	assert.Contains(t, post, "POST-SCENE")
	assert.Contains(t, post, `"role":"user"`)
	assert.Contains(t, mustJSON(t, messages[len(messages)-1]), "NEW-TURN")
	requireOrder(t, mustJSON(t, messages), "HIST-USER", "HIST-ASSISTANT", "MEMORIES", "TOOL-NOTES", "POST-SCENE", "NEW-TURN")
}

func TestTurnContext_ClaudeCachePrefixCoversPreContext(t *testing.T) {
	t.Parallel()
	mc := &ModelContext{}
	mc.Append(SegmentKindSystemPrompt, RoleDeveloper, "SYS", true)
	mc.Append(SegmentKindPreContext, RoleDeveloper, "PRE-LORE", true)
	mc.Append(SegmentKindPostContext, RoleDeveloper, "POST-SCENE", false)
	mc.AppendUserMessage(RoleUser, "NEW-TURN", nil, false)
	system, _ := renderClaudeContext(mc)
	require.Len(t, system, 2)
	assert.Contains(t, mustJSON(t, system[1]), "cache_control", "pre_context ends the cached prefix when nothing cacheable follows")
}

func TestTurnContext_OpenAIResponsesPlacement(t *testing.T) {
	t.Parallel()
	items := RenderOpenAIInputItems(turnContextFixture())
	all := mustJSON(t, items)
	requireOrder(t, all, "SUMMARY", "SCRATCH", "PRE-LORE", "HIST-USER", "HIST-ASSISTANT", "MEMORIES", "TOOL-NOTES", "POST-SCENE", "NEW-TURN")

	require.GreaterOrEqual(t, len(items), 4)
	pre := mustJSON(t, items[2])
	assert.Contains(t, pre, "PRE-LORE")
	assert.Contains(t, pre, `"role":"developer"`)
	post := mustJSON(t, items[len(items)-2])
	assert.Contains(t, post, "POST-SCENE")
	assert.Contains(t, post, `"role":"developer"`)
}

func TestTurnContext_ChatCompletionsPlacement(t *testing.T) {
	t.Parallel()
	messages := renderGeminiMessages(turnContextFixture())
	requireOrder(t, mustJSON(t, messages), "SYS", "SUMMARY", "SCRATCH", "PRE-LORE", "HIST-USER", "HIST-ASSISTANT", "MEMORIES", "TOOL-NOTES", "POST-SCENE", "NEW-TURN")

	// pre_context is the last system message; post_context a user message before the turn.
	require.GreaterOrEqual(t, len(messages), 5)
	pre := mustJSON(t, messages[3])
	assert.Contains(t, pre, "PRE-LORE")
	assert.Contains(t, pre, `"role":"system"`)
	assert.NotContains(t, mustJSON(t, messages[4]), `"role":"system"`)
	post := mustJSON(t, messages[len(messages)-2])
	assert.Contains(t, post, "POST-SCENE")
	assert.Contains(t, post, `"role":"user"`)
}

// Context inserted before the user message after post_context was placed (Advanced instructions,
// a compacted run's work log) goes above post_context, which stays next to the user message.
func TestInsertBeforeLastUserMessage_KeepsPostContextAdjacent(t *testing.T) {
	t.Parallel()
	mc := turnContextFixture()
	mc.InsertBeforeLastUserMessage(SegmentKindDeveloperContext, RoleDeveloper, "ADVANCED", false)
	n := len(mc.Segments)
	assert.Equal(t, SegmentKindUserMessage, mc.Segments[n-1].Kind)
	assert.Equal(t, SegmentKindPostContext, mc.Segments[n-2].Kind)
	assert.Equal(t, "ADVANCED", mc.Segments[n-3].Content)

	// Without post_context the insert lands directly before the user message, as before.
	plain := &ModelContext{}
	plain.Append(SegmentKindDeveloperContext, RoleDeveloper, "TOOL-NOTES", false)
	plain.AppendUserMessage(RoleUser, "NEW-TURN", nil, false)
	plain.InsertBeforeLastUserMessage(SegmentKindDeveloperContext, RoleDeveloper, "ADVANCED", false)
	require.Len(t, plain.Segments, 3)
	assert.Equal(t, "ADVANCED", plain.Segments[1].Content)
}

func TestWithoutTurnContext(t *testing.T) {
	t.Parallel()

	ordinary := &ModelContext{}
	ordinary.Append(SegmentKindSystemPrompt, RoleDeveloper, "SYS", true)
	ordinary.AppendUserMessage(RoleUser, "NEW-TURN", nil, false)
	assert.Same(t, ordinary, ordinary.WithoutTurnContext(), "a context without turn context is returned as is")

	mc := turnContextFixture()
	before := mustJSON(t, mc)
	stripped := mc.WithoutTurnContext()
	out := mustJSON(t, stripped)
	assert.NotContains(t, out, "PRE-LORE")
	assert.NotContains(t, out, "POST-SCENE")
	assert.Contains(t, out, "NEW-TURN")
	assert.Len(t, stripped.Segments, len(mc.Segments)-2)
	assert.Equal(t, before, mustJSON(t, mc), "the turn's own context is left untouched")
}
