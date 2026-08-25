package agent

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestToolContextPolicyFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		toolName     string
		persist      bool
		ttlTurns     int
		staleWarning bool
	}{
		{"subagent persists forever", tools.RunSubagentToolSpec.Name, true, noTTL, false},
		{"image gen persists forever", tools.GenerateImageToolSpec.Name, true, noTTL, false},
		{"find_context persists forever", tools.RecallToolSpec.Name, true, noTTL, false},
		{"web search has TTL", tools.ToolNameWebSearch, true, webSearchTTLTurns, false},
		{"create_memory dropped", tools.CreateMemoryToolSpec.Name, false, 0, false},
		{"update_scratchpad dropped", tools.UpdateScratchpadToolSpec.Name, false, 0, false},
		{"auto memory enrichment dropped", memoryEnrichmentToolCallName, false, 0, false},
		{"mcp tickets short TTL + stale", "mcp__jira__get_ticket", true, mcpMessagesTTLTurns, true},
		{"mcp messages short TTL + stale", "mcp__slack__list_messages", true, mcpMessagesTTLTurns, true},
		{"mcp file read longer TTL", "mcp__github__read_file", true, mcpFileReadTTLTurns, false},
		{"mcp live metrics dropped", "mcp__datadog__get_metrics", false, 0, false},
		{"unknown tool persists forever", "some_future_tool", true, noTTL, false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := toolContextPolicyFor(tc.toolName)
			require.Equal(t, tc.persist, got.persist, "persist")
			require.Equal(t, tc.ttlTurns, got.ttlTurns, "ttlTurns")
			require.Equal(t, tc.staleWarning, got.staleWarning, "staleWarning")
		})
	}
}

func assistantWithTools(calls ...*models.ToolCall) *models.ChatMessage {
	return &models.ChatMessage{
		ID:        uuid.New(),
		Origin:    models.MessageOriginAssistant,
		ToolCalls: calls,
	}
}

func TestSelectPersistedToolResults_TTLEviction(t *testing.T) {
	t.Parallel()

	// web_search has a 5-turn TTL. Oldest turn (age 7) must be evicted; newest kept.
	turns := make([]*models.ChatMessage, 0, 7)
	for i := 0; i < 7; i++ {
		turns = append(turns, assistantWithTools(&models.ToolCall{
			ToolName:   tools.ToolNameWebSearch,
			ToolOutput: "search result " + string(rune('A'+i)),
		}))
	}
	got := selectPersistedToolResults(turns)

	joined := strings.Join(got, "\n")
	// Newest 5 turns are ages 1-5 (kept); ages 6 and 7 (results A, B) evicted.
	require.NotContains(t, joined, "search result A")
	require.NotContains(t, joined, "search result B")
	require.Contains(t, joined, "search result C")
	require.Contains(t, joined, "search result G")
}

func TestSelectPersistedToolResults_SkipsEmptyAndErrors(t *testing.T) {
	t.Parallel()

	turns := []*models.ChatMessage{
		assistantWithTools(&models.ToolCall{ToolName: tools.RecallToolSpec.Name, ToolError: "boom"}),
		assistantWithTools(&models.ToolCall{ToolName: tools.RecallToolSpec.Name, ToolOutput: "   "}),
		assistantWithTools(&models.ToolCall{ToolName: tools.RecallToolSpec.Name, ToolOutput: "real content"}),
	}
	got := selectPersistedToolResults(turns)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "real content")
}

func TestSelectPersistedToolResults_ExcludesAutoMemoryAndWrites(t *testing.T) {
	t.Parallel()

	turns := []*models.ChatMessage{
		assistantWithTools(&models.ToolCall{ToolName: memoryEnrichmentToolCallName, ToolOutput: "Retrieved memories: x"}),
		assistantWithTools(&models.ToolCall{ToolName: tools.CreateMemoryToolSpec.Name, ToolOutput: "memory created"}),
		assistantWithTools(&models.ToolCall{ToolName: tools.RunSubagentToolSpec.Name, ToolOutput: "subagent said hi"}),
	}
	got := selectPersistedToolResults(turns)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "subagent said hi")
}

func TestSelectPersistedToolResults_StaleWarning(t *testing.T) {
	t.Parallel()

	turns := []*models.ChatMessage{
		assistantWithTools(&models.ToolCall{ToolName: "mcp__tracker__get_ticket", ToolOutput: "ISSUE-171 open"}),
	}
	got := selectPersistedToolResults(turns)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "may be stale")
	require.Contains(t, got[0], "ISSUE-171 open")
}

