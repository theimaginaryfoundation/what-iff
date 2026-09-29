package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// mcpStyleProperties mimics what MCP servers generate from Go/TS types (invopop/jsonschema,
// zod-to-json-schema): the constructs Gemini's function-declaration schema rejects.
func mcpStyleProperties() map[string]interface{} {
	var props map[string]interface{}
	raw := `{
		"query":      {"type": "string", "description": "search text", "format": "uri", "examples": ["x"]},
		"limit":      {"type": ["integer", "null"], "default": 10, "exclusiveMinimum": 0},
		"kind":       {"const": "dashboard"},
		"order":      {"oneOf": [{"type": "string", "enum": ["asc", "desc"]}, {"type": "null"}]},
		"labels":     {"type": "object", "additionalProperties": {"type": "string"}},
		"anything":   true,
		"start":      {"type": "string", "format": "date-time"},
		"level":      {"type": "integer", "enum": [1, 2, 3]},
		"tags":       {"type": "array"},
		"filter":     {"$ref": "#/$defs/Filter", "description": "filter"},
		"nested":     {
			"type": "object",
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"additionalProperties": false,
			"properties": {"uid": {"type": "string", "pattern": "^[a-z]+$"}},
			"required": ["uid", "ghost"]
		}
	}`
	if err := json.Unmarshal([]byte(raw), &props); err != nil {
		panic(err)
	}
	return props
}

func TestGeminiSanitizeSchemaRewritesMCPSchemas(t *testing.T) {
	got := geminiSanitizeProperties(mcpStyleProperties())

	require.Equal(t, map[string]interface{}{"type": "string", "description": "search text"}, got["query"])
	require.Equal(t, map[string]interface{}{"type": "integer", "nullable": true}, got["limit"])
	require.Equal(t, map[string]interface{}{"type": "string", "enum": []interface{}{"dashboard"}}, got["kind"])
	require.Equal(t, map[string]interface{}{"type": "string", "enum": []interface{}{"asc", "desc"}, "nullable": true}, got["order"])
	require.Equal(t, map[string]interface{}{}, got["labels"], "free-form object must not be an OBJECT with no properties")
	require.Equal(t, map[string]interface{}{}, got["anything"])
	require.Equal(t, map[string]interface{}{"type": "string", "format": "date-time"}, got["start"])
	require.Equal(t, map[string]interface{}{"type": "integer"}, got["level"], "non-string enums are dropped")
	require.Equal(t, map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}, got["tags"])
	require.Equal(t, map[string]interface{}{"description": "filter"}, got["filter"])
	require.Equal(t, map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"uid": map[string]interface{}{"type": "string", "pattern": "^[a-z]+$"}},
		"required":   []string{"uid"},
	}, got["nested"])
}

func TestGeminiFunctionToolFiltersUndeclaredRequired(t *testing.T) {
	tool := GeminiFunctionTool("mcp__abc__search", "search", map[string]interface{}{
		"query": map[string]interface{}{"type": "string"},
	}, []string{"query", "missing"})
	params := tool.OfFunction.Function.Parameters
	require.Equal(t, []string{"query"}, params["required"])
	require.Equal(t, false, params["additionalProperties"])
}

func TestGeminiSanitizeLeavesAgentToolSchemasIntact(t *testing.T) {
	in := map[string]interface{}{
		"mode":  map[string]interface{}{"type": "string", "enum": []string{"a", "b"}, "description": "d"},
		"tools": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "minItems": 1},
	}
	got := geminiSanitizeProperties(in)
	require.Equal(t, map[string]interface{}{"type": "string", "enum": []interface{}{"a", "b"}, "description": "d"}, got["mode"])
	require.Equal(t, in["tools"], got["tools"])
}
