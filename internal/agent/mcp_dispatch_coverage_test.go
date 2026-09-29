package agent

import (
	"context"
	"encoding/json"
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
