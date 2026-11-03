package provider

import (
	"encoding/json"
	"errors"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestApplyOpenAIReasoning(t *testing.T) {
	params := responses.ResponseNewParams{Model: "gpt-5.1", MaxOutputTokens: openai.Int(DefaultMaxContentLength)}
	ApplyOpenAIReasoning(&params, "")
	assert.Equal(t, shared.ReasoningParam{}, params.Reasoning, "no selection leaves the request as it was")
	assert.Equal(t, int64(DefaultMaxContentLength), params.MaxOutputTokens.Value)

	ApplyOpenAIReasoning(&params, models.ReasoningEffortHigh)
	assert.Equal(t, shared.ReasoningEffortHigh, params.Reasoning.Effort)
	assert.Equal(t, shared.ReasoningSummaryAuto, params.Reasoning.Summary)
	assert.Equal(t, int64(ReasoningMaxOutputTokens), params.MaxOutputTokens.Value)

	off := responses.ResponseNewParams{Model: "gpt-5.1"}
	ApplyOpenAIReasoning(&off, models.ReasoningEffortOff)
	assert.Equal(t, shared.ReasoningEffortNone, off.Reasoning.Effort)
	assert.Empty(t, off.Reasoning.Summary)

	old := responses.ResponseNewParams{Model: "gpt-5-mini"}
	ApplyOpenAIReasoning(&old, models.ReasoningEffortOff)
	assert.Equal(t, shared.ReasoningEffortMinimal, old.Reasoning.Effort, "the original GPT-5 models do not accept none")
}

func claudeParamsJSON(t *testing.T, p anthropic.MessageNewParams) map[string]any {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestApplyClaudeReasoning(t *testing.T) {
	none := anthropic.MessageNewParams{Model: "claude-opus-5", MaxTokens: DefaultMaxContentLength}
	assert.False(t, ApplyClaudeReasoning(&none, ""))
	_, hasThinking := claudeParamsJSON(t, none)["thinking"]
	assert.False(t, hasThinking, "no selection sends no thinking param")

	high := anthropic.MessageNewParams{Model: "claude-opus-5", MaxTokens: DefaultMaxContentLength}
	assert.True(t, ApplyClaudeReasoning(&high, models.ReasoningEffortHigh))
	j := claudeParamsJSON(t, high)
	assert.Equal(t, map[string]any{"type": "adaptive", "display": "summarized"}, j["thinking"])
	assert.Equal(t, "high", j["output_config"].(map[string]any)["effort"])
	assert.Equal(t, float64(ReasoningMaxOutputTokens), j["max_tokens"])

	sonnet46 := anthropic.MessageNewParams{Model: "claude-sonnet-4-6", MaxTokens: DefaultMaxContentLength}
	ApplyClaudeReasoning(&sonnet46, models.ReasoningEffortLow)
	assert.Equal(t, map[string]any{"type": "adaptive"}, claudeParamsJSON(t, sonnet46)["thinking"], "4.6 summarizes by default")

	haiku := anthropic.MessageNewParams{Model: "claude-haiku-4-5", MaxTokens: DefaultMaxContentLength}
	assert.True(t, ApplyClaudeReasoning(&haiku, models.ReasoningEffortMedium))
	j = claudeParamsJSON(t, haiku)
	assert.Equal(t, "enabled", j["thinking"].(map[string]any)["type"])
	assert.Less(t, j["thinking"].(map[string]any)["budget_tokens"].(float64), j["max_tokens"].(float64))
	_, hasOutputConfig := j["output_config"]
	assert.False(t, hasOutputConfig, "Haiku 4.5 rejects effort")

	sonnet45 := anthropic.MessageNewParams{Model: "claude-sonnet-4-5", MaxTokens: DefaultMaxContentLength}
	ApplyClaudeReasoning(&sonnet45, models.ReasoningEffortHigh)
	assert.Equal(t, "enabled", claudeParamsJSON(t, sonnet45)["thinking"].(map[string]any)["type"], "Sonnet 4.5 predates adaptive thinking")

	RaiseClaudeStreamingOutputCap(&high, models.ReasoningEffortHigh)
	assert.Equal(t, int64(ClaudeHighEffortMaxOutputTokens), high.MaxTokens)
	RaiseClaudeStreamingOutputCap(&haiku, models.ReasoningEffortMedium)
	assert.Equal(t, int64(ReasoningMaxOutputTokens), haiku.MaxTokens, "only high gets the larger cap")
	RaiseClaudeStreamingOutputCap(nil, models.ReasoningEffortHigh)

	off := anthropic.MessageNewParams{Model: "claude-opus-5", MaxTokens: DefaultMaxContentLength}
	assert.False(t, ApplyClaudeReasoning(&off, models.ReasoningEffortOff))
	assert.Equal(t, map[string]any{"type": "disabled"}, claudeParamsJSON(t, off)["thinking"])
}

func TestZAIReasoningEffortFor(t *testing.T) {
	assert.Equal(t, anthropic.OutputConfigEffort(ZAIReasoningEffort), ZAIReasoningEffortFor(""), "no selection keeps today's effort")
	assert.Equal(t, anthropic.OutputConfigEffort(ZAIReasoningEffort), ZAIReasoningEffortFor(models.ReasoningEffortMedium))
	assert.Equal(t, anthropic.OutputConfigEffortLow, ZAIReasoningEffortFor(models.ReasoningEffortLow))
	assert.Equal(t, anthropic.OutputConfigEffortMax, ZAIReasoningEffortFor(models.ReasoningEffortHigh))
}

func TestExtractOpenAIReasoningSummary(t *testing.T) {
	var resp responses.Response
	require.NoError(t, json.Unmarshal([]byte(`{"id":"r1","output":[
		{"type":"reasoning","id":"rs1","summary":[{"type":"summary_text","text":"First idea."},{"type":"summary_text","text":"Second idea."}]},
		{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Answer","annotations":[]}]}
	]}`), &resp))
	assert.Equal(t, "First idea.\n\nSecond idea.", ExtractOpenAIReasoningSummary(&resp))
	assert.Empty(t, ExtractOpenAIReasoningSummary(nil))
}

func TestOpenAIStreaming_ForwardsReasoningSummaryDeltas(t *testing.T) {
	srv := sseServer(t, []string{
		`{"type":"response.reasoning_summary_part.added","summary_index":0}`,
		`{"type":"response.reasoning_summary_text.delta","delta":"Thinking"}`,
		`{"type":"response.reasoning_summary_part.added","summary_index":1}`,
		`{"type":"response.reasoning_summary_text.delta","delta":"More"}`,
		`{"type":"response.output_text.delta","delta":"Hi"}`,
		`{"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`,
	})
	defer srv.Close()

	var reasoning, text string
	_, _, err := newTestOpenAIProvider(srv.URL).responsesNewStreaming(t.Context(),
		responses.ResponseNewParams{Model: "test"},
		func(d string) { text += d },
		func(d string) { reasoning += d },
		nil)
	require.NoError(t, err)
	assert.Equal(t, "Thinking\n\nMore", reasoning)
	assert.Equal(t, "Hi", text)
}

func chatParamsJSON(t *testing.T, p openai.ChatCompletionNewParams) map[string]any {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func chatParams(model string) openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{Model: model, MaxCompletionTokens: openai.Int(DefaultMaxContentLength)}
}

func TestApplyChatCompletionsReasoning(t *testing.T) {
	cases := []struct {
		name  string
		apply func(*openai.ChatCompletionNewParams, models.ReasoningEffort)
		model string
		level models.ReasoningEffort
		want  map[string]any
		raise bool
	}{
		{"gemini high", ApplyGeminiReasoning, "gemini-3.8-flash", models.ReasoningEffortHigh, map[string]any{"reasoning_effort": "high"}, true},
		{"gemini 2.5 flash off", ApplyGeminiReasoning, "gemini-2.5-flash", models.ReasoningEffortOff, map[string]any{"reasoning_effort": "none"}, false},
		{"qwen off", ApplyQwenReasoning, "qwen3.7-plus", models.ReasoningEffortOff, map[string]any{"enable_thinking": false}, false},
		{"qwen budget", ApplyQwenReasoning, "qwen3.7-plus", models.ReasoningEffortMedium, map[string]any{"enable_thinking": true, "thinking_budget": float64(6144)}, true},
		{"qwen 3.8 effort", ApplyQwenReasoning, "qwen3.8-max", models.ReasoningEffortHigh, map[string]any{"enable_thinking": true, "reasoning_effort": "xhigh"}, true},
		{"deepseek off", ApplyDeepSeekReasoning, "deepseek-flash", models.ReasoningEffortOff, map[string]any{"reasoning_effort": "none", "thinking": map[string]any{"type": "disabled"}}, false},
		{"deepseek medium", ApplyDeepSeekReasoning, "deepseek-flash", models.ReasoningEffortMedium, map[string]any{"reasoning_effort": "high"}, true},
		{"deepseek high", ApplyDeepSeekReasoning, "deepseek-flash", models.ReasoningEffortHigh, map[string]any{"reasoning_effort": "max"}, true},
		{"mistral on", ApplyMistralReasoning, "mistral-large-4", models.ReasoningEffortMedium, map[string]any{"reasoning_effort": "high"}, true},
		{"mistral off", ApplyMistralReasoning, "mistral-large-4", models.ReasoningEffortOff, map[string]any{"reasoning_effort": "none"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := chatParams(c.model)
			c.apply(&p, c.level)
			j := chatParamsJSON(t, p)
			for k, v := range c.want {
				assert.Equal(t, v, j[k], k)
			}
			if c.raise {
				assert.Equal(t, float64(ReasoningMaxOutputTokens), j["max_completion_tokens"])
			} else {
				assert.Equal(t, float64(DefaultMaxContentLength), j["max_completion_tokens"])
			}
		})
	}

	// No selection leaves every request untouched.
	for _, apply := range []func(*openai.ChatCompletionNewParams, models.ReasoningEffort){ApplyGeminiReasoning, ApplyQwenReasoning, ApplyDeepSeekReasoning, ApplyMistralReasoning} {
		p := chatParams("m")
		apply(&p, "")
		assert.Equal(t, chatParamsJSON(t, chatParams("m")), chatParamsJSON(t, p))
	}
}

// Extra fields from different settings accumulate instead of replacing each other.
func TestMergeChatExtraFields(t *testing.T) {
	p := chatParams("m")
	mergeChatExtraFields(&p, map[string]any{"a": 1})
	mergeChatExtraFields(&p, map[string]any{"b": 2})
	j := chatParamsJSON(t, p)
	assert.Equal(t, float64(1), j["a"])
	assert.Equal(t, float64(2), j["b"])
}

// Reasoning summaries need a verified OpenAI organization: OpenAI's refusal turns them off for the
// process rather than failing every turn.
func TestOpenAIReasoningSummaryFallback(t *testing.T) {
	defer openAIReasoningSummariesOff.Store(false)
	assert.True(t, isOpenAISummaryVerificationError(errors.New(`400 Bad Request: Your organization must be verified to generate reasoning summaries.`)))
	assert.False(t, isOpenAISummaryVerificationError(errors.New("rate limited")))

	assert.True(t, disableOpenAIReasoningSummaries())
	assert.False(t, disableOpenAIReasoningSummaries(), "only the first call turns them off")
	params := responses.ResponseNewParams{Model: "gpt-5.1"}
	ApplyOpenAIReasoning(&params, models.ReasoningEffortHigh)
	assert.Empty(t, params.Reasoning.Summary, "no summary is requested once they are off")
	assert.Equal(t, shared.ReasoningEffort("high"), params.Reasoning.Effort)
}
