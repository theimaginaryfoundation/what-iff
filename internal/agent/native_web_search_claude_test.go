package agent

import (
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestFormatClaudeWebSearchToolOutput_IncludesQueryAndCitations(t *testing.T) {
	t.Parallel()

	raw := `{
		"type":"message",
		"role":"assistant",
		"content":[
			{"type":"server_tool_use","id":"srv_01","name":"web_search","input":{"query":"weather NYC"},"caller":{"type":"direct"}},
			{"type":"web_search_tool_result","tool_use_id":"srv_01","content":[{"type":"web_search_result","title":"Weather","url":"https://example.com/w","encrypted_content":"enc","page_age":"1d"}]},
			{"type":"text","text":"It is sunny.","citations":[{"type":"web_search_result_location","url":"https://example.com/w","title":"Weather","cited_text":"sunny","encrypted_index":"idx"}]}
		]
	}`
	var msg anthropic.Message
	require.NoError(t, json.Unmarshal([]byte(raw), &msg))

	var ws anthropic.WebSearchToolResultBlock
	for _, block := range msg.Content {
		if v, ok := block.AsAny().(anthropic.WebSearchToolResultBlock); ok {
			ws = v
			break
		}
	}
	out := formatClaudeWebSearchToolOutput(&msg, ws)
	require.Contains(t, out, "Queries: weather NYC")
	require.Contains(t, out, "Results:")
	require.Contains(t, out, "https://example.com/w")
	require.Contains(t, out, "Citations:")
}

func TestFormatClaudeWebSearchToolOutput_OmitsEncryptedPlaceholderWhenCitationsPresent(t *testing.T) {
	t.Parallel()

	raw := `{
		"type":"message",
		"role":"assistant",
		"content":[
			{"type":"server_tool_use","id":"srv_01","name":"web_search","input":{"query":"Mariners score today"},"caller":{"type":"direct"}},
			{"type":"web_search_tool_result","tool_use_id":"srv_01","content":{"type":"web_search_result","encrypted_content":"enc"}},
			{"type":"text","text":"The Mariners won.","citations":[{"type":"web_search_result_location","url":"https://example.com/scores","title":"Scores","cited_text":"won","encrypted_index":"idx"}]}
		]
	}`
	var msg anthropic.Message
	require.NoError(t, json.Unmarshal([]byte(raw), &msg))

	var ws anthropic.WebSearchToolResultBlock
	for _, block := range msg.Content {
		if v, ok := block.AsAny().(anthropic.WebSearchToolResultBlock); ok {
			ws = v
			break
		}
	}
	out := formatClaudeWebSearchToolOutput(&msg, ws)
	require.Contains(t, out, "Queries: Mariners score today")
	require.Contains(t, out, "Citations:")
	require.Contains(t, out, "https://example.com/scores")
	require.NotContains(t, out, "encrypted by Anthropic")
}
