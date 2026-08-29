package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelExposesFineGrainedCapabilities(t *testing.T) {
	model := Model{
		Capabilities: ModelCapabilities{
			ToolCalling: true,
			Vision:      true,
			MCP:         true,
			Tools:       []string{"recall", "mcp"},
		},
	}

	payload, err := json.Marshal(model)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))

	capabilities, ok := decoded["capabilities"].(map[string]any)
	require.True(t, ok, "model API contract should expose structured capabilities")
	require.Equal(t, true, capabilities["tool_calling"])
	require.Equal(t, true, capabilities["vision"])
	require.Equal(t, true, capabilities["mcp"])
	require.Equal(t, []any{"recall", "mcp"}, capabilities["tools"])
}
