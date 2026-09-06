package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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

func TestClaudeFunctionToolNormalizesNilRequired(t *testing.T) {
	tp := ClaudeFunctionTool("get_me", "Get current user", map[string]interface{}{
		"type": map[string]interface{}{"type": "string"},
	}, nil, false).OfTool
	require.NotNil(t, tp)
	require.NotNil(t, tp.InputSchema.Required)
	require.Len(t, tp.InputSchema.Required, 0)
}
