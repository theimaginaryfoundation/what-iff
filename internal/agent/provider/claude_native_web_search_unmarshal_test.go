package provider

import (
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestWebSearchToolResultBlock_UnmarshalTypicalResult(t *testing.T) {
	t.Parallel()
	raw := `{"type":"web_search_tool_result","tool_use_id":"srv_01","content":[{"type":"web_search_result","title":"Example","url":"https://example.com","encrypted_content":"abc123","page_age":"1d"}]}`
	var block anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(raw), &block))
	ws, ok := block.AsAny().(anthropic.WebSearchToolResultBlock)
	require.True(t, ok)
	arr := ws.Content.AsWebSearchResultBlockArray()
	require.Len(t, arr, 1)
	require.Equal(t, "abc123", arr[0].EncryptedContent)
	out := FormatClaudeWebSearchResultBlock(ws)
	require.Contains(t, out, "https://example.com")
	require.NotContains(t, out, "not available for replay")
}

func TestWebSearchToolResultBlock_UnmarshalError(t *testing.T) {
	t.Parallel()
	raw := `{"type":"web_search_tool_result","tool_use_id":"srv_01","content":{"type":"web_search_tool_result_error","error_code":"unavailable"}}`
	var block anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(raw), &block))
	ws, ok := block.AsAny().(anthropic.WebSearchToolResultBlock)
	require.True(t, ok)
	require.Empty(t, ws.Content.AsWebSearchResultBlockArray())
	out := FormatClaudeWebSearchResultBlock(ws)
	require.Contains(t, out, "unavailable")
}

func TestWebSearchToolResultBlock_UnmarshalSingleObjectContent(t *testing.T) {
	t.Parallel()
	raw := `{"type":"web_search_tool_result","tool_use_id":"srv_01","content":{"type":"web_search_result","title":"Example","url":"https://example.com","encrypted_content":"abc123","page_age":"1d"}}`
	var block anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(raw), &block))
	ws, ok := block.AsAny().(anthropic.WebSearchToolResultBlock)
	require.True(t, ok)
	require.Empty(t, ws.Content.AsWebSearchResultBlockArray())
	results := claudeWebSearchResultsFromContent(ws.Content)
	require.Len(t, results, 1)
	require.Equal(t, "https://example.com", results[0].URL)
	out := FormatClaudeWebSearchResultBlock(ws)
	require.Contains(t, out, "https://example.com")
	require.NotContains(t, out, "encrypted by Anthropic")
}

func TestFormatClaudeWebSearchResults_EncryptedOnly(t *testing.T) {
	t.Parallel()
	out := FormatClaudeWebSearchResults([]anthropic.WebSearchResultBlock{
		{EncryptedContent: "opaque"},
		{EncryptedContent: "opaque2"},
	})
	require.Contains(t, out, "2 results")
	require.Contains(t, out, "encrypted by Anthropic")
}
