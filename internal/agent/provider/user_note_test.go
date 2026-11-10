package provider

import (
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every built-in adapter takes a follow-up as a user turn after the tool results (ADR 0x042).
var (
	_ UserNoteAppender = (*ClaudeAdapter)(nil)
	_ UserNoteAppender = (*OpenAIAdapter)(nil)
	_ UserNoteAppender = (*GeminiAdapter)(nil)
	_ UserNoteAppender = (*XiaomiAdapter)(nil)
	_ UserNoteAppender = (*LocalAdapter)(nil)
	_ UserNoteAppender = (*DeepSeekAdapter)(nil)
	_ UserNoteAppender = (*MistralAdapter)(nil)
	_ UserNoteAppender = (*QwenAdapter)(nil)
	_ UserNoteAppender = (*ConnectionChatAdapter)(nil)
	_ UserNoteAppender = (*MockAdapter)(nil)
)

func TestClaudeAdapter_UserNoteFollowsTheToolResults(t *testing.T) {
	a := NewClaudeAdapter(nil, anthropic.MessageNewParams{Model: "claude-opus-5"}, nil, false, nil, nil)
	a.AppendToolResults([]ToolResult{{ID: "toolu_1", Output: "42"}})
	a.AppendUserNote("[Sent mid-run] use metric")
	msgs := a.params.Messages
	require.Len(t, msgs, 2)
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[0].Role)
	require.NotNil(t, msgs[0].Content[0].OfToolResult)
	assert.Equal(t, anthropic.MessageParamRoleUser, msgs[1].Role)
	require.NotNil(t, msgs[1].Content[0].OfText)
	assert.Equal(t, "[Sent mid-run] use metric", msgs[1].Content[0].OfText.Text)
}

func TestOpenAIAdapter_UserNoteFollowsTheFunctionOutputs(t *testing.T) {
	a := NewOpenAIAdapter(nil, responses.ResponseNewParams{Model: "gpt-5.1"})
	a.AppendToolResults([]ToolResult{{ID: "call_1", Output: "42"}})
	a.AppendUserNote("use metric")
	items := a.params.Input.OfInputItemList
	require.Len(t, items, 2)
	require.NotNil(t, items[0].OfFunctionCallOutput)
	raw, err := json.Marshal(items[1])
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"role":"user"`)
	assert.Contains(t, string(raw), "use metric")
}

func TestChatCompletionsAdapters_UserNoteFollowsTheToolMessages(t *testing.T) {
	a := NewDeepSeekAdapter(NewDeepSeekProvider("k", "http://127.0.0.1:1", nil, nil), openai.ChatCompletionNewParams{Model: "deepseek-flash"}, nil, nil)
	a.AppendToolResults([]ToolResult{{ID: "call_1", Output: "42"}})
	a.AppendUserNote("use metric")
	msgs := a.params.Messages
	require.Len(t, msgs, 2)
	require.NotNil(t, msgs[0].OfTool)
	require.NotNil(t, msgs[1].OfUser)
	assert.Equal(t, "use metric", msgs[1].OfUser.Content.OfString.Value)
}
