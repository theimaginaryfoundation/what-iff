package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestApplyModelCapabilitiesDisablesAllDefinitionsForNonToolModel(t *testing.T) {
	policy := turnToolPolicy{toolsEnabled: true, disabledTools: map[string]bool{}}
	applyModelCapabilitiesToToolPolicy(&policy, &models.Model{
		Name:        "text-only-model",
		Provider:    "deepseek",
		ToolSupport: false,
	})

	require.False(t, policy.toolsEnabled)
	require.True(t, policy.disabledTools[agenttools.ToolNameWebSearch])
	for _, spec := range agenttools.AgentFunctionToolSpecs(true) {
		require.Truef(t, policy.disabledTools[spec.Name], "tool %q should not be exposed", spec.Name)
	}
}

func TestApplyModelCapabilitiesDisablesUnsupportedNativeWebSearch(t *testing.T) {
	policy := turnToolPolicy{toolsEnabled: true, disabledTools: map[string]bool{}}
	applyModelCapabilitiesToToolPolicy(&policy, &models.Model{
		Name:        "gemini-3.5",
		Provider:    "google",
		ToolSupport: true,
	})

	require.True(t, policy.toolsEnabled)
	require.True(t, policy.disabledTools[agenttools.ToolNameWebSearch])
	require.False(t, policy.disabledTools[agenttools.RecallToolSpec.Name])
}

func TestApplyModelCapabilitiesKeepsNativeOpenAIToolsAvailable(t *testing.T) {
	policy := turnToolPolicy{toolsEnabled: true, disabledTools: map[string]bool{}}
	applyModelCapabilitiesToToolPolicy(&policy, &models.Model{
		Name:        "gpt-5.4",
		Provider:    "openai",
		ToolSupport: true,
	})

	require.True(t, policy.toolsEnabled)
	require.False(t, policy.disabledTools[agenttools.ToolNameWebSearch])
}
