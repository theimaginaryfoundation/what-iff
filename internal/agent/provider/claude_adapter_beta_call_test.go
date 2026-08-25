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

// newTestClaudeMCPAdapter builds a ClaudeAdapter in beta-MCP mode (useBetaMCP),
// the path Call/ForceFinalResponse/AppendToolResults take when an MCP server is
// configured. Same wire format as the plain Messages API (id/type/role/model/
// content/stop_reason/usage), just routed through client.Beta.Messages.
func newTestClaudeMCPAdapter(baseURL string) *ClaudeAdapter {
	provider := NewClaudeProviderWithBaseURL("test-key", baseURL, nil, nil)
	params := anthropic.MessageNewParams{
		Model:     "test-model",
		MaxTokens: 100,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		},
	}
	mcp := &ClaudeMCPConfig{
		Servers: []anthropic.BetaRequestMCPServerURLDefinitionParam{
			{Name: "mcp-a", URL: "https://mcp.example.com"},
		},
	}
	return NewClaudeAdapter(provider, params, nil, false, mcp, nil)
}

func TestClaudeAdapter_BetaMode_Call_TextResponse(t *testing.T) {
	srv := jsonHandlerServer(t, claudeMessageTextJSON("bmsg_1", "hello beta claude"))
	defer srv.Close()

	a := newTestClaudeMCPAdapter(srv.URL)
	require.True(t, a.useBetaMCP)

	resp, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, toolUses)
	require.Equal(t, "hello beta claude", resp.Text)
	require.Len(t, a.AllRawBetaMessages(), 1)
	require.Zero(t, a.WebSearchCompletedCount())
}

func TestClaudeAdapter_BetaMode_Call_ToolUse(t *testing.T) {
	srv := jsonHandlerServer(t, claudeMessageToolUseJSON("bmsg_2", "toolu_b1", "do_thing", `{"x":1}`))
	defer srv.Close()

	a := newTestClaudeMCPAdapter(srv.URL)
	resp, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Len(t, toolUses, 1)
	require.Equal(t, "toolu_b1", toolUses[0].ID)
	require.Equal(t, "do_thing", toolUses[0].Name)
}

func TestClaudeAdapter_BetaMode_Call_APIErrorIsWrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`))
	}))
	defer srv.Close()

	a := newTestClaudeMCPAdapter(srv.URL)
	resp, toolUses, err := a.Call(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Anthropic Beta API call failed")
	require.Nil(t, resp)
	require.Nil(t, toolUses)
}

func TestClaudeAdapter_BetaMode_AppendToolResults_FoldsIntoNextCall(t *testing.T) {
	var lastBody string
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		lastBody = string(body)
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			_, _ = w.Write([]byte(claudeMessageToolUseJSON("bmsg_3", "toolu_b2", "do_thing", `{}`)))
			return
		}
		_, _ = w.Write([]byte(claudeMessageTextJSON("bmsg_4", "beta final answer")))
	}))
	defer srv.Close()

	a := newTestClaudeMCPAdapter(srv.URL)
	_, toolUses, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Len(t, toolUses, 1)

	a.AppendToolResults([]ToolResult{{ID: toolUses[0].ID, Output: "beta-42"}})

	resp, toolUses2, err := a.Call(context.Background())
	require.NoError(t, err)
	require.Empty(t, toolUses2)
	require.Equal(t, "beta final answer", resp.Text)
	require.Contains(t, lastBody, "toolu_b2")
	require.Contains(t, lastBody, "beta-42")
}

func TestClaudeAdapter_BetaMode_ForceFinalResponse(t *testing.T) {
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		lastBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(claudeMessageTextJSON("bmsg_5", "beta forced final")))
	}))
	defer srv.Close()

	a := newTestClaudeMCPAdapter(srv.URL)
	resp, err := a.ForceFinalResponse(context.Background())
	require.NoError(t, err)
	require.Equal(t, "beta forced final", resp.Text)
	require.Empty(t, a.betaParams.Tools, "ForceFinalResponse must strip the beta tool list too")
	require.Contains(t, lastBody, "final response")
}

func TestClaudeAdapter_BetaMode_ForceFinalResponse_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"nope"}}`))
	}))
	defer srv.Close()

	a := newTestClaudeMCPAdapter(srv.URL)
	resp, err := a.ForceFinalResponse(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "Anthropic beta final-response call failed")
	require.Nil(t, resp)
}

