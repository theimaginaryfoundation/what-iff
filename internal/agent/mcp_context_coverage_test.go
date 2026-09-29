package agent

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestSetMCPServerCacheBuildsLoadedSet(t *testing.T) {
	serverID := uuid.New()
	other := uuid.New()
	chatCtx := &chatContext{}

	chatCtx.setMCPServerCache(
		[]*models.MCPServer{{ID: serverID}},
		map[uuid.UUID][]string{
			serverID: {" mcp__a__search ", "", "mcp__a__search"},
			other:    {"mcp__b__get"},
		},
	)

	require.Len(t, chatCtx.mcpServers, 1)
	require.Contains(t, chatCtx.loadedMCPTools[serverID], "mcp__a__search")
	require.Contains(t, chatCtx.loadedMCPTools[other], "mcp__b__get")
}

func TestMCPLifecycleDeveloperContextMentionsLifecycle(t *testing.T) {
	ctx := mcpLifecycleDeveloperContext()
	require.Contains(t, ctx, "list(kind=\"mcp_servers\")")
	require.Contains(t, ctx, "load_mcp_tools")
	require.Contains(t, ctx, "unload_mcp_tools")
	require.Contains(t, ctx, "across future turns")
}
