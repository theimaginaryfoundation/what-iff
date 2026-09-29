package agent

import (
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
)

const claudeBetaWebSearchMessageJSON = `{
	"type":"message",
	"role":"assistant",
	"content":[
		{"type":"server_tool_use","id":"srv_01","name":"web_search","input":{"query":"weather NYC"},"caller":{"type":"direct"}},
		{"type":"web_search_tool_result","tool_use_id":"srv_01","content":[{"type":"web_search_result","title":"Weather","url":"https://example.com/w","encrypted_content":"enc","page_age":"1d"}]},
		{"type":"text","text":"It is sunny.","citations":[{"type":"web_search_result_location","url":"https://example.com/w","title":"Weather","cited_text":"sunny","encrypted_index":"idx"}]}
	]
}`

func TestWebSearchToolCallsFromClaudeBetaMessages_ExtractsToolCall(t *testing.T) {
	t.Parallel()

	var msg anthropic.BetaMessage
	require.NoError(t, json.Unmarshal([]byte(claudeBetaWebSearchMessageJSON), &msg))

	got := webSearchToolCallsFromClaudeBetaMessages(&msg)
	require.Len(t, got, 1)
	require.Equal(t, tools.ToolNameWebSearch, got[0].ToolName)
	require.Equal(t, "weather NYC", got[0].ToolInput)
	require.Contains(t, got[0].ToolOutput, "https://example.com/w")
	require.Contains(t, got[0].ToolOutput, "Citations:")
}

func TestClaudeBetaWebSearchQueryForToolUseID_MatchesByID(t *testing.T) {
	t.Parallel()

	var msg anthropic.BetaMessage
	require.NoError(t, json.Unmarshal([]byte(claudeBetaWebSearchMessageJSON), &msg))

	require.Equal(t, "weather NYC", claudeBetaWebSearchQueryForToolUseID(&msg, "srv_01"))
	require.Equal(t, "", claudeBetaWebSearchQueryForToolUseID(&msg, "other-id"))
}

func TestFormatClaudeBetaWebSearchCitations_DedupesRepeatedCitations(t *testing.T) {
	t.Parallel()

	raw := `{
		"type":"message",
		"role":"assistant",
		"content":[
			{"type":"text","text":"a","citations":[
				{"type":"web_search_result_location","url":"https://example.com/w","title":"Weather","cited_text":"sunny","encrypted_index":"idx"},
				{"type":"web_search_result_location","url":"https://example.com/w","title":"Weather","cited_text":"sunny again","encrypted_index":"idx2"}
			]}
		]
	}`
	var msg anthropic.BetaMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &msg))

	got := formatClaudeBetaWebSearchCitations(&msg)
	require.Equal(t, 1, countOccurrences(got, "https://example.com/w"))
}

func countOccurrences(haystack, needle string) int {
	count := 0
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			count++
		}
	}
	return count
}
