package provider

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalClaudeTextJSON(t *testing.T) {
	t.Parallel()

	payload := `{"memories":[{"content":"remember this","scope":"chat"}]}`
	quoted, err := json.Marshal(payload)
	require.NoError(t, err)
	var textBlock anthropic.ContentBlockUnion
	require.NoError(t, json.Unmarshal([]byte(`{"type":"text","text":`+string(quoted)+`}`), &textBlock))

	msg := &anthropic.Message{Content: []anthropic.ContentBlockUnion{textBlock}}

	var got struct {
		Memories []struct {
			Content string `json:"content"`
			Scope   string `json:"scope"`
		} `json:"memories"`
	}
	require.NoError(t, UnmarshalClaudeTextJSON(msg, &got))
	require.Len(t, got.Memories, 1)
	require.Equal(t, "remember this", got.Memories[0].Content)
	require.Equal(t, "chat", got.Memories[0].Scope)
}

func TestClaudeTotalInputTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		usage anthropic.Usage
		want  int64
	}{
		{
			name:  "no cache (uncached only)",
			usage: anthropic.Usage{InputTokens: 3000},
			want:  3000,
		},
		{
			name: "cache read dominates (the bug case)",
			usage: anthropic.Usage{
				InputTokens:          3000,
				CacheReadInputTokens: 18000,
			},
			want: 21000,
		},
		{
			name: "cache creation plus read plus uncached",
			usage: anthropic.Usage{
				InputTokens:              2500,
				CacheReadInputTokens:     15000,
				CacheCreationInputTokens: 5000,
			},
			want: 22500,
		},
		{
			name:  "all zero",
			usage: anthropic.Usage{},
			want:  0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, claudeTotalInputTokens(tc.usage))
		})
	}
}

func TestClaudeBetaTotalInputTokens(t *testing.T) {
	t.Parallel()

	usage := anthropic.BetaUsage{
		InputTokens:              2500,
		CacheReadInputTokens:     15000,
		CacheCreationInputTokens: 5000,
	}
	require.Equal(t, int64(22500), claudeBetaTotalInputTokens(usage))
	require.Equal(t, int64(0), claudeBetaTotalInputTokens(anthropic.BetaUsage{}))
}

// TestToGenerateResponse_FoldsCachedInputTokens guards the token-accounting regression where
// cached prefix tokens were excluded from the reported input size.
func TestAnthropicUsageInputTokens_OpenAICompatFallback(t *testing.T) {
	t.Parallel()

	var usage anthropic.Usage
	require.NoError(t, usage.UnmarshalJSON([]byte(`{"prompt_tokens":4200,"completion_tokens":900}`)))
	require.Equal(t, int64(4200), anthropicUsageInputTokens(usage))
	require.Equal(t, int64(900), anthropicUsageOutputTokens(usage))
}

func TestAnthropicBetaUsageInputTokens_OpenAICompatFallback(t *testing.T) {
	t.Parallel()

	var usage anthropic.BetaUsage
	require.NoError(t, usage.UnmarshalJSON([]byte(`{"prompt_tokens":3100,"completion_tokens":700}`)))
	require.Equal(t, int64(3100), anthropicBetaUsageInputTokens(usage))
	require.Equal(t, int64(700), anthropicBetaUsageOutputTokens(usage))
}

func TestAnthropicStreamUsage_PreservesOpenAICompatibleEventUsage(t *testing.T) {
	t.Parallel()

	var start anthropic.MessageStreamEventUnion
	require.NoError(t, json.Unmarshal([]byte(`{
		"type":"message_start",
		"message":{
			"id":"msg_glm",
			"type":"message",
			"role":"assistant",
			"model":"glm-5.2",
			"content":[],
			"stop_reason":null,
			"stop_sequence":null,
			"usage":{"prompt_tokens":4200}
		}
	}`), &start))

	var delta anthropic.MessageStreamEventUnion
	require.NoError(t, json.Unmarshal([]byte(`{
		"type":"message_delta",
		"delta":{"stop_reason":"end_turn","stop_sequence":null},
		"usage":{"completion_tokens":900}
	}`), &delta))

	var streamUsage anthropicStreamUsage
	streamUsage.observe(start.RawJSON())
	streamUsage.observe(delta.RawJSON())

	var usage anthropic.Usage
	streamUsage.apply(&usage)
	require.Equal(t, int64(4200), anthropicUsageInputTokens(usage))
	require.Equal(t, int64(900), anthropicUsageOutputTokens(usage))
}

