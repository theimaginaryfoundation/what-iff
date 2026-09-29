package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestGetSubagentMCPTools_ReturnsFunctionToolsFromDiscovery(t *testing.T) {
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
					"tools": []map[string]any{
						{
							"name":        "find-ticket",
							"description": "Find a ticket",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	a := &Agent{mcpClient: mcpclient.New(nil, nil)}
	server := &models.MCPServer{
		ID:        uuid.New(),
		Name:      "tracker",
		ServerURL: srv.URL,
		Status:    models.MCPServerStatusActive,
	}
	tools := a.discoverMCPFunctionToolSpecs(context.Background(), uuid.New(), []*models.MCPServer{server})
	require.Len(t, tools, 1)
	require.Contains(t, tools[0].Name, "mcp__")
}

func TestDiscoveryFailureStatus(t *testing.T) {
	server := &models.MCPServer{Status: models.MCPServerStatusActive}

	require.Equal(t, models.MCPServerStatusInvalid, discoveryFailureStatus(server, "mcp server returned status 401 for tools/list"))
	require.Equal(t, models.MCPServerStatusInvalid, discoveryFailureStatus(server, "oauth connector is not authenticated"))
	require.Equal(t, models.MCPServerStatusRefreshError, discoveryFailureStatus(server, "mcp server returned status 503 for tools/list"))
	require.Equal(t, models.MCPServerStatusRefreshError, discoveryFailureStatus(server, "Post https://example.com: context deadline exceeded"))

	disabled := &models.MCPServer{Status: models.MCPServerStatusDisabled}
	require.Equal(t, models.MCPServerStatusDisabled, discoveryFailureStatus(disabled, `connector status "disabled" not eligible`))
}

func TestFilterMCPToolSpecsByLoaded_RequiresExplicitLoadedSet(t *testing.T) {
	specs := []agenttools.FunctionToolSpec{
		{Name: "mcp__a__search"},
		{Name: "mcp__a__get"},
	}
	// No loaded state => no MCP tool exposure (explicit opt-in semantics).
	require.Nil(t, filterMCPToolSpecsByLoaded(specs, nil))
	require.Nil(t, filterMCPToolSpecsByLoaded(specs, map[uuid.UUID][]string{}))

	serverID := uuid.New()
	out := filterMCPToolSpecsByLoaded(specs, map[uuid.UUID][]string{
		serverID: {"mcp__a__get"},
	})
	require.Len(t, out, 1)
	require.Equal(t, "mcp__a__get", out[0].Name)
}
