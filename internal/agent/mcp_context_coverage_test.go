package agent

import (
	"fmt"
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

func TestMCPLifecycleDeveloperContextNamesConnectorsAndTools(t *testing.T) {
	grafana := &models.MCPServer{ID: uuid.New(), Name: "Grafana", Status: models.MCPServerStatusActive}
	oura := &models.MCPServer{ID: uuid.New(), Name: "Oura", Status: models.MCPServerStatusExpiring}

	ctx := formatMCPLifecycleDeveloperContext(
		[]*models.MCPServer{grafana, oura},
		map[uuid.UUID][]string{grafana.ID: {"search_dashboards", "list_datasources"}},
		map[uuid.UUID][]string{grafana.ID: {"mcp__abc__search_dashboards"}},
		nil,
	)

	require.Contains(t, ctx, "load_mcp_tools")
	require.Contains(t, ctx, "unload_mcp_tools")
	require.Contains(t, ctx, "same turn")
	require.Contains(t, ctx, `"Grafana" mcp_server_id=`+grafana.ID.String())
	require.Contains(t, ctx, "tools: list_datasources, search_dashboards")
	require.Contains(t, ctx, "loaded: mcp__abc__search_dashboards")
	require.Contains(t, ctx, `"Oura" mcp_server_id=`+oura.ID.String()+" status=expiring")
	require.Contains(t, ctx, `not discovered yet; call list(kind="mcp_servers")`)
	require.NotContains(t, ctx, "status=active")
}

func TestMCPLifecycleDeveloperContextFlagsDiscoveryFailure(t *testing.T) {
	s := &models.MCPServer{ID: uuid.New(), Name: "Grafana"}
	ctx := formatMCPLifecycleDeveloperContext([]*models.MCPServer{s}, nil, nil, map[uuid.UUID]string{s.ID: "401 unauthorized"})
	require.Contains(t, ctx, "tools: (discovery failed;")
	require.NotContains(t, ctx, "not discovered yet")
}

func TestMCPLifecycleDeveloperContextCapsToolNames(t *testing.T) {
	s := &models.MCPServer{ID: uuid.New(), Name: "Big"}
	names := make([]string, mcpDeveloperContextMaxToolsPerConnector+5)
	for i := range names {
		names[i] = fmt.Sprintf("tool_%03d", i)
	}
	ctx := formatMCPLifecycleDeveloperContext([]*models.MCPServer{s}, map[uuid.UUID][]string{s.ID: names}, nil, nil)
	require.Contains(t, ctx, "(+5 more;")
	require.NotContains(t, ctx, fmt.Sprintf("tool_%03d", mcpDeveloperContextMaxToolsPerConnector))
}
