package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
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

// The greeting fires before the user has configured anything, so it may only
// name a provider every account is guaranteed to have. OpenAI is the one
// provider chat already requires; anything else turns a new self-hosted user's
// first screen into a missing-API-key error.
func TestFirstChatGreetingModelIsSeededOpenAIModel(t *testing.T) {
	t.Parallel()

	for _, m := range models.AvailableModels {
		if m.Name != FirstChatGreetingModelName {
			continue
		}
		require.Equalf(t, models.ModelProviderOpenAI, m.Provider,
			"first-chat greeting model %q must be an OpenAI model", FirstChatGreetingModelName)
		return
	}
	t.Fatalf("first-chat greeting model %q is not seeded in models.AvailableModels; "+
		"the greeting is resolved through the catalog by name and would silently fall back",
		FirstChatGreetingModelName)
}
