package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func setToolAllowlist(t *testing.T, fn func(context.Context, *Agent, *models.Chat) map[string]bool) {
	t.Helper()
	prev := toolAllowlistForChat
	t.Cleanup(func() { toolAllowlistForChat = prev })
	toolAllowlistForChat = fn
}

// allowOnly sets an allowlist naming exactly the given tools.
func allowOnly(t *testing.T, names ...string) {
	t.Helper()
	setToolAllowlist(t, func(context.Context, *Agent, *models.Chat) map[string]bool {
		m := make(map[string]bool, len(names))
		for _, n := range names {
			m[n] = true
		}
		return m
	})
}

// interactiveAdvancedTurn is a chat turn that would offer both turn-only tools: update_plan (an
// Advanced turn) and ask_user (a turn someone can answer).
func interactiveAdvancedTurn(toolsEnabled bool) *chatContext {
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), ToolsEnabled: toolsEnabled}
	return &chatContext{
		chat:         chat,
		userID:       chat.UserID,
		promptJob:    &models.Job{ID: uuid.New(), JobType: JobTypeChatMessage},
		turnSettings: models.TurnSettings{AgentLoop: models.AgentLoopAdvanced},
	}
}

// With an allowlist, the turn offers exactly the listed tools: no other catalog tool, no
// ask_user, and no vendor-native web search.
func TestToolAllowlist_OffersOnlyTheListedTools(t *testing.T) {
	allowOnly(t, agenttools.RecallToolSpec.Name, agenttools.UpdatePlanToolSpec.Name)
	a := &Agent{logger: zap.NewNop()}
	chatCtx := interactiveAdvancedTurn(true)

	policy := a.buildTurnToolPolicy(context.Background(), chatCtx, chatCtx.userID, &models.ChatMessage{})
	require.True(t, policy.toolsEnabled)
	assert.True(t, policy.disabledTools[agenttools.ToolNameWebSearch])
	assert.False(t, policy.nativeWebSearch, "vendor-native web search follows web_search")
	assert.False(t, policy.firstPartyWebSearch)
	assert.False(t, policy.offersMCP(), "no MCP tool is listed, so connectors are not contacted")
	assert.False(t, policy.toolAllowed(agenttools.LoadToolsToolSpec.Name))
	assert.NotNil(t, chatCtx.toolAllowlist, "the resolved allowlist is kept for the rest of the turn")

	specs := a.turnFunctionToolSpecs(context.Background(), chatCtx, chatCtx.userID, chatCtx.chat.ID, policy)
	names := map[string]struct{}{}
	for _, s := range specs {
		names[s.Name] = struct{}{}
	}
	want := []string{agenttools.RecallToolSpec.Name, agenttools.UpdatePlanToolSpec.Name}
	assert.Equal(t, want, sortedNames(names), "the specs registered with the adapter")
	assert.Equal(t, want, sortedNames(chatCtx.offeredTools), "the offered set dispatch enforces")
	assert.True(t, policy.disabledTools[agenttools.AskUserToolSpec.Name], "a left-out turn-only tool is disabled for the adapters too")

	_, _, err := a.dispatchToolUse(context.Background(), chatCtx, provider.ToolUse{ID: "a1", Name: agenttools.AskUserToolSpec.Name, Input: json.RawMessage(`{}`)})
	require.ErrorContains(t, err, "not available", "dispatch refuses a tool the allowlist leaves out")
}

// The Responses path assembles the turn-only and MCP specs with its own code: it too offers
// exactly the listed tools, in the request and in the offered set.
func TestToolAllowlist_ResponsesPathOffersOnlyTheListedTools(t *testing.T) {
	allowOnly(t, agenttools.RecallToolSpec.Name, agenttools.UpdatePlanToolSpec.Name)
	a := &Agent{logger: zap.NewNop()}
	chatCtx := interactiveAdvancedTurn(true)
	chatCtx.model = "gpt-5.1"

	params := a.openAIResponseParamsForChat(context.Background(), chatCtx, chatCtx.userID, &models.ChatMessage{ChatID: chatCtx.chat.ID}, &provider.ModelContext{})
	var sent []string
	for _, tool := range params.Tools {
		if tool.OfFunction == nil {
			sent = append(sent, "non-function tool")
			continue
		}
		sent = append(sent, tool.OfFunction.Name)
	}
	want := []string{agenttools.RecallToolSpec.Name, agenttools.UpdatePlanToolSpec.Name}
	assert.ElementsMatch(t, want, sent, "params.Tools: no ask_user, no native web search, no MCP")
	assert.Equal(t, want, sortedNames(chatCtx.offeredTools))
}

