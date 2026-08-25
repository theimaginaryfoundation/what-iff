package provider

import (
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestClaudeWebSearchToolResult_ToParamPreservesEncryptedContent(t *testing.T) {
	t.Parallel()
	raw := `{"type":"web_search_tool_result","tool_use_id":"srv_01","content":[{"type":"web_search_result","title":"Example","url":"https://example.com","encrypted_content":"enc_abc","page_age":"1d"}]}`
	var block anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(raw), &block))

	ws, ok := block.AsAny().(anthropic.WebSearchToolResultBlock)
	require.True(t, ok)
	require.True(t, claudeWebSearchToolResultReplayable(ws))

	param := block.ToParam()
	require.NotNil(t, param.OfWebSearchToolResult)
	require.Len(t, param.OfWebSearchToolResult.Content.OfWebSearchToolResultBlockItem, 1)
	require.Equal(t, "enc_abc", param.OfWebSearchToolResult.Content.OfWebSearchToolResultBlockItem[0].EncryptedContent)

	marshaled, err := json.Marshal(param)
	require.NoError(t, err)
	require.Contains(t, string(marshaled), "enc_abc")
}

func TestClaudeWebSearchToolResult_EmptyContentNotReplayable(t *testing.T) {
	t.Parallel()
	raw := `{"type":"web_search_tool_result","tool_use_id":"srv_01","content":[]}`
	var block anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(raw), &block))
	ws, ok := block.AsAny().(anthropic.WebSearchToolResultBlock)
	require.True(t, ok)
	require.False(t, claudeWebSearchToolResultReplayable(ws))
}
