package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

func newGenAITestTelemetry(t *testing.T) (*telemetrytest.Recorder, *telemetry.Telemetry) {
	t.Helper()
	rec := telemetrytest.New(t)
	return rec, &telemetry.Telemetry{Metrics: rec.Metrics}
}

func TestGenAICall_NilSafe(t *testing.T) {
	t.Parallel()
	var call *genAICall
	require.NotPanics(t, func() {
		call.beginAttempt()
		call.firstToken()
		call.retry(retryReasonRateLimited)
		call.blocked()
		call.recordUsage(genAIUsage{Input: 1})
		call.endChatCompletion(nil, nil)
		call.end(nil)
	})
	// A call without a recorder (nil telemetry, or LoggerOnly) records nothing.
	require.NotPanics(t, func() {
		c := startGenAICall(context.Background(), nil, telemetry.DependencyOpenAI, "m", genAIOpChat)
		c.recordUsage(genAIUsage{Input: 1})
		c.end(errors.New("boom"))
		startGenAICall(context.Background(), telemetry.LoggerOnly(nil), telemetry.DependencyOpenAI, "m", genAIOpChat).end(nil)
	})
}

func TestGenAICall_DurationTokensTTFTAndRetries(t *testing.T) {
	t.Parallel()
	rec, tel := newGenAITestTelemetry(t)
	ctx := telemetry.WithCallPath(context.Background(), telemetry.CallPathUserChat)

	call := startGenAICall(ctx, tel, telemetry.DependencyAnthropic, "claude-test", genAIOpChat)
	call.firstToken() // from an attempt that is then retried
	call.retry(retryReasonServerError)
	call.beginAttempt()
	call.firstToken()
	call.recordUsage(genAIUsage{Input: 100, Output: 20, CachedInput: 80, Reasoning: 0})
	call.end(nil)
	call.end(errors.New("ignored: already ended"))

	provider := telemetry.AttrGenAIProvider.String(telemetry.DependencyAnthropic)
	model := telemetry.AttrGenAIModel.String("claude-test")
	callPath := telemetry.AttrCallPath.String(string(telemetry.CallPathUserChat))

	require.Equal(t, uint64(1), rec.HistogramCount(t, telemetry.GenAIOperationDuration.Name,
		provider, model, callPath, telemetry.AttrGenAIOperation.String(genAIOpChat)))
	require.Empty(t, rec.AttributeValues(t, telemetry.GenAIOperationDuration.Name, telemetry.AttrErrorType))
	require.Equal(t, uint64(1), rec.HistogramCount(t, telemetry.GenAITimeToFirstToken.Name, provider, model, callPath),
		"TTFT is recorded once, for the successful attempt")
	require.Equal(t, int64(1), rec.CounterValue(t, telemetry.GenAIRetries.Name, provider, model,
		telemetry.AttrReason.String(retryReasonServerError)))

	for tokenType, want := range map[string]int64{
		telemetry.TokenTypeInput:       100,
		telemetry.TokenTypeOutput:      20,
		telemetry.TokenTypeCachedInput: 80,
	} {
		tt := telemetry.AttrGenAITokenType.String(tokenType)
		require.Equal(t, want, rec.CounterValue(t, telemetry.GenAITokens.Name, provider, model, tt, callPath), tokenType)
		require.InDelta(t, float64(want), rec.HistogramSum(t, telemetry.GenAITokenUsage.Name, provider, tt, callPath), 0, tokenType)
	}
	require.ElementsMatch(t, []string{"input", "output", "cached_input"},
		rec.AttributeValues(t, telemetry.GenAITokens.Name, telemetry.AttrGenAITokenType),
		"zero-count token types are not recorded")
	require.Empty(t, rec.AttributeValues(t, telemetry.GenAITokenUsage.Name, telemetry.AttrGenAIModel),
		"the bucketed token histogram carries no model label")
}

func TestGenAICall_ErrorTypeFromSDKStatus(t *testing.T) {
	t.Parallel()
	rec, tel := newGenAITestTelemetry(t)
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/v1/messages", nil)

	for _, tc := range []struct {
		model string
		err   error
		want  string
	}{
		{"model-a", &anthropic.Error{StatusCode: 529, Request: req, Response: &http.Response{StatusCode: 529}}, telemetry.ErrorTypeServer},
		{"model-b", &openai.Error{StatusCode: 429, Request: req, Response: &http.Response{StatusCode: 429}}, telemetry.ErrorTypeRateLimited},
		{"model-c", context.Canceled, telemetry.ErrorTypeCanceled},
	} {
		startGenAICall(context.Background(), tel, telemetry.DependencyOpenAI, tc.model, genAIOpChat).end(tc.err)
		require.Equal(t, uint64(1), rec.HistogramCount(t, telemetry.GenAIOperationDuration.Name,
			telemetry.AttrGenAIModel.String(tc.model), telemetry.AttrErrorType.String(tc.want)), tc.model)
	}
	require.Equal(t, uint64(0), rec.HistogramCount(t, telemetry.GenAITimeToFirstToken.Name), "no TTFT on failure")
}

