package provider

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
)

func TestNormalizeGeminiToolUses_EmptyIDUsesName(t *testing.T) {
	t.Parallel()
	got := normalizeGeminiToolUses([]ToolUse{
		{ID: "", Name: "generate_image", Input: []byte(`{"prompt":"cat"}`)},
	})
	require.Len(t, got, 1)
	require.Equal(t, "generate_image", got[0].ID)
	require.Equal(t, "generate_image", got[0].Name)
}

func TestNormalizeGeminiToolUses_PreservesNonEmptyID(t *testing.T) {
	t.Parallel()
	got := normalizeGeminiToolUses([]ToolUse{
		{ID: "call_abc", Name: "generate_image", Input: []byte(`{}`)},
	})
	require.Equal(t, "call_abc", got[0].ID)
}

func TestNormalizeGeminiToolUses_DeduplicatesEmptyIDsForSameTool(t *testing.T) {
	t.Parallel()
	got := normalizeGeminiToolUses([]ToolUse{
		{ID: "", Name: "generate_image", Input: []byte(`{"prompt":"a"}`)},
		{ID: "", Name: "generate_image", Input: []byte(`{"prompt":"b"}`)},
	})
	require.Equal(t, "generate_image", got[0].ID)
	require.Equal(t, "generate_image_2", got[1].ID)
}

func TestGeminiAssistantToolCallMessage_PlaceholderContentAndStableID(t *testing.T) {
	t.Parallel()
	raw := `{"id":"","type":"function","function":{"name":"generate_image","arguments":"{\"prompt\":\"a fox\"}"},"extra_content":{"google":{"thought_signature":"sig-abc"}}}`
	var tc openai.ChatCompletionMessageToolCallUnion
	require.NoError(t, json.Unmarshal([]byte(raw), &tc))

	msg := openai.ChatCompletionMessage{
		Role:      "assistant",
		ToolCalls: []openai.ChatCompletionMessageToolCallUnion{tc},
	}
	param := geminiAssistantToolCallMessage(msg)
	require.NotNil(t, param.OfAssistant)
	require.Equal(t, geminiToolCallContentPlaceholder, param.OfAssistant.Content.OfString.Value)
	require.Len(t, param.OfAssistant.ToolCalls, 1)

	out, err := param.MarshalJSON()
	require.NoError(t, err)
	body := string(out)
	require.Contains(t, body, `"id":"generate_image"`)
	require.Contains(t, body, "thought_signature")
	require.Contains(t, body, "sig-abc")
}

func TestGeminiFunctionTool_OmitsEmptyParameters(t *testing.T) {
	t.Parallel()
	tool := GeminiFunctionTool("list_modes", "list modes", map[string]interface{}{}, nil)
	require.NotNil(t, tool.OfFunction)
	_, hasProps := tool.OfFunction.Function.Parameters["properties"]
	require.False(t, hasProps)
	require.Equal(t, "object", tool.OfFunction.Function.Parameters["type"])
}

func TestOpenAIErrorDetail_GenericError(t *testing.T) {
	t.Parallel()
	require.Equal(t, "plain failure", openAIErrorDetail(fmt.Errorf("plain failure")))
}

func TestGeminiToolResultMessage_IncludesName(t *testing.T) {
	t.Parallel()
	msg := geminiToolResultMessage(ToolResult{
		ID:     "generate_image",
		Output: `{"image_url":"https://example.com/img.png"}`,
	}, "generate_image")
	require.NotNil(t, msg.OfTool)
	require.Equal(t, "generate_image", msg.OfTool.ToolCallID)

	raw, err := msg.MarshalJSON()
	require.NoError(t, err)
	require.Contains(t, string(raw), `"name":"generate_image"`)
	require.Contains(t, string(raw), `"tool_call_id":"generate_image"`)
}

func TestGeminiAdapter_toolNameForResult(t *testing.T) {
	t.Parallel()
	a := &GeminiAdapter{
		lastRequested: []ToolUse{
			{ID: "generate_image", Name: "generate_image"},
		},
	}
	require.Equal(t, "generate_image", a.toolNameForResult("generate_image", 0))
	require.Equal(t, "generate_image", a.toolNameForResult("wrong", 0))
}
