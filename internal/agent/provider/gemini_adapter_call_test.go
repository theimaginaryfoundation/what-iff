package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestGeminiAdapter(baseURL string) *GeminiAdapter {
	return NewGeminiAdapter(NewGeminiProvider("test-key", baseURL, nil, nil), openai.ChatCompletionNewParams{Model: "test"}, nil, nil, zap.NewNop())
}

func TestGeminiAdapter_Call_TextResponse(t *testing.T) {
	srv, _ := sequencedJSONServer(t, []string{chatCompletionTextJSON("cmpl-g1", "gemini reply")})
	defer srv.Close()

	a := newTestGeminiAdapter(srv.URL)
	resp, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, toolUses)
	require.Equal(t, "gemini reply", resp.Text)
	require.Equal(t, int64(5), resp.InputTokens)
	require.Equal(t, int64(7), resp.OutputTokens)
	require.Zero(t, a.WebSearchCompletedCount())
}

func TestGeminiAdapter_Call_ToolUse(t *testing.T) {
	srv, _ := sequencedJSONServer(t, []string{chatCompletionToolCallJSON("cmpl-g2", "call_g1", "do_thing", `{}`)})
	defer srv.Close()

	a := newTestGeminiAdapter(srv.URL)
	resp, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Len(t, toolUses, 1)
	require.Equal(t, "do_thing", toolUses[0].Name)
	require.Equal(t, toolUses, a.lastRequested)

	// The assistant tool-call turn must be persisted so a follow-up
	// AppendToolResults can find the tool name for this call id.
	require.Len(t, a.params.Messages, 1)
}

func TestGeminiAdapter_Call_NoChoicesError(t *testing.T) {
	srv, _ := sequencedJSONServer(t, []string{chatCompletionEmptyChoicesJSON})
	defer srv.Close()

	a := newTestGeminiAdapter(srv.URL)
	resp, toolUses, err := a.Call(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no choices")
	require.Nil(t, resp)
	require.Nil(t, toolUses)
}

func TestGeminiAdapter_Call_APIErrorIsWrappedWithContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request: invalid model"}}`))
	}))
	defer srv.Close()

	a := newTestGeminiAdapter(srv.URL)
	resp, toolUses, err := a.Call(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Gemini API call failed")
	_, isSafety := IsSafetyViolationError(err)
	require.False(t, isSafety)
	require.Nil(t, resp)
	require.Nil(t, toolUses)
}

func TestGeminiAdapter_ForceFinalResponse(t *testing.T) {
	srv, requestBody := sequencedJSONServer(t, []string{chatCompletionTextJSON("cmpl-g3", "final gemini answer")})
	defer srv.Close()

	a := newTestGeminiAdapter(srv.URL)
	a.params.Tools = []openai.ChatCompletionToolUnionParam{
		openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{Name: "some_tool"}),
	}

	resp, err := a.ForceFinalResponse(context.Background())
	require.NoError(t, err)
	require.Equal(t, "final gemini answer", resp.Text)

	msgs := decodeRequestMessages(t, requestBody(0))
	last := msgs[len(msgs)-1]
	require.Equal(t, "user", last["role"])
	require.Contains(t, last["content"], "final response")
	require.Empty(t, a.params.Tools, "ForceFinalResponse must strip the tool list")
}

func TestGeminiAdapter_ForceFinalResponse_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
	}))
	defer srv.Close()

	a := newTestGeminiAdapter(srv.URL)
	resp, err := a.ForceFinalResponse(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Gemini final-response call failed")
	require.Nil(t, resp)
}

func TestGeminiAdapter_SetTextDeltaHandlerEnablesStreaming(t *testing.T) {
	frames := []string{
		`{"id":"g1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`,
		`{"id":"g1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":" gemini"},"finish_reason":"stop"}]}`,
		`{"id":"g1","object":"chat.completion.chunk","created":1,"model":"test","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":2,"total_tokens":4}}`,
	}
	srv := sseServer(t, frames)
	defer srv.Close()

	a := newTestGeminiAdapter(srv.URL)
	var deltas []string
	a.SetTextDeltaHandler(func(d string) { deltas = append(deltas, d) })

	resp, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, toolUses)
	require.Equal(t, []string{"Hi", " gemini"}, deltas)
	require.Equal(t, "Hi gemini", resp.Text)
}

func TestGeminiAdapter_ToolNameForResult_FallsBackToIndex(t *testing.T) {
	a := newTestGeminiAdapter("http://unused.invalid")
	a.lastRequested = []ToolUse{{ID: "", Name: "only_tool"}}
	require.Equal(t, "only_tool", a.toolNameForResult("nonexistent-id", 0))
	require.Equal(t, "", a.toolNameForResult("nonexistent-id", 5))
}

func TestExtractGeminiToolUses(t *testing.T) {
	require.Empty(t, extractGeminiToolUses(nil))

	resp := &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{Message: openai.ChatCompletionMessage{
				ToolCalls: []openai.ChatCompletionMessageToolCallUnion{
					{ID: "c1", Type: "function", Function: openai.ChatCompletionMessageFunctionToolCallFunction{Name: "t1", Arguments: "{}"}},
				},
			}},
		},
	}
	uses := extractGeminiToolUses(resp)
	require.Len(t, uses, 1)
	require.Equal(t, "c1", uses[0].ID)
	require.Equal(t, "t1", uses[0].Name)
}

func TestTruncateLogString(t *testing.T) {
	require.Equal(t, "abc", truncateLogString("abc", 10))
	require.Equal(t, "abc", truncateLogString("abc", 3))
	require.Equal(t, "ab…(truncated)", truncateLogString("abcdef", 2))
	require.Equal(t, "abcdef", truncateLogString("abcdef", 0))
	require.Equal(t, "abcdef", truncateLogString("abcdef", -1))
}

func TestOpenAIErrorDetail(t *testing.T) {
	require.Empty(t, openAIErrorDetail(nil))
	require.Equal(t, "boom", openAIErrorDetail(errNonAPI{}))
}

type errNonAPI struct{}

func (errNonAPI) Error() string { return "boom" }

func TestSummarizeGeminiMessages(t *testing.T) {
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hello"),
		openai.AssistantMessage("hi there"),
	}
	summary := summarizeGeminiMessages(msgs)
	require.Contains(t, summary, `"content_len":5`)
	require.Contains(t, summary, `"content_len":8`)

	require.Equal(t, "[]", summarizeGeminiMessages(nil))

	// More than the 4-message tail window: only the last 4 must appear.
	many := make([]openai.ChatCompletionMessageParamUnion, 0, 6)
	for i := 0; i < 6; i++ {
		many = append(many, openai.UserMessage("msg"))
	}
	var decoded []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(summarizeGeminiMessages(many)), &decoded))
	require.Len(t, decoded, 4)
}
