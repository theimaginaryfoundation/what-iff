package provider

import (
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
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

func TestClaudeFunctionToolStripsUnsupportedArrayMaxItems(t *testing.T) {
	props := map[string]interface{}{
		"queries": map[string]interface{}{
			"type":     "array",
			"items":    map[string]string{"type": "string"},
			"maxItems": 3,
		},
		"max_chunks": map[string]interface{}{
			"type":    "integer",
			"default": 5,
		},
	}
	tp := ClaudeFunctionTool("find_context", "Find context", props, []string{"queries"}, false).OfTool
	require.NotNil(t, tp)

	props, ok := tp.InputSchema.Properties.(map[string]interface{})
	require.True(t, ok)

	queriesSchema, ok := props["queries"].(map[string]interface{})
	require.True(t, ok)

	_, hasMaxItems := queriesSchema["maxItems"]
	require.False(t, hasMaxItems, "maxItems must be stripped for Claude")

	maxChunksSchema, ok := props["max_chunks"].(map[string]interface{})
	require.True(t, ok)
	_, hasDefault := maxChunksSchema["default"]
	require.False(t, hasDefault, "default must be stripped for Claude")
}

func TestClaudeFunctionToolStrictFlag(t *testing.T) {
	t.Parallel()

	strict := ClaudeFunctionTool("create_agent_job", "Schedule job", map[string]interface{}{
		"prompt": map[string]interface{}{"type": "string"},
	}, []string{"prompt"}, true).OfTool
	require.NotNil(t, strict)
	require.True(t, strict.Strict.Valid())
	require.True(t, strict.Strict.Value)

	loose := ClaudeFunctionTool("update_scratchpad", "Update scratchpad", map[string]interface{}{}, []string{}, false).OfTool
	require.NotNil(t, loose)
	require.True(t, loose.Strict.Valid())
	require.False(t, loose.Strict.Value)
}

func TestClaudeFunctionToolStripsUnsupportedNumericBounds(t *testing.T) {
	props := map[string]interface{}{
		"count": map[string]interface{}{
			"type":    "integer",
			"minimum": 1,
			"maximum": 4,
		},
	}
	tp := ClaudeFunctionTool("generate_image", "Generate image", props, []string{"prompt"}, true).OfTool
	require.NotNil(t, tp)

	props, ok := tp.InputSchema.Properties.(map[string]interface{})
	require.True(t, ok)

	countSchema, ok := props["count"].(map[string]interface{})
	require.True(t, ok)
	_, hasMinimum := countSchema["minimum"]
	_, hasMaximum := countSchema["maximum"]
	require.False(t, hasMinimum, "minimum must be stripped for Claude")
	require.False(t, hasMaximum, "maximum must be stripped for Claude")
}

// TestBuildClaudeBetaMCPParams_RestoresInputSchemaAdditionalProperties documents that
// encoding/json copy of tools drops ExtraFields-based additionalProperties; buildClaudeBetaMCPParams
// must patch beta tools so v1/messages?beta=true validation succeeds.
func TestBuildClaudeBetaMCPParams_RestoresInputSchemaAdditionalProperties(t *testing.T) {
	base := anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-sonnet-4-6"),
		MaxTokens: 256,
		Tools: []anthropic.ToolUnionParam{
			ClaudeFunctionTool("update_scratchpad", "Update scratchpad", map[string]interface{}{}, []string{}, false),
		},
	}
	mcp := &ClaudeMCPConfig{
		Servers: []anthropic.BetaRequestMCPServerURLDefinitionParam{
			{Name: "mcp-a", URL: "https://mcp.example.com"},
		},
		Toolsets: []anthropic.BetaToolUnionParam{
			anthropic.BetaToolUnionParamOfMCPToolset("mcp-a"),
		},
	}
	out, err := buildClaudeBetaMCPParams(base, mcp)
	require.NoError(t, err)
	require.NotNil(t, out.Tools[0].OfTool)
	raw, err := json.Marshal(out.Tools[0].OfTool.InputSchema)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"additionalProperties":false`, "first custom tool input_schema: %s", string(raw))
}

func TestBuildClaudeBetaMCPParams_AppendsMCPConfig(t *testing.T) {
	base := anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-sonnet-4-6"),
		MaxTokens: 256,
		System: []anthropic.TextBlockParam{
			{Text: "system prompt"},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		},
		Tools: []anthropic.ToolUnionParam{
			ClaudeFunctionTool("update_scratchpad", "Update scratchpad", map[string]interface{}{}, []string{}, false),
		},
		Metadata: anthropic.MetadataParam{
			UserID: anthropic.String("user-1"),
		},
		StopSequences: []string{"END"},
		ServiceTier:   anthropic.MessageNewParamsServiceTierAuto,
	}

	mcp := &ClaudeMCPConfig{
		Servers: []anthropic.BetaRequestMCPServerURLDefinitionParam{
			{Name: "mcp-a", URL: "https://mcp.example.com"},
		},
		Toolsets: []anthropic.BetaToolUnionParam{
			anthropic.BetaToolUnionParamOfMCPToolset("mcp-a"),
		},
	}

	out, err := buildClaudeBetaMCPParams(base, mcp)
	require.NoError(t, err)
	require.Contains(t, out.Betas, anthropic.AnthropicBetaMCPClient2025_11_20)
	require.Equal(t, base.Model, out.Model)
	require.Equal(t, base.MaxTokens, out.MaxTokens)
	require.Equal(t, base.StopSequences, out.StopSequences)
	requireJSONEqual(t, base.Messages, out.Messages)
	requireJSONEqual(t, base.System, out.System)
	requireJSONEqual(t, base.Metadata, out.Metadata)
	require.Equal(t, anthropic.BetaMessageNewParamsServiceTierAuto, out.ServiceTier)
	require.Len(t, out.MCPServers, 1)
	require.Equal(t, "mcp-a", out.MCPServers[0].Name)
	require.NotEmpty(t, out.Tools)

	hasToolset := false
	for _, tool := range out.Tools {
		if name := tool.GetMCPServerName(); name != nil && *name == "mcp-a" {
			hasToolset = true
			break
		}
	}
	require.True(t, hasToolset)
}
