package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
)

func newTestOpenAIProvider(baseURL string) *OpenAIProvider {
	client := openai.NewClient(option.WithAPIKey("test-key"), option.WithBaseURL(baseURL))
	return NewOpenAIProvider(nil, &client, nil, nil)
}

func responseTextJSON(id, text string) string {
	return `{"id":"` + id + `","object":"response","created_at":1,"model":"test","status":"completed",` +
		`"output":[{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"` + text + `"}]}],` +
		`"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}`
}

func responseToolCallJSON(id, callID, name, args string) string {
	return `{"id":"` + id + `","object":"response","created_at":1,"model":"test","status":"completed",` +
		`"output":[{"type":"function_call","call_id":"` + callID + `","name":"` + name + `","arguments":"` + args + `"}],` +
		`"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}`
}

func jsonHandlerServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestOpenAIAdapter_Call_TextResponse(t *testing.T) {
	srv := jsonHandlerServer(t, responseTextJSON("resp_1", "hello from openai"))
	defer srv.Close()

	a := NewOpenAIAdapter(newTestOpenAIProvider(srv.URL), responses.ResponseNewParams{Model: "test"})
	resp, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, toolUses)
	require.Equal(t, "hello from openai", resp.Text)
	require.Equal(t, int64(5), resp.InputTokens)
	require.Equal(t, int64(7), resp.OutputTokens)

	require.Equal(t, "resp_1", a.LastRawResponse().ID)
	require.Len(t, a.AllRawResponses(), 1)
	require.Zero(t, a.WebSearchCompletedCount())
}

func TestOpenAIAdapter_Call_ToolUse(t *testing.T) {
	srv := jsonHandlerServer(t, responseToolCallJSON("resp_2", "call_1", "do_thing", `{\"x\":1}`))
	defer srv.Close()

	a := NewOpenAIAdapter(newTestOpenAIProvider(srv.URL), responses.ResponseNewParams{Model: "test"})
	resp, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Len(t, toolUses, 1)
	require.Equal(t, "call_1", toolUses[0].ID)
	require.Equal(t, "do_thing", toolUses[0].Name)
	require.JSONEq(t, `{"x":1}`, string(toolUses[0].Input))

	// A successful Call must thread previousResponseID for the next request.
	require.Equal(t, "resp_2", a.previousResponseID)
}

func TestOpenAIAdapter_Call_APIErrorIsWrappedNotSafetyViolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request: invalid model"}}`))
	}))
	defer srv.Close()

	a := NewOpenAIAdapter(newTestOpenAIProvider(srv.URL), responses.ResponseNewParams{Model: "test"})
	resp, toolUses, err := a.Call(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "OpenAI API call failed")
	_, isSafety := IsSafetyViolationError(err)
	require.False(t, isSafety)
	require.Nil(t, resp)
	require.Nil(t, toolUses)
}

func TestOpenAIAdapter_ForceFinalResponse(t *testing.T) {
	srv := jsonHandlerServer(t, responseTextJSON("resp_3", "final openai answer"))
	defer srv.Close()

	a := NewOpenAIAdapter(newTestOpenAIProvider(srv.URL), responses.ResponseNewParams{
		Model: "test",
		Tools: []responses.ToolUnionParam{{OfFunction: &responses.FunctionToolParam{Name: "some_tool"}}},
	})

	resp, err := a.ForceFinalResponse(context.Background())
	require.NoError(t, err)
	require.Equal(t, "final openai answer", resp.Text)
	require.Empty(t, a.params.Tools, "ForceFinalResponse must strip the tool list")
	require.Len(t, a.AllRawResponses(), 1)
}

func TestOpenAIAdapter_ForceFinalResponse_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
	}))
	defer srv.Close()

	a := NewOpenAIAdapter(newTestOpenAIProvider(srv.URL), responses.ResponseNewParams{Model: "test"})
	resp, err := a.ForceFinalResponse(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "OpenAI final-response call failed")
	require.Nil(t, resp)
}

func TestOpenAIAdapter_SetTextDeltaHandlerEnablesStreaming(t *testing.T) {
	frames := []string{
		`{"type":"response.output_text.delta","delta":"Hi","sequence_number":1}`,
		`{"type":"response.output_text.delta","delta":" there","sequence_number":2}`,
		`{"type":"response.completed","sequence_number":3,"response":` +
			responseTextJSON("resp_4", "Hi there") + `}`,
	}
	srv := sseServer(t, frames)
	defer srv.Close()

	a := NewOpenAIAdapter(newTestOpenAIProvider(srv.URL), responses.ResponseNewParams{Model: "test"})
	var deltas []string
	a.SetTextDeltaHandler(func(d string) { deltas = append(deltas, d) })

	resp, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, toolUses)
	require.Equal(t, []string{"Hi", " there"}, deltas)
	require.Equal(t, "Hi there", resp.Text)
}

func TestAllRawResponses_EmptyWhenNoCallsMade(t *testing.T) {
	a := NewOpenAIAdapter(newTestOpenAIProvider("http://unused.invalid"), responses.ResponseNewParams{Model: "test"})
	require.Nil(t, a.AllRawResponses())
}

func TestCountCompletedWebSearchesOpenAI(t *testing.T) {
	require.Zero(t, countCompletedWebSearchesOpenAI(nil))

	// AsWebSearchCall() re-parses each output item from its own captured raw
	// JSON, so the fixture must come through json.Unmarshal (a bare struct
	// literal has no raw JSON behind it and always reports zero values).
	raw := `{"id":"resp_ws","object":"response","created_at":1,"model":"test","status":"completed","output":[
		{"type":"web_search_call","id":"ws1","status":"completed","action":{"type":"search","query":"q"}},
		{"type":"web_search_call","id":"ws2","status":"in_progress","action":{"type":"search","query":"q"}},
		{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"x"}]}
	],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
	var resp responses.Response
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	require.Equal(t, 1, countCompletedWebSearchesOpenAI(&resp))
}

func TestWaitForRetry(t *testing.T) {
	t.Run("returns nil once the timer elapses", func(t *testing.T) {
		require.NoError(t, waitForRetry(context.Background(), time.Millisecond))
	})

	t.Run("returns the context error when cancelled first", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := waitForRetry(ctx, time.Minute)
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestIsRateLimitAndServerError_NilError(t *testing.T) {
	require.False(t, isRateLimitError(nil))
	require.False(t, isServerError(nil))
}
