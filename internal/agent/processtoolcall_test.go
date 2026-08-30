package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestGetAgentToolsList_IncludesCreateJobByDefault(t *testing.T) {
	list := getAgentToolsList(nil, false)
	hasCreateJob := false
	for _, tool := range list {
		if tool.OfFunction != nil && tool.OfFunction.Name == "create_agent_job" {
			hasCreateJob = true
			break
		}
	}
	require.True(t, hasCreateJob)
}

func TestGetAgentToolsList_IncludesGenerateImageTool(t *testing.T) {
	list := getAgentToolsList(nil, true)
	hasGenerateImage := false
	for _, tool := range list {
		if tool.OfFunction != nil && tool.OfFunction.Name == "generate_image" {
			hasGenerateImage = true
			break
		}
	}
	require.True(t, hasGenerateImage, "expected generate_image in default tool list")
}

func TestGetAgentToolsListMatchesSharedSpecsOrder(t *testing.T) {
	list := getAgentToolsList(nil, true)
	specs := tools.AgentFunctionToolSpecs(true)
	require.Len(t, list, len(specs))
	for i, spec := range specs {
		require.NotNil(t, list[i].OfFunction)
		require.Equal(t, spec.Name, list[i].OfFunction.Name, "agent tool %d name", i)
	}
}

func TestGetAvailableToolsMatchesCatalogAndLogicalTools(t *testing.T) {
	ctx := context.Background()
	avail := GetAvailableTools(ctx)
	definitions := tools.FunctionToolCatalog()
	userToggleable := make([]tools.FunctionToolDefinition, 0, len(definitions))
	for _, def := range definitions {
		if def.UserToggleable {
			userToggleable = append(userToggleable, def)
		}
	}
	require.Len(t, avail, len(userToggleable)+1, "web + user-toggleable function definitions")

	require.Equal(t, tools.ToolNameWebSearch, avail[0].Name)
	require.Equal(t, tools.AvailableToolDescriptionWebSearch, avail[0].Description)
	for i, def := range userToggleable {
		actual := avail[i+1]
		require.Equal(t, def.Spec.Name, actual.Name)
		require.Equal(t, def.HumanDescription, actual.Description, "tool %q human description", def.Spec.Name)
		require.NotEqual(t, def.Spec.Description, actual.Description, "tool %q must not expose its agent prompt as UI copy", def.Spec.Name)
	}
}

func TestGetAgentToolsList_IncludesSubagentDiscoveryTools(t *testing.T) {
	list := getAgentToolsList(nil, true)
	var hasList, hasRunSubagent bool
	for _, tool := range list {
		if tool.OfFunction == nil {
			continue
		}
		switch tool.OfFunction.Name {
		case "list":
			hasList = true
		case "run_subagent":
			hasRunSubagent = true
		}
	}
	require.True(t, hasList, "expected unified list tool in default tool list")
	require.True(t, hasRunSubagent, "expected run_subagent in default tool list")
}

func TestExecutableCatalogToolsHaveHandlers(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	handlers := a.toolHandlers(&chatContext{chat: &models.Chat{}})
	for _, spec := range tools.ExecutableFunctionToolSpecs() {
		require.Contains(t, handlers, spec.Name)
	}
}

func TestDispatchToolUse_CreateAgentJobValidation_NoAttachments(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	useArgs, err := json.Marshal(map[string]any{
		"prompt": "run status check",
	})
	require.NoError(t, err)

	ctx := context.Background()
	out, attachments, callErr := a.dispatchToolUse(
		ctx,
		&chatContext{chat: &models.Chat{UserID: uuid.New()}},
		"create_agent_job",
		useArgs,
	)
	require.NoError(t, callErr)
	require.Nil(t, attachments)
	require.Contains(t, out, "invalid arguments")
}

func TestDispatchToolUse_UnknownTool(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	out, attachments, err := a.dispatchToolUse(
		context.Background(),
		&chatContext{chat: &models.Chat{}},
		"definitely_not_a_tool",
		nil,
	)
	require.Error(t, err)
	require.Nil(t, attachments)
	require.Empty(t, out)
}

func TestDispatchToolUse_UnsupportedProviderTool(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	out, attachments, err := a.dispatchToolUse(
		context.Background(),
		&chatContext{chat: &models.Chat{}},
		provider.ToolUseNameWebSearch,
		nil,
	)
	require.Error(t, err)
	require.Nil(t, attachments)
	require.Empty(t, out)
}
