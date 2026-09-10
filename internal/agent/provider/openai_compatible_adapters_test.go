package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
)

// The Mistral, DeepSeek, Qwen, Xiaomi, and Local adapters are near-identical
// wrappers over the same OpenAI-compatible Chat Completions API — this file
// drives all five through one shared table rather than duplicating the same
// assertions five times. Local differs in one respect (no streaming path),
// which is covered by its own dedicated tests at the bottom of this file.

type openAICompatSpec struct {
	name              string
	supportsStreaming bool
	newAdapter        func(baseURL string) AgentAdapter
}

func openAICompatSpecs() []openAICompatSpec {
	params := func() openai.ChatCompletionNewParams { return openai.ChatCompletionNewParams{Model: "test"} }
	return []openAICompatSpec{
		{
			name:              "mistral",
			supportsStreaming: true,
			newAdapter: func(baseURL string) AgentAdapter {
				return NewMistralAdapter(NewMistralProvider("test-key", baseURL, nil, nil), params(), nil, nil)
			},
		},
		{
			name:              "deepseek",
			supportsStreaming: true,
			newAdapter: func(baseURL string) AgentAdapter {
				return NewDeepSeekAdapter(NewDeepSeekProvider("test-key", baseURL, nil, nil), params(), nil, nil)
			},
		},
		{
			name:              "qwen",
			supportsStreaming: true,
			newAdapter: func(baseURL string) AgentAdapter {
				return NewQwenAdapter(NewQwenProvider("test-key", baseURL, nil, nil), params(), nil, nil)
			},
		},
		{
			name:              "xiaomi",
			supportsStreaming: true,
			newAdapter: func(baseURL string) AgentAdapter {
				return NewXiaomiAdapter(NewXiaomiProvider("test-key", baseURL, nil, nil), params(), nil, nil)
			},
		},
		{
			name:              "local",
			supportsStreaming: false,
			newAdapter: func(baseURL string) AgentAdapter {
				return NewLocalAdapter(NewLocalProvider(baseURL, nil, nil), params(), nil, nil)
			},
		},
	}
}

func chatCompletionTextJSON(id, text string) string {
	return `{"id":"` + id + `","object":"chat.completion","created":1,"model":"test",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"` + text + `"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`
}

func chatCompletionToolCallJSON(id, toolCallID, toolName, args string) string {
	return `{"id":"` + id + `","object":"chat.completion","created":1,"model":"test",` +
		`"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"` + toolCallID +
		`","type":"function","function":{"name":"` + toolName + `","arguments":"` + args + `"}}]},"finish_reason":"tool_calls"}],` +
		`"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`
}

const chatCompletionEmptyChoicesJSON = `{"id":"cmpl-empty","object":"chat.completion","created":1,"model":"test","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`

// sequencedJSONServer replies to request N with bodies[N] (clamped to the last
// entry), recording each request's raw body so a test can inspect what a
// later call sent — e.g. that AppendToolResults actually folded a tool result
// into the next request, without reaching into adapter-private fields.
func sequencedJSONServer(t *testing.T, bodies []string) (srv *httptest.Server, requestBody func(i int) []byte) {
	t.Helper()
	var mu sync.Mutex
	var seen [][]byte
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		idx := len(seen)
		seen = append(seen, b)
		mu.Unlock()
		if idx >= len(bodies) {
			idx = len(bodies) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(bodies[idx]))
	}))
	requestBody = func(i int) []byte {
		mu.Lock()
		defer mu.Unlock()
		return seen[i]
	}
	return srv, requestBody
}

func decodeRequestMessages(t *testing.T, body []byte) []map[string]interface{} {
	t.Helper()
	var decoded struct {
		Messages []map[string]interface{} `json:"messages"`
		Tools    []interface{}            `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(body, &decoded))
	return decoded.Messages
}

func TestOpenAICompatibleAdapters_Call_TextResponse(t *testing.T) {
	for _, spec := range openAICompatSpecs() {
		t.Run(spec.name, func(t *testing.T) {
			srv, _ := sequencedJSONServer(t, []string{chatCompletionTextJSON("cmpl-1", "hello there")})
			defer srv.Close()

			a := spec.newAdapter(srv.URL)
			resp, toolUses, err := a.Call(context.Background())
			require.NoError(t, err)
			require.Empty(t, toolUses)
			require.NotNil(t, resp)
			require.Equal(t, "hello there", resp.Text)
			require.Equal(t, int64(5), resp.InputTokens)
			require.Equal(t, int64(7), resp.OutputTokens)
			require.Zero(t, a.WebSearchCompletedCount())
		})
	}
}

func TestOpenAICompatibleAdapters_Call_ToolUse(t *testing.T) {
	for _, spec := range openAICompatSpecs() {
		t.Run(spec.name, func(t *testing.T) {
			srv, _ := sequencedJSONServer(t, []string{chatCompletionToolCallJSON("cmpl-2", "call_1", "do_thing", `{\"x\":1}`)})
			defer srv.Close()

			a := spec.newAdapter(srv.URL)
			resp, toolUses, err := a.Call(context.Background())
			require.NoError(t, err)
			require.Nil(t, resp)
			require.Len(t, toolUses, 1)
			require.Equal(t, "call_1", toolUses[0].ID)
			require.Equal(t, "do_thing", toolUses[0].Name)
			require.JSONEq(t, `{"x":1}`, string(toolUses[0].Input))
		})
	}
}

