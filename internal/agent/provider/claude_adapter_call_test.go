package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
)

func newTestClaudeAdapter(baseURL string) *ClaudeAdapter {
	provider := NewClaudeProviderWithBaseURL("test-key", baseURL, nil, nil)
	params := anthropic.MessageNewParams{
		Model:     "test-model",
		MaxTokens: 100,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		},
	}
	return NewClaudeAdapter(provider, params, nil, false, nil, nil)
}

func claudeMessageTextJSON(id, text string) string {
	return `{"id":"` + id + `","type":"message","role":"assistant","model":"test-model",` +
		`"content":[{"type":"text","text":"` + text + `"}],"stop_reason":"end_turn","stop_sequence":null,` +
		`"usage":{"input_tokens":5,"output_tokens":7}}`
}

func claudeMessageToolUseJSON(id, toolID, name string, input string) string {
	return `{"id":"` + id + `","type":"message","role":"assistant","model":"test-model",` +
		`"content":[{"type":"tool_use","id":"` + toolID + `","name":"` + name + `","input":` + input + `}],` +
		`"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":7}}`
}

// claudeSSEServer streams Anthropic Messages API events. Unlike the OpenAI/Gemini
// SSE format (a bare `data: <json>` line with the event type embedded in the JSON),
// the Anthropic SDK's stream decoder dispatches strictly on the SSE `event:` line
// (packages/ssestream), so each frame needs both lines.
func claudeSSEServer(t *testing.T, events []struct{ event, data string }) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		for _, e := range events {
			_, _ = w.Write([]byte("event: " + e.event + "\ndata: " + e.data + "\n\n"))
			flusher.Flush()
		}
	}))
}

func TestClaudeAdapter_Call_TextResponse(t *testing.T) {
	srv := jsonHandlerServer(t, claudeMessageTextJSON("msg_1", "hello claude"))
	defer srv.Close()

	a := newTestClaudeAdapter(srv.URL)
	resp, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, toolUses)
	require.Equal(t, "hello claude", resp.Text)
	require.Equal(t, int64(5), resp.InputTokens)
	require.Equal(t, int64(7), resp.OutputTokens)

	require.Len(t, a.AllRawMessages(), 1)
	require.Zero(t, a.WebSearchCompletedCount())
}

func TestClaudeAdapter_Call_ToolUse(t *testing.T) {
	srv := jsonHandlerServer(t, claudeMessageToolUseJSON("msg_2", "toolu_1", "do_thing", `{"x":1}`))
	defer srv.Close()

	a := newTestClaudeAdapter(srv.URL)
	resp, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Len(t, toolUses, 1)
	require.Equal(t, "toolu_1", toolUses[0].ID)
	require.Equal(t, "do_thing", toolUses[0].Name)
	require.JSONEq(t, `{"x":1}`, string(toolUses[0].Input))
}

func TestClaudeAdapter_Call_APIErrorIsWrappedNotSafetyViolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`))
	}))
	defer srv.Close()

	a := newTestClaudeAdapter(srv.URL)
	resp, toolUses, err := a.Call(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Anthropic API call failed")
	_, isSafety := IsSafetyViolationError(err)
	require.False(t, isSafety)
	require.Nil(t, resp)
	require.Nil(t, toolUses)
}

func TestClaudeAdapter_AppendToolResults_FoldsIntoNextCall(t *testing.T) {
	var lastBody string
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		lastBody = string(body)
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			_, _ = w.Write([]byte(claudeMessageToolUseJSON("msg_3", "toolu_2", "do_thing", `{}`)))
			return
		}
		_, _ = w.Write([]byte(claudeMessageTextJSON("msg_4", "final answer")))
	}))
	defer srv.Close()

	a := newTestClaudeAdapter(srv.URL)
	_, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Len(t, toolUses, 1)

	a.AppendToolResults([]ToolResult{{ID: toolUses[0].ID, Output: "42"}})

	resp, toolUses2, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, toolUses2)
	require.Equal(t, "final answer", resp.Text)

	require.Contains(t, lastBody, "tool_result")
	require.Contains(t, lastBody, "toolu_2")
	require.Contains(t, lastBody, "42")
}

func TestClaudeAdapter_ForceFinalResponse(t *testing.T) {
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		lastBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(claudeMessageTextJSON("msg_5", "final claude answer")))
	}))
	defer srv.Close()

	a := newTestClaudeAdapter(srv.URL)
	a.params.Tools = []anthropic.ToolUnionParam{ClaudeFunctionTool("some_tool", "desc", nil, nil, false)}

	resp, err := a.ForceFinalResponse(context.Background())
	require.NoError(t, err)
	require.Equal(t, "final claude answer", resp.Text)
	require.Empty(t, a.params.Tools, "ForceFinalResponse must strip the tool list")
	require.Contains(t, lastBody, "final response")
	require.Len(t, a.AllRawMessages(), 1)
}

func TestClaudeAdapter_ForceFinalResponse_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"nope"}}`))
	}))
	defer srv.Close()

	a := newTestClaudeAdapter(srv.URL)
	resp, err := a.ForceFinalResponse(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Anthropic final-response call failed")
	require.Nil(t, resp)
}

func TestClaudeAdapter_SetTextDeltaHandlerEnablesStreaming(t *testing.T) {
	events := []struct{ event, data string }{
		{"message_start", `{"type":"message_start","message":{"id":"msg_6","type":"message","role":"assistant","model":"test-model","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}}`},
		{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":5}}`},
		{"message_stop", `{"type":"message_stop"}`},
	}
	srv := claudeSSEServer(t, events)
	defer srv.Close()

	a := newTestClaudeAdapter(srv.URL)
	var deltas []string
	a.SetTextDeltaHandler(func(d string) { deltas = append(deltas, d) })

	resp, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, toolUses)
	require.Equal(t, []string{"Hi", " there"}, deltas)
	require.Equal(t, "Hi there", resp.Text)
	require.Equal(t, int64(10), resp.InputTokens)
	require.Equal(t, int64(5), resp.OutputTokens)
}

func TestCountWebSearchToolResultsInMessage(t *testing.T) {
	require.Zero(t, countWebSearchToolResultsInMessage(nil))

	raw := `{"id":"msg_ws","type":"message","role":"assistant","model":"test-model","content":[
		{"type":"text","text":"searching"},
		{"type":"web_search_tool_result","tool_use_id":"srv_01","content":[{"type":"web_search_result","title":"Example","url":"https://example.com","encrypted_content":"enc","page_age":"1d"}]},
		{"type":"web_search_tool_result","tool_use_id":"srv_02","content":[]}
	],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`
	var msg anthropic.Message
	require.NoError(t, json.Unmarshal([]byte(raw), &msg))
	require.Equal(t, 2, countWebSearchToolResultsInMessage(&msg))
}
