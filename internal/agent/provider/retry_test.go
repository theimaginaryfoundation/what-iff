package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

func withRetryAfter(err error, header, value string) error {
	res := &http.Response{Header: http.Header{}}
	res.Header.Set(header, value)
	var oaiErr *openai.Error
	if errors.As(err, &oaiErr) {
		oaiErr.Response = res
	}
	var antErr *anthropic.Error
	if errors.As(err, &antErr) {
		antErr.Response = res
	}
	return err
}

func TestRetryReason(t *testing.T) {
	t.Parallel()
	connRefused := &url.Error{Op: "Post", URL: "https://api.example", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}
	tests := []struct {
		name       string
		err        error
		wantReason string
	}{
		{"openai 429", &openai.Error{StatusCode: 429}, retryReasonRateLimited},
		{"openai 500", &openai.Error{StatusCode: 500}, retryReasonServerError},
		{"openai 502", &openai.Error{StatusCode: 502}, retryReasonServerError},
		{"openai 503", &openai.Error{StatusCode: 503}, retryReasonServerError},
		{"openai 504", fmt.Errorf("wrapped: %w", &openai.Error{StatusCode: 504}), retryReasonServerError},
		{"anthropic 529 overloaded", &anthropic.Error{StatusCode: 529}, retryReasonServerError},
		{"anthropic 429", &anthropic.Error{StatusCode: 429}, retryReasonRateLimited},
		{"stream failure with a status", withStreamStatus(errors.New("stream failed"), 500), retryReasonServerError},
		{"connection refused", connRefused, retryReasonNetwork},
		{"openai 400", &openai.Error{StatusCode: 400}, ""},
		{"anthropic 401", &anthropic.Error{StatusCode: 401}, ""},
		// Status-looking text no longer triggers a retry: only typed status codes do.
		{"message mentioning 500", errors.New("prompt asked for 500 words; rate limit of politeness"), ""},
		{"canceled", context.Canceled, ""},
		{"deadline", context.DeadlineExceeded, ""},
		{"canceled with usage", WrapCanceledWithUsage(context.Canceled, CancelUsage{}), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, ok := retryReason(tt.err)
			require.Equal(t, tt.wantReason != "", ok)
			require.Equal(t, tt.wantReason, reason)
		})
	}
}

func TestRetryWait(t *testing.T) {
	t.Parallel()
	require.Equal(t, 5*time.Second, retryWait(retryReasonServerError, 0, &openai.Error{StatusCode: 503}))
	require.Equal(t, 30*time.Second, retryWait(retryReasonNetwork, 1, errors.New("reset")))
	require.Equal(t, 100*time.Second, retryWait(retryReasonRateLimited, 1, &anthropic.Error{StatusCode: 429}))
	require.Equal(t, 100*time.Second, retryWait(retryReasonRateLimited, 7, nil), "past the schedule reuses its last wait")

	shorter := withRetryAfter(&openai.Error{StatusCode: 429}, "Retry-After-Ms", "1500")
	require.Equal(t, 1500*time.Millisecond, retryWait(retryReasonRateLimited, 0, shorter))
	seconds := withRetryAfter(&anthropic.Error{StatusCode: 429}, "Retry-After", "7")
	require.Equal(t, 7*time.Second, retryWait(retryReasonRateLimited, 0, seconds))
	// A Retry-After longer than the schedule is capped by it.
	longer := withRetryAfter(&anthropic.Error{StatusCode: 529}, "Retry-After", "600")
	require.Equal(t, 5*time.Second, retryWait(retryReasonServerError, 0, longer))
	garbage := withRetryAfter(&openai.Error{StatusCode: 503}, "Retry-After", "soon")
	require.Equal(t, 5*time.Second, retryWait(retryReasonServerError, 0, garbage))
}