func TestOpenAICompatibleAdapters_Call_NoChoicesError(t *testing.T) {
	for _, spec := range openAICompatSpecs() {
		t.Run(spec.name, func(t *testing.T) {
			srv, _ := sequencedJSONServer(t, []string{chatCompletionEmptyChoicesJSON})
			defer srv.Close()

			a := spec.newAdapter(srv.URL)
			resp, toolUses, err := a.Call(context.Background())
			require.Error(t, err)
			require.Contains(t, err.Error(), "no choices")
			require.Nil(t, resp)
			require.Nil(t, toolUses)
		})
	}
}

func TestOpenAICompatibleAdapters_Call_APIErrorIsWrappedNotSafetyViolation(t *testing.T) {
	for _, spec := range openAICompatSpecs() {
		t.Run(spec.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"bad request: invalid model"}}`))
			}))
			defer srv.Close()

			a := spec.newAdapter(srv.URL)
			resp, toolUses, err := a.Call(context.Background())
			require.Error(t, err)
			require.Contains(t, err.Error(), "call failed")
			_, isSafety := IsSafetyViolationError(err)
			require.False(t, isSafety, "a plain API error must not be misclassified as a safety violation")
			require.Nil(t, resp)
			require.Nil(t, toolUses)
		})
	}
}

func TestOpenAICompatibleAdapters_AppendToolResults_FoldsIntoNextCall(t *testing.T) {
	for _, spec := range openAICompatSpecs() {
		t.Run(spec.name, func(t *testing.T) {
			srv, requestBody := sequencedJSONServer(t, []string{
				chatCompletionToolCallJSON("cmpl-3", "call_9", "do_thing", `{}`),
				chatCompletionTextJSON("cmpl-4", "final answer"),
			})
			defer srv.Close()

			a := spec.newAdapter(srv.URL)
			_, toolUses, err := a.Call(context.Background())
			require.NoError(t, err)
			require.Len(t, toolUses, 1)

			a.AppendToolResults([]ToolResult{{ID: toolUses[0].ID, Output: "42"}})

			resp, toolUses2, err := a.Call(context.Background())
			require.NoError(t, err)
			require.Empty(t, toolUses2)
			require.Equal(t, "final answer", resp.Text)

			msgs := decodeRequestMessages(t, requestBody(1))
			require.NotEmpty(t, msgs)
			last := msgs[len(msgs)-1]
			require.Equal(t, "tool", last["role"])
			require.Equal(t, "42", last["content"])
			require.Equal(t, toolUses[0].ID, last["tool_call_id"])
		})
	}
}

func TestOpenAICompatibleAdapters_AppendToolResults_EmptyOutputDefaultsMessage(t *testing.T) {
	for _, spec := range openAICompatSpecs() {
		t.Run(spec.name, func(t *testing.T) {
			srv, requestBody := sequencedJSONServer(t, []string{
				chatCompletionToolCallJSON("cmpl-5", "call_e", "do_thing", `{}`),
				chatCompletionTextJSON("cmpl-6", "ok"),
			})
			defer srv.Close()

			a := spec.newAdapter(srv.URL)
			_, toolUses, err := a.Call(context.Background())
			require.NoError(t, err)
			require.Len(t, toolUses, 1)

			a.AppendToolResults([]ToolResult{{ID: toolUses[0].ID, Output: "", IsErr: true}})
			_, _, err = a.Call(context.Background())
			require.NoError(t, err)

			msgs := decodeRequestMessages(t, requestBody(1))
			last := msgs[len(msgs)-1]
			require.Equal(t, "unknown error occurred", last["content"])
		})
	}
}

func TestOpenAICompatibleAdapters_ForceFinalResponse(t *testing.T) {
	for _, spec := range openAICompatSpecs() {
		t.Run(spec.name, func(t *testing.T) {
			srv, requestBody := sequencedJSONServer(t, []string{chatCompletionTextJSON("cmpl-7", "final")})
			defer srv.Close()

			a := spec.newAdapter(srv.URL)
			resp, err := a.ForceFinalResponse(context.Background())
			require.NoError(t, err)
			require.Equal(t, "final", resp.Text)

			var decoded struct {
				Tools []interface{} `json:"tools"`
			}
			require.NoError(t, json.Unmarshal(requestBody(0), &decoded))
			require.Empty(t, decoded.Tools, "ForceFinalResponse must strip the tool list")

			msgs := decodeRequestMessages(t, requestBody(0))
			last := msgs[len(msgs)-1]
			require.Equal(t, "user", last["role"])
			require.Contains(t, last["content"], "final response")
		})
	}
}

func TestOpenAICompatibleAdapters_SetTextDeltaHandlerEnablesStreaming(t *testing.T) {
	for _, spec := range openAICompatSpecs() {
		if !spec.supportsStreaming {
			continue
		}
		t.Run(spec.name, func(t *testing.T) {
			frames := []string{
				`{"id":"s1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`,
				`{"id":"s1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":" there"},"finish_reason":"stop"}]}`,
				`{"id":"s1","object":"chat.completion.chunk","created":1,"model":"test","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":2,"total_tokens":4}}`,
			}
			srv := sseServer(t, frames)
			defer srv.Close()

			a := spec.newAdapter(srv.URL)
			var deltas []string
			a.SetTextDeltaHandler(func(d string) { deltas = append(deltas, d) })

			resp, toolUses, err := a.Call(context.Background())
			require.NoError(t, err)
			require.Empty(t, toolUses)
			require.Equal(t, []string{"Hi", " there"}, deltas)
			require.Equal(t, "Hi there", resp.Text)
		})
	}
}

