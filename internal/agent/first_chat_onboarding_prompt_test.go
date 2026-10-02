package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
)

func TestBuildFirstChatGreetingPrompt(t *testing.T) {
	t.Parallel()
	prompt := BuildFirstChatGreetingPrompt(true)
	require.Contains(t, prompt, "very first assistant message")
	require.Contains(t, prompt, "Tools currently available to this assistant:")
	require.Contains(t, prompt, "web_search")
}

func TestBuildFirstChatToolLines(t *testing.T) {
	t.Parallel()
	for _, firstParty := range []bool{true, false} {
		lines := buildFirstChatToolLines(firstParty)
		require.NotEmpty(t, lines)
		require.True(t, strings.HasPrefix(lines[0], "- `web_search`: "+agenttools.WebSearchToggleDescription(firstParty)))
		joined := strings.Join(lines, "\n")
		require.Equal(t, 1, strings.Count(joined, "`web_search`"), "web_search is listed once, not again from the catalog")
		require.NotContains(t, joined, "`fetch_page`", "fetch_page is covered by the web_search line")
	}
}

func TestCompactToolDescription(t *testing.T) {
	t.Parallel()
	require.Equal(t, "No description available.", compactToolDescription(""))
	require.Equal(t, "No description available.", compactToolDescription("   "))
	require.Equal(t, "Do the thing.", compactToolDescription("Do   the\nthing.  Extra detail that should be dropped."))
	require.Equal(t, "single line no period", compactToolDescription("single line no period"))
}
