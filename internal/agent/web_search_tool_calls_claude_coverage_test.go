package agent

import (
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
)

const claudeWebSearchMessageJSON = `{
	"type":"message",
	"role":"assistant",
	"content":[
		{"type":"server_tool_use","id":"srv_01","name":"web_search","input":{"query":"weather NYC"},"caller":{"type":"direct"}},
		{"type":"web_search_tool_result","tool_use_id":"srv_01","content":[{"type":"web_search_result","title":"Weather","url":"https://example.com/w","encrypted_content":"enc","page_age":"1d"}]},
		{"type":"text","text":"It is sunny.","citations":[{"type":"web_search_result_location","url":"https://example.com/w","title":"Weather","cited_text":"sunny","encrypted_index":"idx"}]}
	]
}`

// --- webSearchToolCallsFromClaudeMessages ---

func TestWebSearchToolCallsFromClaudeMessages_NilMessagesAreSkipped(t *testing.T) {
	t.Parallel()
	got := webSearchToolCallsFromClaudeMessages(nil, nil)
	require.Nil(t, got)
}

func TestWebSearchToolCallsFromClaudeMessages_ExtractsToolCall(t *testing.T) {
	t.Parallel()
	var msg anthropic.Message
	require.NoError(t, json.Unmarshal([]byte(claudeWebSearchMessageJSON), &msg))

	got := webSearchToolCallsFromClaudeMessages(&msg)
	require.Len(t, got, 1)
	require.Equal(t, tools.ToolNameWebSearch, got[0].ToolName)
	require.Equal(t, "weather NYC", got[0].ToolInput)
	require.Contains(t, got[0].ToolOutput, "https://example.com/w")
}

func TestWebSearchToolCallsFromClaudeMessages_NoWebSearchBlockReturnsNil(t *testing.T) {
	t.Parallel()
	var msg anthropic.Message
	require.NoError(t, json.Unmarshal([]byte(`{"type":"message","role":"assistant","content":[{"type":"text","text":"hi"}]}`), &msg))

	got := webSearchToolCallsFromClaudeMessages(&msg)
	require.Nil(t, got)
}
