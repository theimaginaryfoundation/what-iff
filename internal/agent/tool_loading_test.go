package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"go.uber.org/zap"
)

// scopingAdapter is a scriptedAdapter that records what each round would hide.
type scopingAdapter struct {
	scriptedAdapter
	probe  []string
	hidden []map[string]bool
}

func (s *scopingAdapter) ScopeTools(hidden func(name string) bool) {
	round := map[string]bool{}
	for _, name := range s.probe {
		round[name] = hidden(name)
	}
	s.hidden = append(s.hidden, round)
}

func gatedOffered(names ...string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, n := range names {
		out[n] = struct{}{}
	}
	return out
}

func TestCatalogMarksHeavyBuiltinsLoadable(t *testing.T) {
	loadable := agenttools.LoadableFunctionToolNames()
	for _, name := range []string{
		agenttools.WebSearchFunctionToolSpec.Name,
		agenttools.FetchPageToolSpec.Name,
		agenttools.GenerateImageToolSpec.Name,
		agenttools.RunSubagentToolSpec.Name,
		agenttools.CreateAgentJobToolSpec.Name,
		agenttools.UpdateAgentJobToolSpec.Name,
	} {
		require.Contains(t, loadable, name)
		require.NotEmpty(t, loadable[name], "%s needs a summary for the developer context", name)
	}
	// The lifecycle tools themselves, and everyday tools, are always sent.
	for _, name := range []string{agenttools.LoadToolsToolSpec.Name, agenttools.UnloadToolsToolSpec.Name, agenttools.RecallToolSpec.Name, agenttools.ListToolSpec.Name} {
		require.NotContains(t, loadable, name)
	}
}

func TestLoadedToolFilter(t *testing.T) {
	require.Nil(t, (&chatContext{}).loadedToolFilter(), "a context that is not load-gated hides nothing")

	c := &chatContext{
		toolsLoadGated: true,
		loadableTools:  gatedOffered("web_search", "generate_image"),
		loadedTools:    gatedOffered("generate_image"),
	}
	hidden := c.loadedToolFilter()
	require.True(t, hidden("web_search"))
	require.False(t, hidden("generate_image"))
	require.False(t, hidden("recall"), "tools that are not loadable are always sent")
	require.True(t, hidden("mcp__tracker__search"), "an MCP tool that is not loaded is hidden")

	specs := []agenttools.FunctionToolSpec{{Name: "recall"}, {Name: "web_search"}, {Name: "generate_image"}}
	var visible []string
	for _, s := range c.visibleToolSpecs(specs) {
		visible = append(visible, s.Name)
	}
	require.Equal(t, []string{"recall", "generate_image"}, visible)
}

func TestLoadableToolsDeveloperContext(t *testing.T) {
	require.Empty(t, loadableToolsDeveloperContext(gatedOffered("recall"), nil))
	got := loadableToolsDeveloperContext(gatedOffered("recall", "web_search", "generate_image"), nil)
	require.Contains(t, got, "load_tools")
	require.Contains(t, got, "- generate_image: ")
	require.Contains(t, got, "- web_search: ")
	require.NotContains(t, got, "Already loaded")
	require.NotContains(t, got, "run_subagent", "only the tools the turn offers are listed")
	require.Less(t, strings.Index(got, "generate_image"), strings.Index(got, "web_search"), "listed in a stable order")

	// Once loaded, a tool is reported as in the toolset, not as needing a load.
	got = loadableToolsDeveloperContext(gatedOffered("web_search", "generate_image"), gatedOffered("generate_image"))
	require.Contains(t, got, "Already loaded in this chat, so they are in your toolset now (call them directly): generate_image.")
	require.NotContains(t, got, "- generate_image:")
	require.Contains(t, got, "- web_search: ")

	got = loadableToolsDeveloperContext(gatedOffered("generate_image"), gatedOffered("generate_image"))
	require.NotContains(t, got, "Not loaded yet")
}

func TestDispatchRefusesUnloadedBuiltinTool(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	c := &chatContext{
		toolsLoadGated: true,
		loadableTools:  gatedOffered("web_search"),
		loadedTools:    map[string]struct{}{},
	}
	c.setOfferedTools(gatedOffered("web_search"))
	_, _, err := a.dispatchToolUse(context.Background(), c, provider.ToolUse{Name: "web_search", Input: []byte(`{}`)})
	require.Error(t, err)
	require.Contains(t, err.Error(), `call "load_tools"`)
}

