package provider

import (
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGeminiAdapter_AppendToolResultsAddsToolName(t *testing.T) {
	t.Parallel()
	adapter := NewGeminiAdapter(NewGeminiProvider("test-key", "", nil, nil), openai.ChatCompletionNewParams{}, nil, nil, zap.NewNop())
	adapter.lastRequested = []ToolUse{{ID: "call1", Name: "generate_image"}}
	adapter.AppendToolResults([]ToolResult{{ID: "call1", Output: `{"image_url":"https://example.com/a.png"}`}})

	require.Len(t, adapter.params.Messages, 1)
	raw, err := adapter.params.Messages[0].MarshalJSON()
	require.NoError(t, err)
	body := string(raw)
	require.Contains(t, body, `"tool_call_id":"call1"`)
	require.Contains(t, body, `"name":"generate_image"`)
}
