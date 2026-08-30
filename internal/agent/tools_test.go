package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestGetChatToolsAddsWebSearch(t *testing.T) {
	tools := getChatTools(ToolConfig{})

	require.Len(t, tools, 1)
	assert.NotNil(t, tools[0].OfWebSearch)
}

func TestGetChatToolsRespectsWebSearchToggle(t *testing.T) {
	tools := getChatTools(ToolConfig{
		DisabledTools: map[string]bool{"web_search": true},
	})

	require.Empty(t, tools)
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