func TestBuiltinToolsLoadAndUnloadFlow(t *testing.T) {
	ctx := context.Background()
	srv := newTestMCPRPCServer()
	defer srv.Close()
	a, chatCtx, mcpServerID, cleanup := newMCPLifecycleAgentFixture(t, srv.URL)
	defer cleanup()
	a.prepareTurnLoadableTools(ctx, chatCtx, gatedOffered("web_search", "generate_image", "recall"))
	require.Equal(t, gatedOffered("web_search", "generate_image"), chatCtx.loadableTools)

	out, err := a.loadMCPToolsTool(ctx, chatCtx, []byte(`{"tools":["web_search","recall","nope"]}`))
	require.NoError(t, err)
	var res mcpToolLifecycleResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Equal(t, []string{"web_search"}, res.Loaded)
	require.Contains(t, res.Note, "nope")
	require.Contains(t, res.Note, "recall")
	require.False(t, chatCtx.builtinToolNeedsLoad("web_search"))

	stored, err := a.ds.ListChatLoadedBuiltinTools(ctx, chatCtx.userID, chatCtx.chat.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"web_search"}, stored)
	mcpLoaded, err := a.ds.ListChatMCPLoadedTools(ctx, chatCtx.userID, chatCtx.chat.ID)
	require.NoError(t, err)
	require.Empty(t, mcpLoaded, "the built-in row is not a connector")

	out, err = a.loadMCPToolsTool(ctx, chatCtx, []byte(`{"tools":["web_search"]}`))
	require.NoError(t, err)
	res = mcpToolLifecycleResult{}
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Equal(t, []string{"web_search"}, res.AlreadyLoaded)

	// Load an MCP tool too, then unload both by name without naming the connector.
	loadMCP, _ := json.Marshal(map[string]any{"mcp_server_id": mcpServerID.String(), "tools": []string{"search"}})
	out, err = a.loadMCPToolsTool(ctx, chatCtx, loadMCP)
	require.NoError(t, err)
	res = mcpToolLifecycleResult{}
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Len(t, res.Loaded, 1)
	mcpName := res.Loaded[0]

	unload, _ := json.Marshal(map[string]any{"tools": []string{"web_search", mcpName, "generate_image"}})
	out, err = a.unloadMCPToolsTool(ctx, chatCtx, unload)
	require.NoError(t, err)
	res = mcpToolLifecycleResult{}
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.ElementsMatch(t, []string{"web_search", mcpName}, res.Unloaded)
	require.Equal(t, []string{"generate_image"}, res.NotLoaded)
	require.True(t, chatCtx.builtinToolNeedsLoad("web_search"))
	require.False(t, mcpToolIsLoaded(chatCtx, mcpName))
}

func TestBuiltinToolsUnloadAllClearsBuiltinRow(t *testing.T) {
	ctx := context.Background()
	srv := newTestMCPRPCServer()
	defer srv.Close()
	a, chatCtx, _, cleanup := newMCPLifecycleAgentFixture(t, srv.URL)
	defer cleanup()
	a.prepareTurnLoadableTools(ctx, chatCtx, gatedOffered("web_search", "generate_image"))

	_, err := a.loadMCPToolsTool(ctx, chatCtx, []byte(`{"tools":["all"]}`))
	require.NoError(t, err)
	require.Equal(t, gatedOffered("web_search", "generate_image"), chatCtx.loadedTools)

	_, err = a.unloadMCPToolsTool(ctx, chatCtx, []byte(`{"tools":["*"]}`))
	require.NoError(t, err)
	stored, err := a.ds.ListChatLoadedBuiltinTools(ctx, chatCtx.userID, chatCtx.chat.ID)
	require.NoError(t, err)
	require.Empty(t, stored)
	require.True(t, chatCtx.builtinToolNeedsLoad("web_search"))
}

// A tool loaded in one round is sent from the next round of the same turn.
func TestHandleAgentLoop_ToolLoadedMidTurnIsSentNextRound(t *testing.T) {
	ctx := context.Background()
	srv := newTestMCPRPCServer()
	defer srv.Close()
	a, chatCtx, _, cleanup := newMCPLifecycleAgentFixture(t, srv.URL)
	defer cleanup()
	offered := gatedOffered("web_search", agenttools.LoadToolsToolSpec.Name)
	chatCtx.setOfferedTools(offered)
	a.prepareTurnLoadableTools(ctx, chatCtx, offered)

	adapter := &scopingAdapter{probe: []string{"web_search", agenttools.LoadToolsToolSpec.Name}}
	adapter.callFn = func(step int) (*provider.GenerateResponse, []provider.ToolUse, error) {
		if step == 0 {
			return nil, []provider.ToolUse{{ID: "t1", Name: agenttools.LoadToolsToolSpec.Name, Input: []byte(`{"tools":["web_search"]}`)}}, nil
		}
		return &provider.GenerateResponse{Text: "done"}, nil, nil
	}
	_, toolCalls, _, err := a.handleAgentLoop(ctx, chatCtx, adapter)
	require.NoError(t, err)
	require.Len(t, toolCalls, 1)
	require.Empty(t, toolCalls[0].ToolError)
	require.Equal(t, []map[string]bool{
		{"web_search": true, "load_tools": false},
		{"web_search": false, "load_tools": false},
	}, adapter.hidden)
}

// A sub-agent loop is not load-gated: its adapter is never scoped.
func TestHandleAgentLoop_UngatedContextIsNotScoped(t *testing.T) {
	adapter := &scopingAdapter{probe: []string{"mcp__x__y"}}
	adapter.callFn = func(int) (*provider.GenerateResponse, []provider.ToolUse, error) {
		return &provider.GenerateResponse{Text: "done"}, nil, nil
	}
	a := &Agent{logger: zap.NewNop()}
	_, _, _, err := a.handleAgentLoop(context.Background(), &chatContext{}, adapter)
	require.NoError(t, err)
	require.Empty(t, adapter.hidden)
}

// A built-in tool loaded in one turn is still loaded, and sent, in the chat's next turn.
func TestBuiltinToolStaysLoadedNextTurn(t *testing.T) {
	ctx := context.Background()
	srv := newTestMCPRPCServer()
	defer srv.Close()
	a, turn1, _, cleanup := newMCPLifecycleAgentFixture(t, srv.URL)
	defer cleanup()
	offered := gatedOffered("generate_image", agenttools.LoadToolsToolSpec.Name)
	a.prepareTurnLoadableTools(ctx, turn1, offered)
	_, err := a.loadMCPToolsTool(ctx, turn1, []byte(`{"tools":["generate_image"]}`))
	require.NoError(t, err)

	turn2 := &chatContext{userID: turn1.userID, chat: turn1.chat}
	a.prepareTurnLoadableTools(ctx, turn2, offered)
	require.False(t, turn2.loadedToolFilter()("generate_image"), "loaded state carries into the next turn")
}