func TestAnthropicStreamUsage_PreservesNativeCachedUsage(t *testing.T) {
	t.Parallel()

	usage := anthropic.Usage{
		InputTokens:          1200,
		CacheReadInputTokens: 800,
	}
	anthropicStreamUsage{inputTokens: 9999, outputTokens: 300}.apply(&usage)

	require.Equal(t, int64(2000), anthropicUsageInputTokens(usage))
	require.Equal(t, int64(300), anthropicUsageOutputTokens(usage))
}

func TestToGenerateResponse_FoldsCachedInputTokens(t *testing.T) {
	t.Parallel()

	c := &ClaudeProvider{}
	msg := &anthropic.Message{
		ID: "msg_1",
		Usage: anthropic.Usage{
			InputTokens:              3000,
			CacheReadInputTokens:     18000,
			CacheCreationInputTokens: 1000,
			OutputTokens:             500,
		},
	}
	got := c.ToGenerateResponse(msg)
	require.Equal(t, int64(22000), got.InputTokens)
	require.Equal(t, int64(500), got.OutputTokens)
}

func TestHandleClaudeTextDeltaEvent_EmitsTextDeltaOnly(t *testing.T) {
	t.Parallel()

	var textEvent anthropic.MessageStreamEventUnion
	require.NoError(t, json.Unmarshal([]byte(`{
		"type":"content_block_delta",
		"index":0,
		"delta":{"type":"text_delta","text":"Hi"}
	}`), &textEvent))

	var got []string
	emitted := handleClaudeTextDeltaEvent(textEvent, func(delta string) {
		got = append(got, delta)
	})
	require.True(t, emitted)
	require.Equal(t, []string{"Hi"}, got)

	var inputJSONEvent anthropic.MessageStreamEventUnion
	require.NoError(t, json.Unmarshal([]byte(`{
		"type":"content_block_delta",
		"index":0,
		"delta":{"type":"input_json_delta","partial_json":"{\"a\":1}"}
	}`), &inputJSONEvent))
	emitted = handleClaudeTextDeltaEvent(inputJSONEvent, func(delta string) {
		got = append(got, delta)
	})
	require.False(t, emitted)
	require.Equal(t, []string{"Hi"}, got)
}

func TestHandleClaudeBetaTextDeltaEvent_EmitsTextDeltaOnly(t *testing.T) {
	t.Parallel()

	var textEvent anthropic.BetaRawMessageStreamEventUnion
	require.NoError(t, json.Unmarshal([]byte(`{
		"type":"content_block_delta",
		"index":0,
		"delta":{"type":"text_delta","text":"Beta hi"}
	}`), &textEvent))

	var got []string
	emitted := handleClaudeBetaTextDeltaEvent(textEvent, func(delta string) {
		got = append(got, delta)
	})
	require.True(t, emitted)
	require.Equal(t, []string{"Beta hi"}, got)

	var inputJSONEvent anthropic.BetaRawMessageStreamEventUnion
	require.NoError(t, json.Unmarshal([]byte(`{
		"type":"content_block_delta",
		"index":0,
		"delta":{"type":"input_json_delta","partial_json":"{\"a\":1}"}
	}`), &inputJSONEvent))
	emitted = handleClaudeBetaTextDeltaEvent(inputJSONEvent, func(delta string) {
		got = append(got, delta)
	})
	require.False(t, emitted)
	require.Equal(t, []string{"Beta hi"}, got)
}

func TestCallClaudeWithRetry_StopsRetryAfterDeltaEmission(t *testing.T) {
	t.Parallel()

	attempts := 0
	_, err := callClaudeWithRetry(context.Background(), func(context.Context) (*string, bool, error) {
		attempts++
		return nil, true, errors.New("429 rate limit")
	})
	require.Error(t, err)
	require.Equal(t, 1, attempts, "must not retry once any delta has been emitted")
}
