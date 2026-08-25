package agent

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestBuildMCPToolsFromServers_EncodesRequiredFields(t *testing.T) {
	tools := buildMCPToolsFromServers([]*models.MCPServer{
		{
			Name:        "stripe",
			Description: "Stripe MCP server",
			ServerURL:   "https://mcp.stripe.com",
			AuthToken:   "token-123",
		},
	}, true)
	require.Len(t, tools, 1)
	require.NotNil(t, tools[0].OfMcp)

	raw, err := json.Marshal(tools[0].OfMcp)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.Equal(t, "mcp", payload["type"])
	require.Equal(t, "stripe", payload["server_label"])
	require.Equal(t, "Stripe MCP server", payload["server_description"])
	require.Equal(t, "https://mcp.stripe.com", payload["server_url"])
	require.Equal(t, true, payload["defer_loading"])
	require.Equal(t, "never", payload["require_approval"])

	headers, ok := payload["headers"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "token-123", headers["Authorization"])
}

func TestAppendToolSearchIfNeeded(t *testing.T) {
	baseTools := buildMCPToolsFromServers([]*models.MCPServer{
		{
			Name:        "stripe",
			Description: "Stripe MCP server",
			ServerURL:   "https://mcp.stripe.com",
		},
	}, true)
	require.Len(t, baseTools, 1)

	withSearch := appendToolSearchIfNeeded(baseTools, true)
	require.Len(t, withSearch, 2)
	require.NotNil(t, withSearch[1].OfToolSearch)
	require.Equal(t, responses.ToolSearchToolExecutionServer, withSearch[1].OfToolSearch.Execution)

	withoutSearch := appendToolSearchIfNeeded(baseTools, false)
	require.Len(t, withoutSearch, 1)
}

func TestBuildMCPToolsFromServers_EmptyTokenSkipsHeader(t *testing.T) {
	tools := buildMCPToolsFromServers([]*models.MCPServer{
		{
			Name:        "docs",
			Description: "Docs MCP",
			ServerURL:   "https://example.com/mcp",
		},
		{
			Name:         "broken",
			Description:  "Broken MCP",
			ServerURL:    "https://broken.example/mcp",
			ErrorMessage: "Authentication token decryption failed.",
		},
		nil,
	}, false)
	require.Len(t, tools, 1)

	raw, err := json.Marshal(tools[0].OfMcp)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	_, hasHeaders := payload["headers"]
	require.False(t, hasHeaders)
	_, hasDefer := payload["defer_loading"]
	require.False(t, hasDefer)
}

func TestModelSupportsMCPDeferLoading(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{model: "gpt-5", want: false},
		{model: "gpt-5-mini", want: false},
		{model: "gpt-5.3", want: false},
		{model: "gpt-5.4", want: true},
		{model: "gpt-5.4-mini", want: true},
		{model: "gpt-5.5", want: true},
		{model: "gpt-6", want: true},
		{model: "gpt-4o", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			require.Equal(t, tt.want, modelSupportsMCPDeferLoading(tt.model))
		})
	}
}

func TestBuildClaudeMCPConfigFromServers_EncodesRequiredFields(t *testing.T) {
	id := mustUUID(t, "66d20dca-9d4b-45ca-a08f-c11cc6f84f50")
	cfg := buildClaudeMCPConfigFromServers([]*models.MCPServer{
		{
			ID:        id,
			Name:      "stripe",
			ServerURL: "https://mcp.stripe.com",
			AuthToken: "token-123",
		},
	})
	require.NotNil(t, cfg)
	require.Len(t, cfg.Servers, 1)
	require.Len(t, cfg.Toolsets, 1)
	require.Equal(t, "mcp-"+id.String(), cfg.Servers[0].Name)
	require.Equal(t, "https://mcp.stripe.com", cfg.Servers[0].URL)
	require.Equal(t, "token-123", cfg.Servers[0].AuthorizationToken.Or(""))
	require.NotNil(t, cfg.Toolsets[0].GetMCPServerName())
	require.Equal(t, "mcp-"+id.String(), *cfg.Toolsets[0].GetMCPServerName())
}

func TestBuildClaudeMCPConfigFromServers_SkipsUnhealthyAndNil(t *testing.T) {
	cfg := buildClaudeMCPConfigFromServers([]*models.MCPServer{
		nil,
		{
			ID:           mustUUID(t, "3ca8466e-cbbf-47e3-9134-e2fbc7348f72"),
			ServerURL:    "https://bad.example/mcp",
			ErrorMessage: "decrypt failed",
		},
	})
	require.Nil(t, cfg)
}

func mustUUID(t *testing.T, s string) (id uuid.UUID) {
	t.Helper()
	id, err := uuid.Parse(s)
	require.NoError(t, err)
	return id
}
