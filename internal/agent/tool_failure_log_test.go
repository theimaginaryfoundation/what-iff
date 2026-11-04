package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// mcpFailingServer replies to every tools/call with an isError result carrying errText.
func mcpFailingServer(t *testing.T, errText string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		var result any = map[string]any{}
		switch req["method"] {
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "lookup", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": errText}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A failure the MCP server reports itself is the model's to recover from, not a fault here. It is a
// Warn with metadata only: neither the server's error text nor the call's input may reach the logs,
// because both can hold personal data.
func TestToolReportedFailureIsLoggedAsWarnWithoutItsText(t *testing.T) {
	const (
		serverText = "no mailbox found for alice@example.com"
		inputEmail = "bob@example.com"
	)
	srv := mcpFailingServer(t, serverText)

	serverID := uuid.New()
	prefix := strings.ReplaceAll(serverID.String(), "-", "")
	fullToolName := "mcp__" + prefix[:8] + "__lookup"

	core, logs := observer.New(zap.DebugLevel)
	a := &Agent{mcpClient: mcpclient.New(nil, nil), logger: zap.New(core)}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New()}
	chatCtx := &chatContext{
		userID:     chat.UserID,
		chat:       chat,
		mcpServers: []*models.MCPServer{{ID: serverID, Name: "mail", ServerURL: srv.URL, Status: models.MCPServerStatusActive}},
		loadedMCPTools: map[uuid.UUID]map[string]struct{}{
			serverID: {fullToolName: {}},
		},
	}
	offering(chatCtx, fullToolName)

	input := json.RawMessage(fmt.Sprintf(`{"email":%q}`, inputEmail))
	results, calls, _ := a.executeToolUses(context.Background(), chatCtx, 3, []provider.ToolUse{{ID: "call-1", Name: fullToolName, Input: input}})

	// The model and the stored tool call still get the full text: that is how the person sees what happened.
	require.True(t, results[0].IsErr)
	require.Equal(t, serverText, results[0].Output)
	require.Equal(t, serverText, calls[0].ToolError)

	var failures []observer.LoggedEntry
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "fail") {
			failures = append(failures, e)
		}
	}
	require.Len(t, failures, 1, "one log per failed call")
	entry := failures[0]
	require.Equal(t, zap.WarnLevel, entry.Level, "a tool-reported failure is not an error in this system")
	require.Equal(t, "mcp tool reported a failure", entry.Message)

	ctx := entry.ContextMap()
	require.Equal(t, "call-1", ctx["tool_id"])
	require.Equal(t, fullToolName, ctx["tool_name"])
	require.EqualValues(t, 3, ctx["round"])
	require.EqualValues(t, len(input), ctx["input_bytes"])
	require.EqualValues(t, len(serverText), ctx["error_bytes"])
	require.Equal(t, chat.ID.String(), ctx["chat_id"])
	require.Equal(t, chat.UserID.String(), ctx["user_id"])
	require.NotContains(t, ctx, "error", "the error text must not be logged")

	for _, all := range logs.All() {
		rendered := fmt.Sprintf("%s %v", all.Message, all.ContextMap())
		require.NotContains(t, rendered, "alice@example.com", "the server's error text leaked into a log")
		require.NotContains(t, rendered, inputEmail, "the call's input leaked into a log")
	}
}

// Every other failure keeps the existing behaviour: an Error carrying the message.
func TestOtherToolFailuresAreStillLoggedAsError(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	a := &Agent{logger: zap.New(core)}
	chatCtx := &chatContext{chat: &models.Chat{ID: uuid.New(), UserID: uuid.New()}}

	results, _, _ := a.executeToolUses(context.Background(), chatCtx, 0, []provider.ToolUse{{ID: "t1", Name: "hallucinated_tool_42"}})
	require.True(t, results[0].IsErr)

	entries := logs.FilterMessage("tool call failed").All()
	require.Len(t, entries, 1)
	require.Equal(t, zap.ErrorLevel, entries[0].Level)
	require.Equal(t, "hallucinated_tool_42", entries[0].ContextMap()["tool_name"])
	require.Contains(t, entries[0].ContextMap()["error"], "hallucinated_tool_42")
}
