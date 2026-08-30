package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
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

func TestGetAvailableToolsUsesHumanDescriptions(t *testing.T) {
	available := GetAvailableTools(context.Background())
	byName := make(map[string]string, len(available))
	for _, tool := range available {
		byName[tool.Name] = tool.Description
	}

	require.Equal(t, agenttools.AvailableToolDescriptionWebSearch, byName[agenttools.ToolNameWebSearch])
	for _, def := range agenttools.FunctionToolCatalog() {
		if !def.UserToggleable {
			continue
		}
		require.Equal(t, def.HumanDescription, byName[def.Spec.Name], "UI metadata should use the human description for %s", def.Spec.Name)
		require.NotEqual(t, def.Spec.Description, byName[def.Spec.Name], "agent prompt leaked into UI metadata for %s", def.Spec.Name)
	}
}
