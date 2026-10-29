package models

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// modelProviderEnumFromSpec extracts the ModelProvider schema's inline enum:
//
//	ModelProvider:      <- exactly four spaces, then the schema name
//	  type: string
//	  enum: [openai, anthropic]   <- one line, inside the schema's block
//
// Like errorCodeEnumFromSpec it scans rather than parses, and fails loudly if
// the shape moves.
func modelProviderEnumFromSpec(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	require.NoError(t, err)

	lines := strings.Split(string(raw), "\n")
	sibling := regexp.MustCompile(`^    \S`)
	inline := regexp.MustCompile(`^\s+enum:\s*\[(.*)\]\s*$`)
	for i, l := range lines {
		if strings.TrimRight(l, " ") != "    ModelProvider:" {
			continue
		}
		for _, inner := range lines[i+1:] {
			if sibling.MatchString(inner) {
				break
			}
			if m := inline.FindStringSubmatch(inner); m != nil {
				var out []string
				for _, v := range strings.Split(m[1], ",") {
					out = append(out, strings.TrimSpace(v))
				}
				return out
			}
		}
		t.Fatalf("ModelProvider schema in %s has no inline enum", specPath)
	}
	t.Fatalf("no ModelProvider schema found in %s — did the spec move or get renamed?", specPath)
	return nil
}

// The API's ModelProvider enum and ModelProviders list the same providers, in
// the same order, so a provider added to one is added to the other.
func TestModelProvidersMatchTheSpec(t *testing.T) {
	var want []string
	for _, p := range ModelProviders {
		want = append(want, string(p))
	}
	require.Equal(t, want, modelProviderEnumFromSpec(t))
}

func TestModelProviderKnown(t *testing.T) {
	for _, p := range ModelProviders {
		require.True(t, p.Known(), p)
	}
	require.False(t, ModelProvider("acme").Known())
	require.False(t, ModelProvider("").Known())
}

func TestModelProviderDisplayName(t *testing.T) {
	for _, p := range ModelProviders {
		require.NotEqual(t, string(p), p.DisplayName(), "every provider has a display name: %s", p)
	}
	require.Equal(t, "Google Gemini", ModelProviderGoogle.DisplayName())
	require.Equal(t, "acme", ModelProvider("acme").DisplayName())
}