// The mock path records its offered set with its own code too.
func TestToolAllowlist_MockPathOffersOnlyTheListedTools(t *testing.T) {
	allowOnly(t, agenttools.RecallToolSpec.Name, agenttools.UpdatePlanToolSpec.Name)
	a := &Agent{logger: zap.NewNop()}
	chatCtx := interactiveAdvancedTurn(true)

	a.prepareMockTurnTools(context.Background(), chatCtx, chatCtx.userID, &models.ChatMessage{ChatID: chatCtx.chat.ID})
	assert.Equal(t, []string{agenttools.RecallToolSpec.Name, agenttools.UpdatePlanToolSpec.Name}, sortedNames(chatCtx.offeredTools))
}

// The allowlist also holds when the chat's tools are off (the policy's early return), where the
// turn-only tools would otherwise still be registered.
func TestToolAllowlist_AppliesWhenToolsAreOff(t *testing.T) {
	allowOnly(t, agenttools.RecallToolSpec.Name)
	a := &Agent{logger: zap.NewNop()}
	chatCtx := interactiveAdvancedTurn(false)

	policy := a.buildTurnToolPolicy(context.Background(), chatCtx, chatCtx.userID, &models.ChatMessage{})
	require.False(t, policy.toolsEnabled)

	a.turnFunctionToolSpecs(context.Background(), chatCtx, chatCtx.userID, chatCtx.chat.ID, policy)
	assert.Equal(t, []string{agenttools.RecallToolSpec.Name}, sortedNames(chatCtx.offeredTools))
	assert.True(t, policy.disabledTools[agenttools.AskUserToolSpec.Name])
	assert.True(t, policy.disabledTools[agenttools.UpdatePlanToolSpec.Name])
}

// The allowlist only narrows: it cannot bring back a tool the chat's own disabled_tools turn off.
func TestToolAllowlist_DoesNotOverrideTheChatsDisabledTools(t *testing.T) {
	allowOnly(t, agenttools.RecallToolSpec.Name, agenttools.CreateMemoryToolSpec.Name)
	a := &Agent{logger: zap.NewNop()}
	chatCtx := interactiveAdvancedTurn(true)
	chatCtx.chat.DisabledTools = []string{agenttools.CreateMemoryToolSpec.Name}

	policy := a.buildTurnToolPolicy(context.Background(), chatCtx, chatCtx.userID, &models.ChatMessage{})
	a.turnFunctionToolSpecs(context.Background(), chatCtx, chatCtx.userID, chatCtx.chat.ID, policy)
	assert.Equal(t, []string{agenttools.RecallToolSpec.Name}, sortedNames(chatCtx.offeredTools))
}

// Unset (the default) or returning nil, the seam changes nothing.
func TestToolAllowlist_NilMeansNoRestriction(t *testing.T) {
	for name, fn := range map[string]func(context.Context, *Agent, *models.Chat) map[string]bool{
		"unset":      nil,
		"nil result": func(context.Context, *Agent, *models.Chat) map[string]bool { return nil },
	} {
		t.Run(name, func(t *testing.T) {
			setToolAllowlist(t, fn)
			a := &Agent{logger: zap.NewNop()}
			chatCtx := interactiveAdvancedTurn(true)
			policy := a.buildTurnToolPolicy(context.Background(), chatCtx, chatCtx.userID, &models.ChatMessage{})
			assert.Nil(t, policy.allowedTools)
			assert.Nil(t, chatCtx.toolAllowlist)
			assert.False(t, policy.disabledTools[agenttools.AskUserToolSpec.Name])
			assert.True(t, policy.offersMCP())
			assert.True(t, policy.toolAllowed(agenttools.LoadToolsToolSpec.Name))
			assert.True(t, chatCtx.toolAllowedThisTurn(agenttools.UpdatePlanToolSpec.Name))

			specs := []agenttools.FunctionToolSpec{agenttools.AskUserToolSpec, {Name: "mcp__x__y"}}
			assert.Equal(t, specs, policy.restrictToAllowlist(specs))
		})
	}
}