func TestGenAICall_SafetyBlocks(t *testing.T) {
	t.Parallel()
	rec, tel := newGenAITestTelemetry(t)

	startGenAICall(context.Background(), tel, telemetry.DependencyOpenAI, "gpt-test", genAIOpChat).
		end(errors.New(`400 {"error":{"code":"moderation_blocked","message":"Your request was rejected by the safety system."}}`))
	c := startGenAICall(context.Background(), tel, telemetry.DependencyDeepSeek, "ds-test", genAIOpChat)
	c.endChatCompletion(&openai.ChatCompletion{Choices: []openai.ChatCompletionChoice{{FinishReason: "content_filter"}}}, nil)
	startGenAICall(context.Background(), tel, telemetry.DependencyOpenAI, "gpt-test", genAIOpChat).end(errors.New("500 internal server error"))

	require.Equal(t, int64(1), rec.CounterValue(t, telemetry.GenAISafetyBlocks.Name, telemetry.AttrGenAIModel.String("gpt-test")))
	require.Equal(t, int64(1), rec.CounterValue(t, telemetry.GenAISafetyBlocks.Name, telemetry.AttrGenAIModel.String("ds-test")))
}

func TestResponsesUsage_ReadsCachedAndReasoning(t *testing.T) {
	t.Parallel()
	u := responses.ResponseUsage{
		InputTokens:         1000,
		OutputTokens:        300,
		InputTokensDetails:  responses.ResponseUsageInputTokensDetails{CachedTokens: 900},
		OutputTokensDetails: responses.ResponseUsageOutputTokensDetails{ReasoningTokens: 250},
	}
	require.Equal(t, genAIUsage{Input: 1000, Output: 300, CachedInput: 900, Reasoning: 250}, responsesUsage(u))
}

// Input keeps the full prompt total (uncached + cache reads + cache writes) that billing and
// the Context X-ray rely on; cached_input is cache reads only.
func TestAnthropicUsage_KeepsTotalInputAndSplitsCacheReads(t *testing.T) {
	t.Parallel()
	u := anthropic.Usage{InputTokens: 10, CacheReadInputTokens: 500, CacheCreationInputTokens: 90, OutputTokens: 40}
	require.Equal(t, genAIUsage{Input: 600, Output: 40, CachedInput: 500}, anthropicUsage(u))
}

// The shared Chat Completions stream (all OpenAI-compatible providers) records duration, time
// to first token and token usage, including DeepSeek's prompt_cache_hit_tokens and
// completion_tokens_details.reasoning_tokens.
func TestChatCompletionsStream_RecordsMetrics(t *testing.T) {
	t.Parallel()
	rec, tel := newGenAITestTelemetry(t)
	frames := []string{
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-test","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"thinking"},"finish_reason":null}]}`,
		deltaFrame("Hello"),
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-test","choices":[],"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150,"prompt_cache_hit_tokens":100,"completion_tokens_details":{"reasoning_tokens":12}}}`,
	}
	srv := sseServer(t, frames)
	defer srv.Close()

	p := &DeepSeekProvider{client: newTestClient(srv.URL), tel: tel}
	ctx := telemetry.WithCallPath(t.Context(), telemetry.CallPathAgentJob)
	resp, err := p.CallStreaming(ctx, openai.ChatCompletionNewParams{Model: "deepseek-test"}, nil)
	require.NoError(t, err)
	require.Equal(t, int64(120), resp.Usage.PromptTokens, "returned usage is unchanged")

	provider := telemetry.AttrGenAIProvider.String(telemetry.DependencyDeepSeek)
	model := telemetry.AttrGenAIModel.String("deepseek-test")
	callPath := telemetry.AttrCallPath.String(string(telemetry.CallPathAgentJob))
	require.Equal(t, uint64(1), rec.HistogramCount(t, telemetry.GenAIOperationDuration.Name, provider, model, callPath))
	require.Equal(t, uint64(1), rec.HistogramCount(t, telemetry.GenAITimeToFirstToken.Name, provider, model, callPath))
	for tokenType, want := range map[string]int64{"input": 120, "output": 30, "cached_input": 100, "reasoning": 12} {
		require.Equal(t, want, rec.CounterValue(t, telemetry.GenAITokens.Name, provider, model,
			telemetry.AttrGenAITokenType.String(tokenType), callPath), tokenType)
	}
}

// OpenAI Responses calls record one duration per logical call through callWithRetry, with the
// status-based error.type and the model on the error series.
func TestOpenAICallWithRetry_RecordsDurationAndErrorType(t *testing.T) {
	t.Parallel()
	rec, tel := newGenAITestTelemetry(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad","type":"invalid_request_error","code":"bad"}}`))
	}))
	defer srv.Close()
	client := openai.NewClient(option.WithAPIKey("k"), option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	p := NewOpenAIProvider(nil, &client, nil, tel)

	_, err := p.CallWithRetry(t.Context(), responses.ResponseNewParams{Model: "gpt-test"})
	require.Error(t, err)
	require.Equal(t, uint64(1), rec.HistogramCount(t, telemetry.GenAIOperationDuration.Name,
		telemetry.AttrGenAIProvider.String(telemetry.DependencyOpenAI),
		telemetry.AttrGenAIModel.String("gpt-test"),
		telemetry.AttrCallPath.String(string(telemetry.CallPathUnknown)),
		telemetry.AttrErrorType.String(telemetry.ErrorTypeClient)))
}

func TestClaudeProviderName(t *testing.T) {
	t.Parallel()
	require.Equal(t, telemetry.DependencyAnthropic, NewClaudeProvider("k", nil, nil).providerName)
	require.Equal(t, telemetry.DependencyZAI, NewClaudeProviderWithBaseURL("k", DefaultZAIBaseURL, nil, nil).providerName)
}
