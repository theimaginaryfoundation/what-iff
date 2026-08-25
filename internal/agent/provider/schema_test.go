package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type schemaRootFixture struct {
	Name    string `json:"name" jsonschema:"required"`
	Details string `json:"details"`
}

type schemaNestedFixture struct {
	Outer string `json:"outer" jsonschema:"required"`
	Inner struct {
		X int `json:"x" jsonschema:"required"`
	} `json:"inner" jsonschema:"required"`
}

func TestGenerateSchema_ObjectHasAdditionalPropertiesFalseAndRequired(t *testing.T) {
	t.Parallel()
	schema := GenerateSchema[schemaRootFixture]()
	require.Equal(t, "object", schema[typeKey])
	require.Equal(t, false, schema[additionalPropertiesKey])

	props, ok := schema[propertiesKey].(map[string]interface{})
	require.True(t, ok)
	require.Contains(t, props, "name")
	require.Contains(t, props, "details")

	req, ok := schema[requiredKey].([]string)
	require.True(t, ok)
	require.ElementsMatch(t, []string{"name", "details"}, req)
}

func TestGenerateSchema_NestedObjectsComply(t *testing.T) {
	t.Parallel()
	schema := GenerateSchema[schemaNestedFixture]()
	require.Equal(t, false, schema[additionalPropertiesKey])

	props, ok := schema[propertiesKey].(map[string]interface{})
	require.True(t, ok)
	inner, ok := props["inner"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, false, inner[additionalPropertiesKey])
}

func TestGenerateSchema_DoesNotPanic(t *testing.T) {
	t.Parallel()
	require.NotPanics(t, func() {
		_ = GenerateSchema[schemaRootFixture]()
	})
}