// An MCP tool is admitted only by its full name, and naming one turns MCP discovery on.
func TestToolAllowlist_MCPToolsByName(t *testing.T) {
	allowOnly(t, "mcp__jira__get_issue")
	a := &Agent{logger: zap.NewNop()}
	chatCtx := interactiveAdvancedTurn(true)
	policy := a.buildTurnToolPolicy(context.Background(), chatCtx, chatCtx.userID, &models.ChatMessage{})
	require.True(t, policy.offersMCP())

	got := policy.restrictToAllowlist([]agenttools.FunctionToolSpec{{Name: "mcp__jira__get_issue"}, {Name: "mcp__jira__delete_issue"}})
	require.Len(t, got, 1)
	assert.Equal(t, "mcp__jira__get_issue", got[0].Name)
	assert.True(t, policy.disabledTools["mcp__jira__delete_issue"], "a left-out MCP tool is disabled for the adapters too")
}

// countingMCPServer is newTestMCPRPCServer with a count of the requests it receives.
func countingMCPServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	inner := newTestMCPRPCServer()
	t.Cleanup(inner.Close)
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		inner.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

// With an allowlist that names no MCP tool, list and load_tools (allowed themselves) see no
// connector in scope, and no connector is contacted. Naming an MCP tool brings them back.
func TestToolAllowlist_NoMCPToolMeansNoConnectorInScope(t *testing.T) {
	ctx := context.Background()
	srv, hits := countingMCPServer(t)
	agent, chatCtx, serverID, cleanup := newMCPLifecycleAgentFixture(t, srv.URL)
	defer cleanup()
	agent.listTool = agenttools.NewListTool(agent.ds, zap.NewNop())
	agent.listTool.SetMCPDiscoverer(agent.mcpClient)
	chatCtx.chat.ToolsEnabled = true
	msg := &models.ChatMessage{ChatID: chatCtx.chat.ID}

	allowOnly(t, agenttools.ListToolSpec.Name, agenttools.LoadToolsToolSpec.Name)
	require.False(t, agent.buildTurnToolPolicy(ctx, chatCtx, chatCtx.userID, msg).offersMCP())

	listOut, _, err := agent.toolHandlers(chatCtx)[agenttools.ListToolSpec.Name](ctx, []byte(`{"kind":"mcp_servers"}`))
	require.NoError(t, err)
	assert.NotContains(t, listOut, serverID.String(), "list(kind=\"mcp_servers\") shows no connector")
	loadRaw, _ := json.Marshal(map[string]any{"mcp_server_id": serverID.String(), "tools": []string{"search"}})
	out, err := agent.loadMCPToolsTool(ctx, chatCtx, loadRaw)
	require.NoError(t, err)
	assert.Contains(t, out, "not available in this chat", "load_tools finds no connector")
	assert.Zero(t, hits.Load(), "no connector was contacted")

	// Control: an allowlist that names an MCP tool puts the thread's connector back in scope.
	allowOnly(t, agenttools.ListToolSpec.Name, "mcp__tracker__search")
	agent.buildTurnToolPolicy(ctx, chatCtx, chatCtx.userID, msg)
	listOut, _, err = agent.toolHandlers(chatCtx)[agenttools.ListToolSpec.Name](ctx, []byte(`{"kind":"mcp_servers"}`))
	require.NoError(t, err)
	assert.Contains(t, listOut, serverID.String())
	assert.NotZero(t, hits.Load())
}