func TestSelectPersistedToolResults_BudgetEvictsOldestFirst(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("x", maxToolResultChars) // each result ~maxToolResultChars after truncation
	// Enough turns to blow past the total budget; subagent output persists with no TTL.
	n := (maxToolResultTotalChars / maxToolResultChars) + 3
	turns := make([]*models.ChatMessage, 0, n)
	for i := 0; i < n; i++ {
		turns = append(turns, assistantWithTools(&models.ToolCall{
			ToolName:   tools.RunSubagentToolSpec.Name,
			ToolOutput: string(rune('A'+i)) + big,
		}))
	}
	got := selectPersistedToolResults(turns)

	joined := strings.Join(got, "\n")
	// The newest result must survive; the oldest must be evicted by the budget.
	require.Contains(t, joined, string(rune('A'+n-1)))
	require.NotContains(t, joined, "A"+strings.Repeat("x", 8)) // oldest ("A"+big) dropped
	// Results are returned oldest→newest.
	require.Greater(t, len(got), 0)
}

func TestSelectPersistedToolResults_TruncatesLargeOutput(t *testing.T) {
	t.Parallel()

	turns := []*models.ChatMessage{
		assistantWithTools(&models.ToolCall{
			ToolName:   tools.RecallToolSpec.Name,
			ToolOutput: strings.Repeat("y", maxToolResultChars+500),
		}),
	}
	got := selectPersistedToolResults(turns)
	require.Len(t, got, 1)
	require.Contains(t, got[0], toolResultTruncatedNote)
}

func TestBuildAttachmentContextHint_ImagesAndDocs(t *testing.T) {
	t.Parallel()

	desc := "quarterly numbers"
	atts := []*models.FileAttachment{
		{Name: "shot.png", FileType: "image/png"},
		{Name: "report.pdf", FileType: "application/pdf", Description: &desc},
	}
	hint := buildAttachmentContextHint(atts)

	require.Contains(t, hint, "shot.png")
	require.Contains(t, hint, "[image]")
	require.Contains(t, hint, "report.pdf")
	require.Contains(t, hint, "[document]")
	require.Contains(t, hint, "quarterly numbers")
	require.Contains(t, hint, "find_context")
	require.Contains(t, hint, "visible to you inline")
}

func TestBuildAttachmentContextHint_ImagesOnlyNoDocGuidance(t *testing.T) {
	t.Parallel()

	atts := []*models.FileAttachment{{Name: "a.png", FileType: "image/png"}}
	hint := buildAttachmentContextHint(atts)
	require.Contains(t, hint, "a.png")
	require.NotContains(t, hint, "find_context")
}

func TestBuildAttachmentContextHint_Empty(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", buildAttachmentContextHint(nil))
	require.Equal(t, "", buildAttachmentContextHint([]*models.FileAttachment{nil}))
}

func TestSanitizePersistedWebSearchOutput_StripsEncryptedFields(t *testing.T) {
	t.Parallel()
	raw := "Queries: weather\n" +
		`{"encrypted_content":"enc_abc"}` + "\n" +
		"Results:\n- Weather (https://example.com/w)\n" +
		"Citations:\n- Weather (https://example.com/w)"
	got := sanitizePersistedWebSearchOutput(raw)
	require.NotContains(t, got, "enc_abc")
	require.Contains(t, got, "https://example.com/w")
}

func TestFormatPersistedToolResult_WebSearchSkipsEncryptedOnlyNoise(t *testing.T) {
	t.Parallel()
	tc := &models.ToolCall{
		ToolName: tools.ToolNameWebSearch,
		ToolOutput: "Queries: Mariners score today\n" +
			"Web search completed (1 result). Full page text is encrypted by Anthropic and is not shown here.",
	}
	got, ok := formatPersistedToolResult(tc, 1)
	require.False(t, ok)
	require.Empty(t, got)
}

func TestFormatPersistedToolResult_WebSearchKeepsPortableCitations(t *testing.T) {
	t.Parallel()
	tc := &models.ToolCall{
		ToolName: tools.ToolNameWebSearch,
		ToolOutput: "Queries: Mariners score today\n" +
			"Web search completed (1 result). Full page text is encrypted by Anthropic and is not shown here.\n\n" +
			"Citations:\n- Scores (https://example.com/scores)",
	}
	got, ok := formatPersistedToolResult(tc, 1)
	require.True(t, ok)
	require.Contains(t, got, "https://example.com/scores")
	require.NotContains(t, got, "encrypted by Anthropic")
	require.NotContains(t, got, "enc_")
}
