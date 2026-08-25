package provider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
)

// retryingClient is newTestClient with retries switched on, matching how the
// real providers build their clients (option.WithMaxRetries(2)).
func retryingClient(baseURL string) *openai.Client {
	c := openai.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(baseURL),
		option.WithMaxRetries(2),
	)
	return &c
}

func deltaFrame(content string) string {
	return `{"id":"cmpl-1","object":"chat.completion.chunk","created":1,"model":"test",` +
		`"choices":[{"index":0,"delta":{"content":"` + content + `"},"finish_reason":null}]}`
}

// sseServer returns a test server that streams the given SSE data frames as a
// Chat Completions stream, followed by the terminating [DONE] sentinel.
func sseServer(t *testing.T, frames []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok, "test server response writer must support flushing")
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
}

func newTestClient(baseURL string) *openai.Client {
	c := openai.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(baseURL),
	)
	return &c
}

func TestStreamChatCompletion_ForwardsTextDeltasAndAccumulates(t *testing.T) {
	t.Parallel()
	frames := []string{
		`{"id":"cmpl-1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
		`{"id":"cmpl-1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":", world"},"finish_reason":null}]}`,
		`{"id":"cmpl-1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"id":"cmpl-1","object":"chat.completion.chunk","created":1,"model":"test","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":34,"total_tokens":46}}`,
	}
	srv := sseServer(t, frames)
	defer srv.Close()

	var deltas []string
	resp, err := streamChatCompletion(
		t.Context(),
		newTestClient(srv.URL),
		openai.ChatCompletionNewParams{Model: "test"},
		func(d string) { deltas = append(deltas, d) },
	)
	require.NoError(t, err)
	require.Equal(t, []string{"Hello", ", world"}, deltas)
	require.NotNil(t, resp)
	require.Equal(t, "Hello, world", ExtractChatCompletionText(resp))
	inputTokens, outputTokens := chatCompletionTokenUsage(resp)
	require.Equal(t, int64(12), inputTokens)
	require.Equal(t, int64(34), outputTokens)
}

func TestStreamChatCompletion_AccumulatesToolCallsWithoutDeltas(t *testing.T) {
	t.Parallel()
	frames := []string{
		`{"id":"cmpl-2","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"do_thing","arguments":"{\"x\":1}"}}]},"finish_reason":null}]}`,
		`{"id":"cmpl-2","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	srv := sseServer(t, frames)
	defer srv.Close()

	resp, err := streamChatCompletion(
		t.Context(),
		newTestClient(srv.URL),
		openai.ChatCompletionNewParams{Model: "test"},
		func(d string) { t.Fatalf("unexpected text delta %q on tool-call turn", d) },
	)
	require.NoError(t, err)

	toolUses := extractChatCompletionToolUses(resp)
	require.Len(t, toolUses, 1)
	require.Equal(t, "do_thing", toolUses[0].Name)
	require.Equal(t, "call_1", toolUses[0].ID)
	require.Equal(t, `{"x":1}`, string(toolUses[0].Input))
}

func TestStreamChatCompletion_NilHandlerStillAccumulates(t *testing.T) {
	t.Parallel()
	frames := []string{
		`{"id":"cmpl-3","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
	}
	srv := sseServer(t, frames)
	defer srv.Close()

	resp, err := streamChatCompletion(
		t.Context(),
		newTestClient(srv.URL),
		openai.ChatCompletionNewParams{Model: "test"},
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, "ok", ExtractChatCompletionText(resp))
}

func TestChatCompletionTokenUsage_MissingUsageReturnsZero(t *testing.T) {
	t.Parallel()
	in, out := chatCompletionTokenUsage(&openai.ChatCompletion{})
	require.Zero(t, in)
	require.Zero(t, out)

	in, out = chatCompletionTokenUsage(nil)
	require.Zero(t, in)
	require.Zero(t, out)
}

func TestStreamChatCompletion_EmptyStreamErrors(t *testing.T) {
	t.Parallel()
	srv := sseServer(t, nil)
	defer srv.Close()

	_, err := streamChatCompletion(
		t.Context(),
		newTestClient(srv.URL),
		openai.ChatCompletionNewParams{Model: "test"},
		nil,
	)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "no chunks"))
}

// The invariant documented on streamChatCompletion: openai-go decides retries
// from the response status line and headers, before any SSE body is read, so a
// stream that has already delivered chunks can never be re-issued. That is why
// this path carries no "delta already emitted" guard while the Claude and OpenAI
// Responses paths — which wrap their calls in application-level retry loops — do.
//
// If an SDK upgrade ever made retries reach into a started stream, a retry would
// re-send text the user had already seen. This test fails if that happens.
func TestStreamChatCompletion_DoesNotRetryAfterDeltasDelivered(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok, "test server response writer must support flushing")
		for _, content := range []string{"Hel", "lo"} {
			_, _ = w.Write([]byte("data: " + deltaFrame(content) + "\n\n"))
			flusher.Flush()
		}
		// Abort mid-stream, after real content and with no [DONE]. A transport
		// failure is the only retryable-looking error reachable at this point,
		// since the 200 status was already sent before the first chunk.
		panic(http.ErrAbortHandler)
	}))
	defer srv.Close()

	var deltas []string
	_, err := streamChatCompletion(
		t.Context(),
		retryingClient(srv.URL),
		openai.ChatCompletionNewParams{Model: "test"},
		func(d string) { deltas = append(deltas, d) },
	)

	require.Error(t, err, "an aborted stream must surface as an error, not a silent truncation")
	require.Equal(t, []string{"Hel", "lo"}, deltas, "content was delivered before the abort")
	require.EqualValues(t, 1, requests.Load(),
		"WithMaxRetries(2) must not re-issue a stream that already delivered chunks")
}

// Control for the test above. Without this, a request count of 1 there could
// mean the invariant holds or merely that retries never fire in this harness.
// Here the failure precedes the body, so the SDK does retry — proving the
// counter can observe a retry when one happens.
func TestStreamChatCompletion_DoesRetryWhenFailurePrecedesTheBody(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			// 500 is retryable and arrives before any SSE body.
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"transient"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok, "test server response writer must support flushing")
		_, _ = w.Write([]byte("data: " + deltaFrame("recovered") + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	var deltas []string
	resp, err := streamChatCompletion(
		t.Context(),
		retryingClient(srv.URL),
		openai.ChatCompletionNewParams{Model: "test"},
		func(d string) { deltas = append(deltas, d) },
	)

	require.NoError(t, err)
	require.Equal(t, []string{"recovered"}, deltas)
	require.NotNil(t, resp)
	require.EqualValues(t, 2, requests.Load(),
		"a pre-body 500 must be retried, or the sibling test proves nothing")
}
