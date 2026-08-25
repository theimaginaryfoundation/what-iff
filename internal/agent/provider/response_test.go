package provider

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
)

// mustParseResponse builds a *responses.Response from JSON for OpenAIToGenerateResponse tests.
func mustParseResponse(t *testing.T, raw string) *responses.Response {
	t.Helper()
	var r responses.Response
	require.NoError(t, json.Unmarshal([]byte(raw), &r))
	return &r
}

func TestOpenAIToGenerateResponse_Nil(t *testing.T) {
	t.Parallel()
	got := OpenAIToGenerateResponse(nil)
	require.NotNil(t, got)
	require.Empty(t, got.ID)
	require.Empty(t, got.Text)
	require.Zero(t, got.InputTokens)
	require.Zero(t, got.OutputTokens)
}

func TestOpenAIToGenerateResponse_MapsIDUsageCreatedAtAndText(t *testing.T) {
	t.Parallel()
	raw := `{
		"id": "resp_test_1",
		"created_at": 1700000000,
		"usage": {
			"input_tokens": 11,
			"output_tokens": 22,
			"total_tokens": 33,
			"input_tokens_details": {"cached_tokens": 0},
			"output_tokens_details": {}
		},
		"output": [
			{
				"type": "message",
				"id": "msg_1",
				"role": "assistant",
				"status": "completed",
				"content": [
					{"type": "output_text", "text": "Short reply."}
				]
			}
		]
	}`
	resp := mustParseResponse(t, raw)
	got := OpenAIToGenerateResponse(resp)
	require.Equal(t, "resp_test_1", got.ID)
	require.Equal(t, int64(11), got.InputTokens)
	require.Equal(t, int64(22), got.OutputTokens)
	require.Equal(t, int64(1700000000), got.CreatedAt)
	require.Equal(t, "Short reply.", got.Text)
}

func TestClaudeGenerateResponse_FieldsAndRecentCreatedAt(t *testing.T) {
	t.Parallel()
	before := time.Now().Unix()
	got := ClaudeGenerateResponse("msg_claude", 100, 200, "hello from claude")
	after := time.Now().Unix()
	require.Equal(t, "msg_claude", got.ID)
	require.Equal(t, int64(100), got.InputTokens)
	require.Equal(t, int64(200), got.OutputTokens)
	require.Equal(t, "hello from claude", got.Text)
	require.GreaterOrEqual(t, got.CreatedAt, before)
	require.LessOrEqual(t, got.CreatedAt, after)
}
