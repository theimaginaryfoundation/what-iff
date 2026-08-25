package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeToolStrict_OnlyComplexTools(t *testing.T) {
	t.Parallel()

	require.True(t, ClaudeToolStrict(CreateAgentJobToolSpec))
	require.True(t, ClaudeToolStrict(RunSubagentToolSpec))
	require.True(t, ClaudeToolStrict(GenerateImageToolSpec))

	require.False(t, ClaudeToolStrict(UpdateScratchpadToolSpec))
	require.False(t, ClaudeToolStrict(CreateMemoryToolSpec))
	require.False(t, ClaudeToolStrict(ListToolSpec))
	require.False(t, ClaudeToolStrict(ChangeMoodToolSpec))
}
