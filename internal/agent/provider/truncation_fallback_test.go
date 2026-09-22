package provider

import (
	"context"
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
)

// Truncated-turn fallback for the always-on reasoning providers: z.ai GLM (Claude
// path; retried once at lower effort) and Xiaomi MiMo (Chat Completions; retried once
// with thinking disabled).

func claudeMessageThinkingTextJSON(id, thinking, text string) string {
	return `{"id":"` + id + `","type":"message","role":"assistant","model":"test-model",` +
		`"content":[{"type":"thinking","thinking":"` + thinking + `","signature":"sig"},` +
		`{"type":"text","text":"` + text + `"}],"stop_reason":"end_turn","stop_sequence":null,` +
		`"usage":{"input_tokens":5,"output_tokens":7}}`
}

// claudeMessageTruncatedThinkingJSON is a response whose whole output cap went to
// thinking: stop_reason max_tokens and no text block.
func claudeMessageTruncatedThinkingJSON(id string) string {
	return `{"id":"` + id + `","type":"message","role":"assistant","model":"test-model",` +
		`"content":[{"type":"thinking","thinking":"thinking forever","signature":"sig"}],` +
		`"stop_reason":"max_tokens","stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":100}}`
}

func TestClaudeAdapter_TruncationFallbackRetriesOnce(t *testing.T) {
	srv, requestBody := sequencedJSONServer(t, []string{
		claudeMessageTruncatedThinkingJSON("msg_trunc"),
		claudeMessageThinkingTextJSON("msg_ok", "short thought", "made it"),
	})
	defer srv.Close()

	a := newTestClaudeAdapter(srv.URL)
	fallbacks := 0
	a.SetTruncationFallback(func(p *anthropic.MessageNewParams) {
		fallbacks++
		ApplyZAIReasoningEffort(p, ZAIFallbackReasoningEffort)
	})

	resp, uses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, uses)
	require.Equal(t, 1, fallbacks)
	require.Equal(t, "made it", resp.Text)
	// The discarded truncated attempt is not recorded as a raw message.
	require.Len(t, a.AllRawMessages(), 1)

	require.NotContains(t, string(requestBody(0)), `"output_config"`)
	require.Contains(t, string(requestBody(1)), `"output_config":{"effort":"low"}`)
}

func TestClaudeAdapter_TruncationFallbackIsOneShot(t *testing.T) {
	srv, _ := sequencedJSONServer(t, []string{claudeMessageTruncatedThinkingJSON("msg_trunc")})
	defer srv.Close()

	a := newTestClaudeAdapter(srv.URL)
	fallbacks := 0
	a.SetTruncationFallback(func(*anthropic.MessageNewParams) { fallbacks++ })

	// Still truncated after the retry: surface it (the agent's empty-turn guard turns
	// max_tokens into the clear "cut off" error) rather than retrying again.
	resp, _, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Equal(t, "max_tokens", resp.StopReason)
	require.Empty(t, resp.Text)

	_, _, err = a.Call(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, fallbacks, "fallback applies at most once per turn")
}

func TestClaudeAdapter_NoFallbackWithoutTruncation(t *testing.T) {
	srv, _ := sequencedJSONServer(t, []string{claudeMessageTextJSON("msg_1", "fine")})
	defer srv.Close()

	a := newTestClaudeAdapter(srv.URL)
	a.SetTruncationFallback(func(*anthropic.MessageNewParams) { t.Fatal("fallback must not run on a normal response") })
	resp, _, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Equal(t, "fine", resp.Text)
}

func chatCompletionReasoningTextJSON(id, reasoning, text, finish string) string {
	return `{"id":"` + id + `","object":"chat.completion","created":1,"model":"test",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"` + text + `","reasoning_content":"` + reasoning + `"},"finish_reason":"` + finish + `"}],` +
		`"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`
}

func newTestXiaomiAdapter(baseURL string) *XiaomiAdapter {
	return NewXiaomiAdapter(NewXiaomiProvider("test-key", baseURL, nil, nil), openai.ChatCompletionNewParams{Model: "mimo-test"}, nil, nil)
}

func TestXiaomiAdapter_RaisesOutputCap(t *testing.T) {
	a := newTestXiaomiAdapter("http://unused.invalid")
	require.Equal(t, int64(ReasoningMaxOutputTokens), a.params.MaxCompletionTokens.Value)

	// The generic default every Chat Completions caller builds with is raised too...
	p := NewXiaomiProvider("k", "http://unused.invalid", nil, nil)
	a = NewXiaomiAdapter(p, openai.ChatCompletionNewParams{Model: "m", MaxCompletionTokens: openai.Int(DefaultMaxContentLength)}, nil, nil)
	require.Equal(t, int64(ReasoningMaxOutputTokens), a.params.MaxCompletionTokens.Value)

	// ...but a deliberately chosen cap is honored.
	a = NewXiaomiAdapter(p, openai.ChatCompletionNewParams{Model: "m", MaxCompletionTokens: openai.Int(512)}, nil, nil)
	require.Equal(t, int64(512), a.params.MaxCompletionTokens.Value)
}

func TestXiaomiAdapter_TruncationRetriesWithThinkingDisabled(t *testing.T) {
	srv, requestBody := sequencedJSONServer(t, []string{
		chatCompletionReasoningTextJSON("c_trunc", "thinking forever", "", "length"),
		chatCompletionReasoningTextJSON("c_ok", "", "answer without thinking", "stop"),
		chatCompletionTextJSON("c_next", "later round"),
	})
	defer srv.Close()

	a := newTestXiaomiAdapter(srv.URL)
	resp, _, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Equal(t, "answer without thinking", resp.Text)

	thinking := func(i int) any {
		var body map[string]any
		require.NoError(t, json.Unmarshal(requestBody(i), &body))
		return body["thinking"]
	}
	require.Nil(t, thinking(0))
	require.Equal(t, map[string]any{"type": "disabled"}, thinking(1))

	// Thinking stays off for the rest of the turn.
	_, _, err = a.Call(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]any{"type": "disabled"}, thinking(2))
}
