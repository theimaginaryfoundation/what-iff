package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func requireJSONEqual(t *testing.T, want, got interface{}) {
	t.Helper()
	wb, err := json.Marshal(want)
	require.NoError(t, err)
	gb, err := json.Marshal(got)
	require.NoError(t, err)
	require.Equal(t, string(wb), string(gb), "JSON-normalized value mismatch")
}

// TestOpenAIFunctionToolsMatchSharedSpecs guards against drift between
// FunctionToolSpec and the OpenAI responses.ToolUnionParam declarations.
func TestOpenAIFunctionToolsMatchSharedSpecs(t *testing.T) {
	specs := AgentFunctionToolSpecs(true)
	openAITools := OpenAIFunctionTools(specs)
	require.Len(t, openAITools, len(specs), "OpenAI tool count must match spec count")

	for i, spec := range specs {
		tu := openAITools[i]
		require.NotNil(t, tu.OfFunction, "tool %d must be OfFunction", i)
		ft := tu.OfFunction
		require.Equal(t, spec.Name, ft.Name, "tool %d name", i)
		require.Equal(t, spec.Description, ft.Description.Or(""), "tool %d description text", i)
		// Parameters is a JSON-schema map (type/properties/required).
		wantParams := map[string]any{
			"type":       "object",
			"properties": spec.Properties,
			"required":   spec.Required,
		}
		requireJSONEqual(t, wantParams, ft.Parameters)
	}
}

func TestAgentFunctionToolSpecs_OmitsMoodToolsWhenRequested(t *testing.T) {
	specs := AgentFunctionToolSpecs(false)
	for _, spec := range specs {
		require.NotEqual(t, ListMoodsToolSpec.Name, spec.Name)
		require.NotEqual(t, ChangeMoodToolSpec.Name, spec.Name)
	}
}

func TestUserToggleableFunctionToolSpecs_OmitsSystemMoodTools(t *testing.T) {
	specs := UserToggleableFunctionToolSpecs()
	for _, spec := range specs {
		require.NotEqual(t, ListMoodsToolSpec.Name, spec.Name)
		require.NotEqual(t, ChangeMoodToolSpec.Name, spec.Name)
	}
}

func TestCreateMemoryToolScopeEnumExcludesSummary(t *testing.T) {
	scopeProperty, ok := CreateMemoryToolSpec.Properties["scope"].(map[string]interface{})
	require.True(t, ok)

	require.Equal(t, []string{MemoryScopeUser, MemoryScopeChat}, scopeProperty["enum"])
	require.NotContains(t, scopeProperty["enum"], "Summary")
}

// An append operation prevents a near-full scratchpad update from requiring the
// model to echo the entire existing scratchpad back inside one JSON argument.
// That keeps the generated tool payload proportional to the new context instead
// of the accumulated scratchpad size.
func TestUpdateScratchpadToolSpecSupportsAppendOperation(t *testing.T) {
	operation, ok := UpdateScratchpadToolSpec.Properties["operation"].(map[string]interface{})
	require.True(t, ok, "update_scratchpad should expose an explicit operation property")
	require.Equal(t, []string{"replace", "append"}, operation["enum"])
	require.Contains(t, UpdateScratchpadToolSpec.Required, "operation")
	require.Contains(t, UpdateScratchpadToolSpec.Required, "content")
}
