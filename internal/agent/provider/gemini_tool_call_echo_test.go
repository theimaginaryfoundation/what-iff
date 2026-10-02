package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestStripGeminiToolCallEcho(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"sentinel only", "[tool call]", ""},
		{"repeated sentinels", "[tool call][tool call]\n[tool call]", ""},
		{"sentinel then answer", "[tool call]\n\nHere is your image.", "Here is your image."},
		{"leading whitespace", "  \n[tool call] Done.", "Done."},
		{"no sentinel", "Plain answer.", "Plain answer."},
		{"mid-text mention kept", "The literal [tool call] marker is internal.", "The literal [tool call] marker is internal."},
		{"trailing mention kept", "Done. [tool call]", "Done. [tool call]"},
		{"partial prefix kept", "[tool", "[tool"},
		{"different bracket text kept", "[tool calls are fun]", "[tool calls are fun]"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, stripGeminiToolCallEcho(tc.in))
		})
	}
}

func TestGeminiToolCallEchoFilter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		deltas []string
		want   string
	}{
		{"sentinel only in one delta", []string{"[tool call]"}, ""},
		{"sentinel split across deltas", []string{"[to", "ol ca", "ll]"}, ""},
		{"sentinel split then answer", []string{"[tool", " call]", "Here ", "it is."}, "Here it is."},
		{"sentinel and answer in one delta", []string{"[tool call]\n\nAnswer"}, "\n\nAnswer"},
		{"repeated sentinels", []string{"[tool call]", "[tool call]", "ok"}, "ok"},
		{"no sentinel passes through", []string{"Hel", "lo"}, "Hello"},
		{"mid-text mention kept", []string{"See ", "[tool call]", " below"}, "See [tool call] below"},
		{"partial prefix flushed at end", []string{"[tool"}, "[tool"},
		{"prefix that diverges", []string{"[to", "day]"}, "[today]"},
		{"whitespace only flushed", []string{"\n"}, "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got strings.Builder
			f := newGeminiToolCallEchoFilter(func(d string) { got.WriteString(d) })
			for _, d := range tc.deltas {
				f.HandleDelta(d)
			}
			f.Flush()
			require.Equal(t, tc.want, got.String())
		})
	}
}

func TestGeminiMessagesCarryToolCallPlaceholder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		msgs []openai.ChatCompletionMessageParamUnion
		want bool
	}{
		{"none", nil, false},
		{"user-authored sentinel does not arm", []openai.ChatCompletionMessageParamUnion{openai.UserMessage("what does [tool call] mean?")}, false},
		{"assistant text without sentinel", []openai.ChatCompletionMessageParamUnion{openai.AssistantMessage("hi")}, false},
		{"assistant placeholder arms", []openai.ChatCompletionMessageParamUnion{openai.AssistantMessage(geminiToolCallContentPlaceholder)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, geminiMessagesCarryToolCallPlaceholder(tc.msgs))
		})
	}
}

// sequencedSSEServer streams frames[N] (clamped to the last entry) for request N,
// recording each request body.
func sequencedSSEServer(t *testing.T, frames [][]string) (*httptest.Server, func(i int) string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		idx := len(seen)
		seen = append(seen, string(b))
		mu.Unlock()
		if idx >= len(frames) {
			idx = len(frames) - 1
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		for _, f := range frames[idx] {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	return srv, func(i int) string {
		mu.Lock()
		defer mu.Unlock()
		return seen[i]
	}
}

const geminiStreamToolCallFrame = `{"id":"g1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"do_thing","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`

func geminiStreamTextFrame(content, finish string) string {
	fr := "null"
	if finish != "" {
		fr = `"` + finish + `"`
	}
	return `{"id":"g2","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"` + content + `"},"finish_reason":` + fr + `}]}`
}

// TestGeminiAdapter_StreamedEchoOfToolCallPlaceholderIsHidden reproduces issue
// #142: after an empty-content tool-call round, the adapter replays that turn to
// Gemini with the internal "[tool call]" placeholder; the model imitates it and
// streams it back as text. The echo must not reach the delta stream (the draft
// UI and, via concatenated deltas, the persisted message) nor the final Text.
func TestGeminiAdapter_StreamedEchoOfToolCallPlaceholderIsHidden(t *testing.T) {
	srv, requestBody := sequencedSSEServer(t, [][]string{
		{geminiStreamToolCallFrame},
		{geminiStreamTextFrame("[tool", ""), geminiStreamTextFrame(" call]", ""), geminiStreamTextFrame("Here it is.", "stop")},
	})
	defer srv.Close()

	a := newTestGeminiAdapter(srv.URL)
	var streamed strings.Builder
	a.SetTextDeltaHandler(func(d string) { streamed.WriteString(d) })

	resp, uses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Len(t, uses, 1)
	a.AppendToolResults([]ToolResult{{ID: uses[0].ID, Output: "done"}})

	resp, uses, err = a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, uses)
	// The placeholder is still sent to Gemini (it rejects empty content)...
	require.Contains(t, requestBody(1), geminiToolCallContentPlaceholder)
	// ...but never surfaces as assistant-visible text.
	require.Equal(t, "Here it is.", streamed.String())
	require.Equal(t, "Here it is.", resp.Text)
}

// TestGeminiAdapter_EchoOnlyFinalTextIsEmpty covers the non-streamed path and the
// fallback runGeneration uses when streamed text is empty: a response that is only
// the echoed placeholder yields empty Text rather than the sentinel.
func TestGeminiAdapter_EchoOnlyFinalTextIsEmpty(t *testing.T) {
	srv, _ := sequencedJSONServer(t, []string{
		chatCompletionToolCallJSON("cmpl-1", "call_1", "do_thing", `{}`),
		chatCompletionTextJSON("cmpl-2", "[tool call]"),
	})
	defer srv.Close()

	a := newTestGeminiAdapter(srv.URL)
	_, uses, err := a.Call(context.Background())
	require.NoError(t, err)
	a.AppendToolResults([]ToolResult{{ID: uses[0].ID, Output: "done"}})

	resp, _, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, resp.Text)
}

// TestGeminiAdapter_UserAuthoredToolCallTextPreserved: when the placeholder was
// never shown to the model as assistant output (here only the user typed it), the
// filter stays disarmed and a reply that legitimately starts with the same string
// is streamed and returned intact.
func TestGeminiAdapter_UserAuthoredToolCallTextPreserved(t *testing.T) {
	srv, requestBody := sequencedSSEServer(t, [][]string{
		{geminiStreamTextFrame("[tool call] is just text.", "stop")},
	})
	defer srv.Close()

	a := NewGeminiAdapter(NewGeminiProvider("test-key", srv.URL, nil, nil), openai.ChatCompletionNewParams{
		Model:    "test",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("Say [tool call] back to me")},
	}, nil, nil, zap.NewNop())
	var streamed strings.Builder
	a.SetTextDeltaHandler(func(d string) { streamed.WriteString(d) })

	resp, _, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Contains(t, requestBody(0), "Say [tool call] back to me")
	require.Equal(t, "[tool call] is just text.", streamed.String())
	require.Equal(t, "[tool call] is just text.", resp.Text)
}
