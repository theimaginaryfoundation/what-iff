package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestMCPToolIsLoaded(t *testing.T) {
	serverID := uuid.New()
	chatCtx := &chatContext{
		mcpServers: []*models.MCPServer{
			{ID: serverID},
			nil,
			{ID: uuid.Nil},
		},
		loadedMCPTools: map[uuid.UUID]map[string]struct{}{
			serverID: {"mcp__a__search": {}},
		},
	}

	require.True(t, mcpToolIsLoaded(chatCtx, "mcp__a__search"))
	require.False(t, mcpToolIsLoaded(chatCtx, "mcp__a__missing"))
	require.False(t, mcpToolIsLoaded(chatCtx, "   "))
	require.False(t, mcpToolIsLoaded(nil, "mcp__a__search"))
}

func TestDispatchMCPToolUse_Guards(t *testing.T) {
	a := &Agent{}
	use := provider.ToolUse{Name: "mcp__a__search", Input: json.RawMessage(`{}`)}
	out, files, err := a.dispatchMCPToolUse(context.Background(), nil, use)
	require.ErrorContains(t, err, "mcp client is not configured")
	require.Empty(t, out)
	require.Nil(t, files)

	// mcp client configured, but missing chat context
	ds, _, cleanup := newTestDatastore(t)
	defer cleanup()
	a = &Agent{ds: ds, mcpClient: mcpclient.New(nil, nil)}
	out, files, err = a.dispatchMCPToolUse(context.Background(), nil, use)
	require.ErrorContains(t, err, "active chat context")
	require.Empty(t, out)
	require.Nil(t, files)
}

func TestDispatchMCPToolUse_ReturnsGeneratedAttachments(t *testing.T) {
	const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	serverID := uuid.New()
	prefix := strings.ReplaceAll(serverID.String(), "-", "")
	fullToolName := "mcp__" + prefix[:8] + "__render_graph"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		switch method {
		case "initialize", "notifications/initialized":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"tools": []map[string]any{{"name": "render_graph", "inputSchema": map[string]any{"type": "object"}}},
				},
			})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": "attached graph"},
						{"type": "image", "data": tinyPNG, "mimeType": "image/png"},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	a := &Agent{mcpClient: mcpclient.New(nil, nil)}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New()}
	chatCtx := &chatContext{
		userID: chat.UserID,
		chat:   chat,
		mcpServers: []*models.MCPServer{
			{ID: serverID, Name: "grafana", ServerURL: srv.URL, Status: models.MCPServerStatusActive},
		},
		loadedMCPTools: map[uuid.UUID]map[string]struct{}{
			serverID: {fullToolName: {}},
		},
	}
	out, atts, err := a.dispatchMCPToolUse(context.Background(), chatCtx, provider.ToolUse{
		ID:    "mcp-1",
		Name:  fullToolName,
		Input: json.RawMessage(`{"metric":"cpu"}`),
	})
	require.NoError(t, err)
	require.Equal(t, "attached graph", out)
	require.Len(t, atts, 1)
	require.Equal(t, "image/png", atts[0].FileType)
	require.Equal(t, tinyPNG, atts[0].FileContent)
}

func TestExecuteToolUses_MCPImagesBecomeInTurnVisionPayloads(t *testing.T) {
	const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	serverID := uuid.New()
	prefix := strings.ReplaceAll(serverID.String(), "-", "")
	fullToolName := "mcp__" + prefix[:8] + "__render_graph"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		switch method {
		case "initialize", "notifications/initialized":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"tools": []map[string]any{{"name": "render_graph", "inputSchema": map[string]any{"type": "object"}}},
				},
			})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"content": []map[string]any{
						{"type": "image", "data": tinyPNG, "mimeType": "image/png"},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	a := &Agent{mcpClient: mcpclient.New(nil, nil)}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New()}
	chatCtx := &chatContext{
		userID: chat.UserID,
		chat:   chat,
		mcpServers: []*models.MCPServer{
			{ID: serverID, Name: "grafana", ServerURL: srv.URL, Status: models.MCPServerStatusActive},
		},
		loadedMCPTools: map[uuid.UUID]map[string]struct{}{
			serverID: {fullToolName: {}},
		},
	}
	results, _, atts := a.executeToolUses(context.Background(), chatCtx, 0, []provider.ToolUse{{
		ID:    "mcp-img",
		Name:  fullToolName,
		Input: json.RawMessage(`{"metric":"cpu"}`),
	}})
	require.Len(t, atts, 1)
	require.Len(t, results, 1)
	require.False(t, results[0].IsErr)
	require.Len(t, results[0].Images, 1)
	require.Equal(t, "image/png", results[0].Images[0].MediaType)
	require.NotEmpty(t, results[0].Images[0].RawBytes)
}
