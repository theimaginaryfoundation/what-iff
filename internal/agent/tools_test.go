package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestGetChatToolsAddsNativeWebSearchWhenPolicyAllows(t *testing.T) {
	tools := getChatTools(ToolConfig{NativeWebSearch: true})

	require.Len(t, tools, 1)
	assert.NotNil(t, tools[0].OfWebSearch)
}

func TestGetChatToolsOmitsNativeWebSearchOtherwise(t *testing.T) {
	// The web_search toggle and first-party web search both reach getChatTools as
	// NativeWebSearch=false (see applyWebSearchPolicy).
	require.Empty(t, getChatTools(ToolConfig{}))
}

func TestGetAvailableToolsUsesHumanDescriptions(t *testing.T) {
	available := GetAvailableTools(context.Background(), false)
	byName := make(map[string]string, len(available))
	for _, tool := range available {
		byName[tool.Name] = tool.Description
	}

	require.Equal(t, agenttools.WebSearchDescriptionNative, byName[agenttools.ToolNameWebSearch])
	for _, def := range agenttools.FunctionToolCatalog() {
		if !def.UserToggleable || strings.TrimSpace(def.HumanDescription) == "" {
			continue
		}
		require.Equal(t, strings.TrimSpace(def.HumanDescription), byName[def.Spec.Name], "UI metadata should use the human description for %s", def.Spec.Name)
		require.NotEqual(t, def.Spec.Description, byName[def.Spec.Name], "agent prompt leaked into UI metadata for %s", def.Spec.Name)
	}
}

func TestGetAvailableToolsFallsBackForExternalToolWithoutHumanDescription(t *testing.T) {
	prev := agenttools.AdditionalFunctionToolCatalog
	t.Cleanup(func() { agenttools.AdditionalFunctionToolCatalog = prev })

	const agentDescription = "Agent-facing external tool description."
	agenttools.AdditionalFunctionToolCatalog = func() []agenttools.FunctionToolDefinition {
		return []agenttools.FunctionToolDefinition{{
			Spec: agenttools.FunctionToolSpec{
				Name:        "external_tool",
				Description: agentDescription,
				Properties:  map[string]interface{}{},
			},
			HumanDescription: "   ",
			UserToggleable:   true,
		}}
	}

	available := GetAvailableTools(context.Background(), false)
	byName := make(map[string]string, len(available))
	for _, tool := range available {
		byName[tool.Name] = tool.Description
	}

	require.Equal(t, agentDescription, byName["external_tool"])
}

func TestDisabledToolsSetReturnsEmptyMapForEmptyInput(t *testing.T) {
	t.Parallel()
	set := disabledToolsSet(nil)
	require.NotNil(t, set)
	require.Empty(t, set)
}

func TestBuildTurnToolPolicy_StripsMoodToolsAndMergesAdditionalDisabled(t *testing.T) {
	t.Parallel()
	prev := additionalDisabledToolsForChat
	t.Cleanup(func() { additionalDisabledToolsForChat = prev })

	additionalDisabledToolsForChat = func(_ *Agent, _ *models.Chat) map[string]bool {
		return map[string]bool{"run_subagent": true}
	}

	a := &Agent{logger: zap.NewNop()}
	chat := &models.Chat{
		ID:            uuid.New(),
		UserID:        uuid.New(),
		ToolsEnabled:  true,
		DisabledTools: []string{agenttools.ListMoodsToolSpec.Name, agenttools.ChangeMoodToolSpec.Name},
	}
	chatCtx := &chatContext{chat: chat}

	policy := a.buildTurnToolPolicy(context.Background(), chatCtx, chat.UserID, &models.ChatMessage{})
	require.True(t, policy.toolsEnabled)
	assert.False(t, policy.disabledTools[agenttools.ListMoodsToolSpec.Name], "system mood tools should never be user-disabled")
	assert.False(t, policy.disabledTools[agenttools.ChangeMoodToolSpec.Name], "system mood tools should never be user-disabled")
	assert.True(t, policy.disabledTools["run_subagent"], "runtime disabled tools should be merged")
}

// The web_search toggle copy follows the active web search (ADR 0x021): only first-party
// search can read pages, so only it promises that.
func TestGetAvailableToolsWebSearchCopyFollowsMode(t *testing.T) {
	describe := func(firstParty bool) string {
		for _, tool := range GetAvailableTools(context.Background(), firstParty) {
			if tool.Name == agenttools.ToolNameWebSearch {
				return tool.Description
			}
		}
		t.Fatal("web_search missing from available tools")
		return ""
	}
	assert.Contains(t, describe(true), "read web pages")
	assert.NotContains(t, describe(false), "read web pages")
	assert.Contains(t, describe(false), "built-in search")
}
