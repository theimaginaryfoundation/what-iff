package agent

import (
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestMergeWebSearchToolCalls_Dedupes(t *testing.T) {
	t.Parallel()

	existing := []*models.ToolCall{{ToolName: tools.ToolNameWebSearch, ToolOutput: "same"}}
	native := []*models.ToolCall{
		{ToolName: tools.ToolNameWebSearch, ToolOutput: "same"},
		{ToolName: tools.ToolNameWebSearch, ToolOutput: "other"},
	}
	got := mergeWebSearchToolCalls(existing, native)
	require.Len(t, got, 2)
}

func TestFormatOpenAIWebSearchOutput_ParsesResultsFromRawJSON(t *testing.T) {
	t.Parallel()

	raw := `{
		"action": {
			"type": "search",
			"queries": ["weather NYC"],
			"sources": [{"type": "url", "url": "https://example.com/a", "title": "Example A"}]
		},
		"results": [
			{"url": "https://example.com/a", "title": "Example A", "snippet": "Sunny and 72F"}
		]
	}`
	var ws responses.ResponseFunctionWebSearch
	require.NoError(t, ws.UnmarshalJSON([]byte(raw)))
	out := formatOpenAIWebSearchOutput(ws, nil)
	require.Contains(t, out, "weather NYC")
	require.Contains(t, out, "Sources:")
	require.Contains(t, out, "Results:")
	require.Contains(t, out, "Sunny and 72F")
}