// A sub-agent inherits the turn's allowlist: it is offered only the listed MCP tools of its skills'
// connectors, and none (without contacting them) when no MCP tool is listed.
func TestToolAllowlist_SubagentMCPTools(t *testing.T) {
	ctx := context.Background()
	srv, hits := countingMCPServer(t)
	agent, chatCtx, _, cleanup := newMCPLifecycleAgentFixture(t, srv.URL)
	defer cleanup()
	_, skillID := newSkillOnlyConnector(t, agent, chatCtx, srv.URL)
	skills := []uuid.UUID{skillID}

	all, _ := agent.getSubagentMCPFunctionToolSpecs(ctx, chatCtx.userID, skills)
	require.Len(t, all, 2, "with no allowlist the sub-agent gets every tool of the skill's connector")
	keep := all[0].Name

	listed := withSubagentToolAllowlist(ctx, map[string]bool{agenttools.RunSubagentToolSpec.Name: true, keep: true})
	got, _ := agent.getSubagentMCPFunctionToolSpecs(listed, chatCtx.userID, skills)
	assert.Equal(t, []string{keep}, mcpToolNames(got))
	assert.Len(t, agent.getSubagentMCPTools(listed, chatCtx.userID, skills, ""), 1, "the Responses sub-agent path filters too")

	before := hits.Load()
	none := withSubagentToolAllowlist(ctx, map[string]bool{agenttools.RunSubagentToolSpec.Name: true})
	got, servers := agent.getSubagentMCPFunctionToolSpecs(none, chatCtx.userID, skills)
	assert.Empty(t, got)
	assert.Empty(t, servers)
	assert.Equal(t, before, hits.Load(), "no connector was contacted")
}

// The developer context does not point the model at load_skill or load_tools when the allowlist
// leaves them out.
func TestToolAllowlist_DeveloperContextFollowsTheList(t *testing.T) {
	render := func(t *testing.T) string {
		t.Helper()
		chatCtx := makeChatContext("gpt-5.1")
		chatCtx.chat.ToolsEnabled = true
		chatCtx.skills = &turnSkills{inScope: []*models.Ritual{{ID: uuid.New(), Name: "Research", Description: "deep research"}}}
		a := testAgentWithHistoryOverride(nil)
		current := &models.ChatMessage{ID: uuid.New(), ChatID: chatCtx.chat.ID, Message: "hi", Origin: models.MessageOriginUser}
		modelCtx, err := a.buildModelContextForChatMessage(context.Background(), chatCtx.chat.UserID, current, chatCtx, nil)
		require.NoError(t, err)
		var all []string
		for _, item := range provider.RenderOpenAIInputItems(modelCtx) {
			all = append(all, itemMessageText(item))
		}
		return strings.Join(all, "\n")
	}

	allowOnly(t, agenttools.LoadSkillToolSpec.Name, agenttools.LoadToolsToolSpec.Name)
	got := render(t)
	require.Contains(t, got, "Skills available")
	require.Contains(t, got, "Tool lifecycle")

	allowOnly(t, agenttools.RecallToolSpec.Name)
	got = render(t)
	assert.NotContains(t, got, "Skills available", "no skills index without load_skill")
	assert.NotContains(t, got, "Tool lifecycle", "no lifecycle note without load_tools")
	assert.NotContains(t, got, agenttools.LoadToolsToolSpec.Name)
}

// The Advanced loop's checklist paragraph names update_plan, so it is left out without it.
func TestAdvancedLoopInstructions_FollowTheAllowlist(t *testing.T) {
	assert.Contains(t, advancedLoopInstructions(true), agenttools.UpdatePlanToolSpec.Name)
	assert.NotContains(t, advancedLoopInstructions(false), agenttools.UpdatePlanToolSpec.Name)
	assert.Contains(t, advancedLoopInstructions(false), "extended mode")

	chatCtx := &chatContext{toolAllowlist: map[string]bool{agenttools.RecallToolSpec.Name: true}}
	assert.False(t, chatCtx.toolAllowedThisTurn(agenttools.UpdatePlanToolSpec.Name))
}
