package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/tidwall/gjson"
)

// Reuse the MCP HTTP fixture and scripted adapter from the existing loop tests.
// A registered local handler exercises the same dispatch path without MCP.
func assertMCPAndLocalErrorContinuation(t *testing.T, a *Agent, chatCtx *chatContext, mcpUse provider.ToolUse) {
	t.Helper()
	previous := extraToolHandlersForChat
	defer func() { extraToolHandlersForChat = previous }()
	extraToolHandlersForChat = func(*Agent, *models.Chat) map[string]ExtraToolHandler {
		return map[string]ExtraToolHandler{"local_failure": func(context.Context, []byte) (string, []*models.FileAttachment, error) {
			return "", nil, errors.New("local tool failed")
		}}
	}
	localUse := provider.ToolUse{ID: "local-error", Name: "local_failure", Input: json.RawMessage(`{}`)}
	uses := []provider.ToolUse{mcpUse, localUse}
	offering(chatCtx, mcpUse.Name, localUse.Name)
	adapter := &scriptedAdapter{callFn: func(step int) (*provider.GenerateResponse, []provider.ToolUse, error) {
		if step == 0 {
			return nil, uses, nil
		}
		return &provider.GenerateResponse{Text: "recovered after tool failures"}, nil, nil
	}}
	reply, calls, _, err := a.handleAgentLoop(context.Background(), chatCtx, adapter)
	require.NoError(t, err, "tool failures must not abort the agent turn")
	require.Equal(t, "recovered after tool failures", reply.Text)
	require.Equal(t, 2, adapter.callStep)
	require.Len(t, adapter.appendedBatches, 1)
	results := adapter.appendedBatches[0]
	require.Len(t, results, 2)
	require.Len(t, calls, 2)
	for i, result := range results {
		require.True(t, result.IsErr)
		require.Equal(t, uses[i].ID, result.ID)
		require.NotEmpty(t, result.Output)
		require.Equal(t, result.Output, calls[i].ToolError, "the persistence record uses the model-visible error")
		require.Empty(t, calls[i].ToolOutput)
	}
	require.Contains(t, results[0].Output, "graph rendering failed")
	require.Equal(t, "local tool failed", results[1].Output)
	assertErrorResultsOpenAI(t, results)
	assertErrorResultsClaude(t, uses, results)
}

func assertErrorResultsOpenAI(t *testing.T, results []provider.ToolResult) {
	t.Helper()
	adapter := provider.NewOpenAIAdapter(nil, responses.ResponseNewParams{Model: "test-model"})
	adapter.AppendToolResults(results)
	encoded, err := json.Marshal(adapter.CurrentParams())
	require.NoError(t, err)
	outputs := map[string]string{}
	for _, item := range gjson.GetBytes(encoded, "input").Array() {
		if item.Get("type").String() == "function_call_output" {
			outputs[item.Get("call_id").String()] = item.Get("output").String()
		}
	}
	require.Len(t, outputs, len(results))
	for _, result := range results {
		require.Equal(t, result.Output, outputs[result.ID])
	}
}

func assertErrorResultsClaude(t *testing.T, uses []provider.ToolUse, results []provider.ToolResult) {
	t.Helper()
	requests := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read provider request: %v", err)
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}
		select {
		case requests <- body:
		default:
			t.Error("unexpected repeated provider request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, err = io.WriteString(w, `{"id":"msg-test","type":"message","role":"assistant","model":"test-model","content":[{"type":"text","text":"recovered"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
		if err != nil {
			t.Errorf("write provider response: %v", err)
		}
	}))
	defer srv.Close()
	var toolBlocks []anthropic.ContentBlockParamUnion
	for _, use := range uses {
		toolBlocks = append(toolBlocks, anthropic.NewToolUseBlock(use.ID, map[string]any{}, use.Name))
	}
	params := anthropic.MessageNewParams{
		Model: "test-model", MaxTokens: 100,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("run the tools")),
			anthropic.NewAssistantMessage(toolBlocks...),
		},
	}
	client := provider.NewClaudeProviderWithBaseURL("test-key", srv.URL, nil, srv.Client())
	adapter := provider.NewClaudeAdapter(client, params, nil, false, nil, nil)
	adapter.AppendToolResults(results)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reply, nextUses, err := adapter.Call(ctx)
	require.NoError(t, err)
	require.Empty(t, nextUses)
	require.Equal(t, "recovered", reply.Text)
	outputs := map[string]gjson.Result{}
	for _, message := range gjson.GetBytes(<-requests, "messages").Array() {
		for _, block := range message.Get("content").Array() {
			if block.Get("type").String() == "tool_result" {
				outputs[block.Get("tool_use_id").String()] = block
			}
		}
	}
	require.Len(t, outputs, len(results))
	for _, result := range results {
		block := outputs[result.ID]
		require.True(t, block.Get("is_error").Bool())
		require.Equal(t, result.Output, block.Get("content.0.text").String())
	}
}
