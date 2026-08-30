package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergedFunctionToolCatalog_DefaultOnly(t *testing.T) {
	prev := AdditionalFunctionToolCatalog
	t.Cleanup(func() { AdditionalFunctionToolCatalog = prev })
	AdditionalFunctionToolCatalog = nil

	got := mergedFunctionToolCatalog()
	require.Equal(t, len(functionToolCatalog), len(got))
	require.Equal(t, functionToolCatalog[0].Spec.Name, got[0].Spec.Name)
}

func TestMergedFunctionToolCatalog_AppendsExtras(t *testing.T) {
	prev := AdditionalFunctionToolCatalog
	t.Cleanup(func() { AdditionalFunctionToolCatalog = prev })

	extra := FunctionToolDefinition{
		Spec:         FunctionToolSpec{Name: "extra_tool", Description: "extra tool", Properties: map[string]any{}},
		AgentDefault: true,
	}
	AdditionalFunctionToolCatalog = func() []FunctionToolDefinition { return []FunctionToolDefinition{extra} }

	got := mergedFunctionToolCatalog()
	require.Equal(t, len(functionToolCatalog)+1, len(got))
	require.Equal(t, extra.Spec.Name, got[len(got)-1].Spec.Name)
}