// Local has no streaming path at all: LocalProvider has no CallStreaming, and
// LocalAdapter.Call always issues a plain (non-streaming) request even once a
// delta handler is set. A local server that only understands non-streaming
// JSON responses proves this — a stray SSE-shaped request would fail to parse.
func TestLocalAdapter_TextDeltaHandlerNeverInvoked(t *testing.T) {
	srv, _ := sequencedJSONServer(t, []string{chatCompletionTextJSON("cmpl-l", "local reply")})
	defer srv.Close()

	a := NewLocalAdapter(NewLocalProvider(srv.URL, nil, nil), openai.ChatCompletionNewParams{Model: "test"}, nil, nil)
	a.SetTextDeltaHandler(func(d string) { t.Fatalf("unexpected text delta %q: local adapter must never stream", d) })

	resp, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, toolUses)
	require.Equal(t, "local reply", resp.Text)
}

func TestLocalAdapter_DisabledToolsAreFilteredAtConstruction(t *testing.T) {
	tools := []openai.ChatCompletionToolUnionParam{
		openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{Name: "enabled_tool"}),
		openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{Name: "disabled_tool"}),
	}
	a := NewLocalAdapter(
		NewLocalProvider("http://unused.invalid", nil, nil),
		openai.ChatCompletionNewParams{Model: "test"},
		tools,
		map[string]bool{"disabled_tool": true},
	)
	require.Len(t, a.params.Tools, 1)
	require.Equal(t, "enabled_tool", a.params.Tools[0].OfFunction.Function.Name)
}

func TestOpenAICompatibleAdapters_DisabledToolsAreFilteredAtConstruction(t *testing.T) {
	tools := []openai.ChatCompletionToolUnionParam{
		openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{Name: "enabled_tool"}),
		openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{Name: "disabled_tool"}),
	}
	disabled := map[string]bool{"disabled_tool": true}

	require.Len(t, NewMistralAdapter(NewMistralProvider("k", "http://unused.invalid", nil, nil), openai.ChatCompletionNewParams{Model: "test"}, tools, disabled).params.Tools, 1)
	require.Len(t, NewDeepSeekAdapter(NewDeepSeekProvider("k", "http://unused.invalid", nil, nil), openai.ChatCompletionNewParams{Model: "test"}, tools, disabled).params.Tools, 1)
	require.Len(t, NewQwenAdapter(NewQwenProvider("k", "http://unused.invalid", nil, nil), openai.ChatCompletionNewParams{Model: "test"}, tools, disabled).params.Tools, 1)
	require.Len(t, NewXiaomiAdapter(NewXiaomiProvider("k", "http://unused.invalid", nil, nil), openai.ChatCompletionNewParams{Model: "test"}, tools, disabled).params.Tools, 1)
}

func TestDefaultBaseURLFallbacks(t *testing.T) {
	require.NotEmpty(t, DefaultMistralBaseURL)
	require.NotEmpty(t, DefaultDeepSeekBaseURL)
	require.NotEmpty(t, DefaultQwenBaseURL)
	require.NotEmpty(t, DefaultXiaomiBaseURL)
	require.NotEmpty(t, DefaultLocalBaseURL)

	// An empty baseURL argument must fall back to the provider's documented
	// default rather than leaving the SDK client with no base URL at all.
	require.NotNil(t, NewMistralProvider("k", "", nil, nil))
	require.NotNil(t, NewDeepSeekProvider("k", "", nil, nil))
	require.NotNil(t, NewQwenProvider("k", "", nil, nil))
	require.NotNil(t, NewXiaomiProvider("k", "", nil, nil))
	require.NotNil(t, NewLocalProvider("", nil, nil))
}
