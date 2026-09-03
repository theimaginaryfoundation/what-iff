package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeriveModelCapabilitiesReflectsRuntimeProviderSupport(t *testing.T) {
	toolNames := []string{"recall", "update_scratchpad"}

	openAI := DeriveModelCapabilities(&Model{Name: "gpt-5.4", Provider: "openai", ToolSupport: true}, toolNames)
	require.True(t, openAI.ToolCalling)
	require.True(t, openAI.Vision)
	require.True(t, openAI.MCP)
	require.ElementsMatch(t, []string{"recall", "update_scratchpad", "web_search"}, openAI.Tools)

	gemini := DeriveModelCapabilities(&Model{Name: "gemini-3.5", Provider: "google", ToolSupport: true}, toolNames)
	require.True(t, gemini.ToolCalling)
	require.True(t, gemini.Vision)
	require.False(t, gemini.MCP)
	require.ElementsMatch(t, toolNames, gemini.Tools)

	textOnly := DeriveModelCapabilities(&Model{Name: "text-only", Provider: "deepseek", ToolSupport: false}, toolNames)
	require.False(t, textOnly.ToolCalling)
	require.False(t, textOnly.Vision)
	require.False(t, textOnly.MCP)
	require.Empty(t, textOnly.Tools)
}

func TestModelCapabilityContractSerializesDerivedValues(t *testing.T) {
	model := Model{Name: "claude-sonnet-4-6", Provider: "anthropic", ToolSupport: true}
	model.Capabilities = DeriveModelCapabilities(&model, []string{"recall"})

	payload, err := json.Marshal(model)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))

	capabilities, ok := decoded["capabilities"].(map[string]any)
	require.True(t, ok, "model API contract should expose structured capabilities")
	require.Equal(t, true, capabilities["tool_calling"])
	require.Equal(t, true, capabilities["vision"])
	require.Equal(t, true, capabilities["mcp"])
	require.Equal(t, []any{"recall", "web_search"}, capabilities["tools"])
}
