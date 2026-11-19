package agent

import (
	"context"
	"encoding/json"
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
	setToolAllowlist(t, func(context.Context, *Agent, *models.Chat) map[string]bool {
		return map[string]bool{
			agenttools.RecallToolSpec.Name:     true,
			agenttools.UpdatePlanToolSpec.Name: true,
		}
	})
	a := &Agent{logger: zap.NewNop()}
	chatCtx := interactiveAdvancedTurn(true)

	policy := a.buildTurnToolPolicy(context.Background(), chatCtx, chatCtx.userID, &models.ChatMessage{})
	require.True(t, policy.toolsEnabled)
	assert.True(t, policy.disabledTools[agenttools.AskUserToolSpec.Name], "ask_user is disabled by name")
	assert.True(t, policy.disabledTools[agenttools.ToolNameWebSearch])
	assert.False(t, policy.nativeWebSearch, "vendor-native web search follows web_search")
	assert.False(t, policy.firstPartyWebSearch)
	assert.False(t, policy.offersMCP(), "no MCP tool is listed, so connectors are not contacted")
	assert.False(t, policy.toolAllowed(agenttools.LoadToolsToolSpec.Name))

	specs := a.turnFunctionToolSpecs(context.Background(), chatCtx, chatCtx.userID, chatCtx.chat.ID, policy)
	names := map[string]struct{}{}
	for _, s := range specs {
		names[s.Name] = struct{}{}
	}
	want := []string{agenttools.RecallToolSpec.Name, agenttools.UpdatePlanToolSpec.Name}
	assert.Equal(t, want, sortedNames(names), "the specs registered with the adapter")
	assert.Equal(t, want, sortedNames(chatCtx.offeredTools), "the offered set dispatch enforces")

	_, _, err := a.dispatchToolUse(context.Background(), chatCtx, provider.ToolUse{ID: "a1", Name: agenttools.AskUserToolSpec.Name, Input: json.RawMessage(`{}`)})
	require.ErrorContains(t, err, "not available", "dispatch refuses a tool the allowlist leaves out")
}

// The allowlist also holds when the chat's tools are off (the policy's early return), where the
// turn-only tools would otherwise still be registered.
func TestToolAllowlist_AppliesWhenToolsAreOff(t *testing.T) {
	setToolAllowlist(t, func(context.Context, *Agent, *models.Chat) map[string]bool {
		return map[string]bool{agenttools.RecallToolSpec.Name: true}
	})
	a := &Agent{logger: zap.NewNop()}
	chatCtx := interactiveAdvancedTurn(false)

	policy := a.buildTurnToolPolicy(context.Background(), chatCtx, chatCtx.userID, &models.ChatMessage{})
	require.False(t, policy.toolsEnabled)
	assert.True(t, policy.disabledTools[agenttools.AskUserToolSpec.Name])
	assert.True(t, policy.disabledTools[agenttools.UpdatePlanToolSpec.Name])

	a.turnFunctionToolSpecs(context.Background(), chatCtx, chatCtx.userID, chatCtx.chat.ID, policy)
	assert.Equal(t, []string{agenttools.RecallToolSpec.Name}, sortedNames(chatCtx.offeredTools))
}

// The allowlist only narrows: it cannot bring back a tool the chat's own disabled_tools turn off.
func TestToolAllowlist_DoesNotOverrideTheChatsDisabledTools(t *testing.T) {
	setToolAllowlist(t, func(context.Context, *Agent, *models.Chat) map[string]bool {
		return map[string]bool{agenttools.RecallToolSpec.Name: true, agenttools.CreateMemoryToolSpec.Name: true}
	})
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
			assert.False(t, policy.disabledTools[agenttools.AskUserToolSpec.Name])
			assert.True(t, policy.offersMCP())
			assert.True(t, policy.toolAllowed(agenttools.LoadToolsToolSpec.Name))

			specs := []agenttools.FunctionToolSpec{agenttools.AskUserToolSpec, {Name: "mcp__x__y"}}
			assert.Equal(t, specs, policy.restrictToAllowlist(specs))
		})
	}
}

// An MCP tool is admitted only by its full name, and naming one turns MCP discovery on.
func TestToolAllowlist_MCPToolsByName(t *testing.T) {
	setToolAllowlist(t, func(context.Context, *Agent, *models.Chat) map[string]bool {
		return map[string]bool{"mcp__jira__get_issue": true}
	})
	a := &Agent{logger: zap.NewNop()}
	chatCtx := interactiveAdvancedTurn(true)
	policy := a.buildTurnToolPolicy(context.Background(), chatCtx, chatCtx.userID, &models.ChatMessage{})
	require.True(t, policy.offersMCP())

	got := policy.restrictToAllowlist([]agenttools.FunctionToolSpec{{Name: "mcp__jira__get_issue"}, {Name: "mcp__jira__delete_issue"}})
	require.Len(t, got, 1)
	assert.Equal(t, "mcp__jira__get_issue", got[0].Name)
	assert.True(t, policy.disabledTools["mcp__jira__delete_issue"], "a left-out MCP tool is disabled for the adapters too")
}
