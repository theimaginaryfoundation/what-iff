package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// recordingSSEServer serves bodies[i] (raw SSE) to the i-th request and records each
// request body, so a test can inspect what the adapter sent on the next tool round.
func recordingSSEServer(t *testing.T, bodies []string) (*httptest.Server, func(i int) string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		idx := len(seen)
		seen = append(seen, string(b))
		mu.Unlock()
		if idx >= len(bodies) {
			idx = len(bodies) - 1
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(bodies[idx]))
	}))
	return srv, func(i int) string {
		mu.Lock()
		defer mu.Unlock()
		require.Greater(t, len(seen), i, "request %d was never made", i)
		return seen[i]
	}
}

func streamedToolRoundSSE(reasoning, arguments string) string {
	frame := func(delta string, finish string) string {
		return `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":` + delta + `,"finish_reason":` + finish + `}]}` + "\n\n"
	}
	var out string
	if reasoning != "" {
		out += frame(`{"reasoning_content":"`+reasoning+`"}`, "null")
	}
	out += frame(`{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"`+arguments+`"}}]}`, "null")
	out += frame(`{}`, `"tool_calls"`)
	return out + "data: [DONE]\n\n"
}

// MiMo 400s when an assistant tool-call message is replayed without its
// reasoning_content while thinking is enabled; the stream accumulator drops the
// field, so the adapter must re-attach what it captured for that call.
func TestXiaomiAdapter_ReplaysReasoningContentOnStreamedToolCall(t *testing.T) {
	srv, request := recordingSSEServer(t, []string{
		streamedToolRoundSSE("need a tool", `{\"q\":1}`),
		chatCompletionSSE(nil, []string{"done"}, "stop"),
	})
	defer srv.Close()

	a := newTestXiaomiAdapter(srv.URL)
	a.SetTextDeltaHandler(func(string) {})
	_, uses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Len(t, uses, 1)
	a.AppendToolResults([]ToolResult{{ID: "call_1", Output: "x"}})
	resp, _, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Equal(t, "done", resp.Text)

	assistant := gjson.Get(request(1), `messages.#(role=="assistant")`)
	require.True(t, assistant.Exists(), request(1))
	require.Equal(t, "need a tool", assistant.Get("reasoning_content").String())
	content := assistant.Get("content")
	require.True(t, content.Exists(), "a text-less tool-call turn must still carry a content field: %s", request(1))
	require.Equal(t, "", content.String())
	require.Equal(t, "call_1", assistant.Get("tool_calls.0.id").String())
	require.Equal(t, "lookup", assistant.Get("tool_calls.0.function.name").String())
	require.Equal(t, `{"q":1}`, assistant.Get("tool_calls.0.function.arguments").String())
}

func TestXiaomiAdapter_NoReasoningContentWhenNoneStreamed(t *testing.T) {
	srv, request := recordingSSEServer(t, []string{
		streamedToolRoundSSE("", `{}`),
		chatCompletionSSE(nil, []string{"done"}, "stop"),
	})
	defer srv.Close()

	a := newTestXiaomiAdapter(srv.URL)
	a.SetTextDeltaHandler(func(string) {})
	_, _, err := a.Call(context.Background())
	require.NoError(t, err)
	a.AppendToolResults([]ToolResult{{ID: "call_1", Output: "x"}})
	_, _, err = a.Call(context.Background())
	require.NoError(t, err)
	require.False(t, gjson.Get(request(1), `messages.#(role=="assistant").reasoning_content`).Exists())
}

// A no-argument call can arrive with "" as its arguments; tools decode Input as a
// JSON object, so it is normalised to "{}" rather than failing with
// "unexpected end of JSON input".
func TestExtractChatCompletionToolUses_EmptyArgumentsBecomeEmptyObject(t *testing.T) {
	srv, _ := recordingSSEServer(t, []string{streamedToolRoundSSE("", "")})
	defer srv.Close()

	a := newTestXiaomiAdapter(srv.URL)
	a.SetTextDeltaHandler(func(string) {})
	_, uses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Len(t, uses, 1)
	require.JSONEq(t, `{}`, string(uses[0].Input))
}

// ContinueWith records the final reply as plain assistant text (with MiMo's reasoning echoed)
// before the next user turn.
func TestChatCompletionsFinal_ContinueWithAppendsReplyThenUser(t *testing.T) {
	var resp openai.ChatCompletion
	if err := json.Unmarshal([]byte(`{"id":"c1","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"partial answer"}}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	f := chatCompletionsFinal{last: &resp}
	var msgs []openai.ChatCompletionMessageParamUnion
	f.continueWith(&msgs, "keep going", "my reasoning")

	raw, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{`"role":"assistant"`, `"content":"partial answer"`, `"reasoning_content":"my reasoning"`, `"role":"user"`, `"content":"keep going"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if f.last != nil {
		t.Errorf("the reply is recorded once")
	}
}

// DeepSeek (thinking on by default, 400 without it) and Qwen (read with preserve_thinking) get the
// tool-call turn's reasoning_content echoed back, through the shared reasoningChatAdapter.
func TestReasoningChatAdapters_ReplayReasoningContentOnToolCall(t *testing.T) {
	for name, build := range map[string]func(baseURL string) AgentAdapter{
		"deepseek": func(baseURL string) AgentAdapter {
			return NewDeepSeekAdapter(NewDeepSeekProvider("k", baseURL, nil, nil), openai.ChatCompletionNewParams{Model: "deepseek-flash"}, nil, nil)
		},
		"qwen": func(baseURL string) AgentAdapter {
			return NewQwenAdapter(NewQwenProvider("k", baseURL, nil, nil), openai.ChatCompletionNewParams{Model: "qwen3.7-plus"}, nil, nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv, body := recordingSSEServer(t, []string{
				streamedToolRoundSSE("checking the weather", `{\"city\":\"Paris\"}`),
				chatCompletionSSE(nil, []string{"Sunny."}, "stop"),
			})
			defer srv.Close()
			a := build(srv.URL)
			var live strings.Builder
			a.(ReasoningStreamer).SetReasoningStream(ReasoningStream{OnDelta: func(d string) { live.WriteString(d) }})
			a.SetTextDeltaHandler(func(string) {})

			_, uses, err := a.Call(context.Background())
			require.NoError(t, err)
			require.Len(t, uses, 1)
			a.AppendToolResults([]ToolResult{{ID: uses[0].ID, Output: "sunny"}})
			final, _, err := a.Call(context.Background())
			require.NoError(t, err)

			require.Equal(t, "checking the weather", gjson.Get(body(1), "messages.#(role==\"assistant\").reasoning_content").String())
			require.Equal(t, "checking the weather", final.Reasoning)
			require.Equal(t, "checking the weather", live.String())
		})
	}
}
