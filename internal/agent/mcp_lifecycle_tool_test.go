package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
)

func TestMCPToolLifecycleArgsLenientDecoding(t *testing.T) {
	cases := map[string]struct {
		in       string
		wantID   string
		wantList []string
	}{
		"declared shape":      {`{"mcp_server_id":"abc","tools":["a","b"]}`, "abc", []string{"a", "b"}},
		"json-encoded array":  {`{"mcp_server_id":"abc","tools":"[\"a\",\"b\"]"}`, "abc", []string{"a", "b"}},
		"comma string":        {`{"mcp_server_id":"abc","tools":"a, b"}`, "abc", []string{"a", "b"}},
		"single string":       {`{"mcp_server_id":"abc","tools":"all"}`, "abc", []string{"all"}},
		"aliases":             {`{"server_id":"abc","tool_names":["a"]}`, "abc", []string{"a"}},
		"empty id falls back": {`{"mcp_server_id":"","connector_id":"abc","tools":["a"]}`, "abc", []string{"a"}},
		"missing everything":  {`{}`, "", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var args mcpToolLifecycleArgs
			require.NoError(t, json.Unmarshal([]byte(tc.in), &args))
			require.Equal(t, tc.wantID, args.MCPServerID)
			require.Equal(t, tc.wantList, args.Tools)
		})
	}

	var args mcpToolLifecycleArgs
	require.Error(t, json.Unmarshal([]byte(`{"tools":{"a":1}}`), &args))
}

func TestNormalizeRequestedTools(t *testing.T) {
	got := normalizeRequestedTools([]string{"  ", "ALL", "*", "foo", "foo", "Bar"})
	require.True(t, got.all)
	require.Equal(t, []string{"foo", "Bar"}, got.names)
}

func TestBuildConnectorToolCatalogAndResolveNames(t *testing.T) {
	connA := uuid.New()
	connB := uuid.New()
	tools := []mcpclient.ConnectorTool{
		{ConnectorID: connA, Name: "Search", FullName: "mcp__a__search"},
		{ConnectorID: connA, Name: "Get", FullName: "mcp__a__get"},
		{ConnectorID: connB, Name: "Other", FullName: "mcp__b__other"},
		{ConnectorID: connA, Name: "", FullName: " "}, // ignored
	}

	catalog := buildConnectorToolCatalog(tools, connA)
	require.Len(t, catalog.byFullName, 2)
	require.Equal(t, []string{"mcp__a__get", "mcp__a__search"}, catalog.allFullNames)

	got, unknown := resolveRequestedToolNames(catalog, requestedTools{
		names: []string{"Search", "mcp__a__get", "missing", "search"},
	})
	require.Equal(t, []string{"mcp__a__get", "mcp__a__search"}, got)
	require.Equal(t, []string{"missing"}, unknown)

	all, unknown := resolveRequestedToolNames(catalog, requestedTools{all: true})
	require.Equal(t, []string{"mcp__a__get", "mcp__a__search"}, all)
	require.Nil(t, unknown)
}

func TestSliceSetAndLoadedMapHelpers(t *testing.T) {
	set := sliceToSet([]string{" a ", "", "a", "b"})
	require.Len(t, set, 2)
	require.Contains(t, set, "a")
	require.Contains(t, set, "b")

	sorted := setToSortedSlice(set)
	require.Equal(t, []string{"a", "b"}, sorted)
	require.Nil(t, setToSortedSlice(map[string]struct{}{}))

	sid := uuid.New()
	src := map[uuid.UUID][]string{sid: {"x"}}
	withUpdate := loadedByServerWithUpdate(src, sid, []string{"y"})
	require.Equal(t, []string{"y"}, withUpdate[sid])
	require.Equal(t, []string{"x"}, src[sid], "source map must be unchanged")

	without := loadedByServerWithUpdate(src, sid, nil)
	_, ok := without[sid]
	require.False(t, ok)
}

func TestMarshalMCPToolLifecycleResult(t *testing.T) {
	out, err := marshalMCPToolLifecycleResult(mcpToolLifecycleResult{
		ServerID: "s",
		Loaded:   []string{"mcp__a__search"},
	})
	require.NoError(t, err)
	var decoded mcpToolLifecycleResult
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	require.Equal(t, "s", decoded.ServerID)
}

func TestLoadUnloadMCPToolsTool_RequiresChatContext(t *testing.T) {
	a := &Agent{}

	out, err := a.loadMCPToolsTool(context.Background(), nil, []byte(`{}`))
	require.NoError(t, err)
	require.Contains(t, out, "requires an active chat context")

	out, err = a.unloadMCPToolsTool(context.Background(), nil, []byte(`{}`))
	require.NoError(t, err)
	require.Contains(t, out, "requires an active chat context")
}
