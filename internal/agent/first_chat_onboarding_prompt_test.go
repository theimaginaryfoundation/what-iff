package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildFirstChatGreetingPrompt(t *testing.T) {
	t.Parallel()
	prompt := BuildFirstChatGreetingPrompt()
	require.Contains(t, prompt, "very first assistant message")
	require.Contains(t, prompt, "Tools currently available to this assistant:")
	require.Contains(t, prompt, "web_search")
}

func TestBuildFirstChatToolLines(t *testing.T) {
	t.Parallel()
	lines := buildFirstChatToolLines()
	require.NotEmpty(t, lines)
	// The web search tool is always included as the first entry.
	require.True(t, strings.HasPrefix(lines[0], "- `"))
}

func TestCompactToolDescription(t *testing.T) {
	t.Parallel()
	require.Equal(t, "No description available.", compactToolDescription(""))
	require.Equal(t, "No description available.", compactToolDescription("   "))
	require.Equal(t, "Do the thing.", compactToolDescription("Do   the\nthing.  Extra detail that should be dropped."))
	require.Equal(t, "single line no period", compactToolDescription("single line no period"))
}