func TestExtractClaudeBetaToolUses(t *testing.T) {
	require.Empty(t, extractClaudeBetaToolUses(nil))

	raw := `{"id":"bmsg_6","type":"message","role":"assistant","model":"test-model",` +
		`"content":[{"type":"tool_use","id":"toolu_x","name":"do_thing","input":{"x":1}}],` +
		`"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`
	var msg anthropic.BetaMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &msg))

	uses := extractClaudeBetaToolUses(&msg)
	require.Len(t, uses, 1)
	require.Equal(t, "toolu_x", uses[0].ID)
	require.Equal(t, "do_thing", uses[0].Name)
	require.JSONEq(t, `{"x":1}`, string(uses[0].Input))
}

func TestCountWebSearchToolResultsInBetaMessage(t *testing.T) {
	require.Zero(t, countWebSearchToolResultsInBetaMessage(nil))

	raw := `{"id":"bmsg_ws","type":"message","role":"assistant","model":"test-model","content":[
		{"type":"text","text":"searching"},
		{"type":"web_search_tool_result","tool_use_id":"srv_01","content":[{"type":"web_search_result","title":"Example","url":"https://example.com","encrypted_content":"enc"}]}
	],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`
	var msg anthropic.BetaMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &msg))
	require.Equal(t, 1, countWebSearchToolResultsInBetaMessage(&msg))
}

func TestClaudeBetaWebSearchToolResultReplayable(t *testing.T) {
	replayable := `{"type":"web_search_tool_result","tool_use_id":"srv_01","content":[{"type":"web_search_result","title":"Example","url":"https://example.com","encrypted_content":"enc"}]}`
	var withURL anthropic.BetaWebSearchToolResultBlock
	require.NoError(t, json.Unmarshal([]byte(replayable), &withURL))
	require.True(t, claudeBetaWebSearchToolResultReplayable(withURL))

	empty := `{"type":"web_search_tool_result","tool_use_id":"srv_02","content":[]}`
	var noContent anthropic.BetaWebSearchToolResultBlock
	require.NoError(t, json.Unmarshal([]byte(empty), &noContent))
	require.False(t, claudeBetaWebSearchToolResultReplayable(noContent))
}

func TestAppendClaudeInLoopWebSearchContextText(t *testing.T) {
	require.NotPanics(t, func() { appendClaudeInLoopWebSearchContextText(nil, "text") })

	params := &anthropic.MessageNewParams{}
	appendClaudeInLoopWebSearchContextText(params, "   ")
	require.Empty(t, params.Messages, "blank text must not append a message")

	appendClaudeInLoopWebSearchContextText(params, "web search results here")
	require.Len(t, params.Messages, 1)
	require.Equal(t, anthropic.MessageParamRoleUser, params.Messages[0].Role)
}

func TestAppendClaudeBetaInLoopWebSearchContextText(t *testing.T) {
	require.NotPanics(t, func() { appendClaudeBetaInLoopWebSearchContextText(nil, "text") })

	params := &anthropic.BetaMessageNewParams{}
	appendClaudeBetaInLoopWebSearchContextText(params, "")
	require.Empty(t, params.Messages, "empty text must not append a message")

	appendClaudeBetaInLoopWebSearchContextText(params, "beta web search results here")
	require.Len(t, params.Messages, 1)
	require.Equal(t, anthropic.BetaMessageParamRoleUser, params.Messages[0].Role)
}

func TestBuildClaudeBetaMCPParams(t *testing.T) {
	base := anthropic.MessageNewParams{
		Model:     "test-model",
		MaxTokens: 128,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		},
	}
	mcp := &ClaudeMCPConfig{
		Servers: []anthropic.BetaRequestMCPServerURLDefinitionParam{
			{Name: "mcp-a", URL: "https://mcp.example.com"},
		},
	}
	out, err := BuildClaudeBetaMCPParams(base, mcp)
	require.NoError(t, err)
	require.Equal(t, base.Model, out.Model)
	require.Len(t, out.MCPServers, 1)
	require.NotEmpty(t, out.Betas)
}