// statusSequenceServer answers each request with the next status (the last one repeats),
// sending body for 200s and errBody otherwise. Retry-After-Ms keeps the test's waits short.
func statusSequenceServer(t *testing.T, statuses []int, contentType, body, errBody string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(n.Add(1)) - 1
		status := statuses[min(i, len(statuses)-1)]
		if status != http.StatusOK {
			w.Header().Set("Retry-After-Ms", "1")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(errBody))
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

const (
	openAIServerErrorBody     = `{"error":{"message":"upstream","type":"server_error","code":null}}`
	anthropicOverloadedBody   = `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`
	anthropicOverloadedStream = "event: error\ndata: " + anthropicOverloadedBody + "\n\n"
)

// The client is built like the production one (SDK default retries left on), so any SDK
// retry under the app loop would show up as extra requests.
func TestOpenAICallWithRetry_SingleRetryLayer(t *testing.T) {
	t.Parallel()
	srv, requests := statusSequenceServer(t, []int{http.StatusServiceUnavailable}, "application/json", "", openAIServerErrorBody)
	rec, tel := newGenAITestTelemetry(t)
	client := openai.NewClient(option.WithAPIKey("k"), option.WithBaseURL(srv.URL))
	p := NewOpenAIProvider(nil, &client, nil, tel)

	_, err := p.CallWithRetry(t.Context(), responses.ResponseNewParams{Model: "gpt-test"})
	require.Error(t, err)
	require.Equal(t, int32(llmMaxAttempts), requests.Load(), "one HTTP attempt per app-level attempt")

	provider := telemetry.AttrGenAIProvider.String(telemetry.DependencyOpenAI)
	model := telemetry.AttrGenAIModel.String("gpt-test")
	require.Equal(t, int64(llmMaxAttempts-1), rec.CounterValue(t, telemetry.GenAIRetries.Name, provider, model,
		telemetry.AttrReason.String(retryReasonServerError)))
	require.Equal(t, uint64(1), rec.HistogramCount(t, telemetry.GenAIOperationDuration.Name, provider, model,
		telemetry.AttrErrorType.String(telemetry.ErrorTypeServer)), "one duration per logical call")
}

func TestOpenAICallWithRetry_RecoversAfterRateLimit(t *testing.T) {
	t.Parallel()
	srv, requests := statusSequenceServer(t, []int{http.StatusTooManyRequests, http.StatusOK}, "application/json",
		responseTextJSON("resp_ok", "hi"), `{"error":{"message":"slow down","type":"requests","code":"rate_limit_exceeded"}}`)
	rec, tel := newGenAITestTelemetry(t)
	client := openai.NewClient(option.WithAPIKey("k"), option.WithBaseURL(srv.URL))
	p := NewOpenAIProvider(nil, &client, nil, tel)

	resp, err := p.CallWithRetry(t.Context(), responses.ResponseNewParams{Model: "gpt-test"})
	require.NoError(t, err)
	require.Equal(t, "resp_ok", resp.ID)
	require.Equal(t, int32(2), requests.Load())
	require.Equal(t, int64(1), rec.CounterValue(t, telemetry.GenAIRetries.Name,
		telemetry.AttrReason.String(retryReasonRateLimited)))
}

func TestClaudeStreaming_SingleRetryLayer(t *testing.T) {
	t.Parallel()
	srv, requests := statusSequenceServer(t, []int{529}, "text/event-stream", "", anthropicOverloadedBody)
	rec, tel := newGenAITestTelemetry(t)
	p := NewClaudeProviderWithBaseURL("k", srv.URL, tel, nil)

	_, err := p.CallWithRetryStreaming(t.Context(), claudeTestParams(), nil)
	require.Error(t, err)
	require.Equal(t, int32(llmMaxAttempts), requests.Load(), "SDK retries must be off under the app loop")
	require.Equal(t, int64(llmMaxAttempts-1), rec.CounterValue(t, telemetry.GenAIRetries.Name,
		telemetry.AttrReason.String(retryReasonServerError)))
}

func TestClaudeBetaStreaming_RecoversAfterOverload(t *testing.T) {
	t.Parallel()
	srv, requests := statusSequenceServer(t, []int{529, http.StatusOK}, "text/event-stream",
		claudeSSE("msg_ok", nil, []string{"hi"}, "end_turn"), anthropicOverloadedBody)
	p := NewClaudeProviderWithBaseURL("k", srv.URL, nil, nil)

	msg, err := p.CallBetaWithRetryStreaming(t.Context(), anthropic.BetaMessageNewParams{
		Model: "test-model", MaxTokens: 10,
		Messages: []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "msg_ok", msg.ID)
	require.Equal(t, int32(2), requests.Load())
}

// A mid-stream overloaded_error arrives inside a 200, so it has no status code of its own.
// It is classified like a 529 so the loop retries it. This goes through the real SDK decoder,
// pinning the error text classifyAnthropicStreamError parses.
func TestClaudeStreaming_RetriesOverloadedStreamErrorEvent(t *testing.T) {
	t.Parallel()
	srv, _ := statusSequenceServer(t, []int{http.StatusOK}, "text/event-stream", anthropicOverloadedStream, "")
	p := NewClaudeProviderWithBaseURL("k", srv.URL, nil, nil)

	_, streamed, err := p.messagesNewStreaming(t.Context(), claudeTestParams(), nil, nil, nil)
	require.Error(t, err)
	require.False(t, streamed)
	reason, ok := retryReason(err)
	require.True(t, ok, "overloaded stream error must be retryable: %v", err)
	require.Equal(t, retryReasonServerError, reason)
	require.Equal(t, telemetry.ErrorTypeServer, telemetry.ClassifyError(err))

	invalid := classifyAnthropicStreamError(errors.New(anthropicStreamErrorPrefix +
		`{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`))
	_, ok = retryReason(invalid)
	require.False(t, ok)
}

func TestResponsesNewStreaming_FailedEventCarriesStatus(t *testing.T) {
	t.Parallel()
	failed := func(code string) string {
		return `{"type":"response.failed","sequence_number":1,"response":{"id":"r","object":"response","created_at":1,` +
			`"model":"test","status":"failed","error":{"code":"` + code + `","message":"x"},"output":[]}}`
	}
	for code, want := range map[string]string{
		"server_error":        retryReasonServerError,
		"rate_limit_exceeded": retryReasonRateLimited,
		"invalid_prompt":      "",
	} {
		srv := sseServer(t, []string{failed(code)})
		_, _, err := newTestOpenAIProvider(srv.URL).responsesNewStreaming(t.Context(),
			responses.ResponseNewParams{Model: "test"}, nil, nil)
		srv.Close()
		require.Error(t, err)
		require.Contains(t, err.Error(), code, "message is unchanged")
		reason, _ := retryReason(err)
		require.Equal(t, want, reason, code)
	}
}

func claudeTestParams() anthropic.MessageNewParams {
	return anthropic.MessageNewParams{
		Model:     "test-model",
		MaxTokens: 10,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}
}
