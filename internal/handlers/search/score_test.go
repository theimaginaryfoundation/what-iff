package search

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScore_TiersAreOrdered(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		haystack string
		needle   string
		want     int
	}{
		{name: "exact case-insensitive match", haystack: "Atlas", needle: "ATLAS", want: ScoreExact},
		{name: "prefix match", haystack: "Atlas roadmap", needle: "atl", want: ScorePrefix},
		{name: "word-boundary match in multi-word label", haystack: "Project Atlas notes", needle: "atlas", want: ScoreWordBoundary},
		{name: "substring mid-word", haystack: "Catalyst", needle: "atal", want: ScoreSubstring},
		{name: "no match returns zero", haystack: "Atlas roadmap", needle: "elephant", want: ScoreNoMatch},
		{name: "empty needle never scores", haystack: "Atlas", needle: "", want: ScoreNoMatch},
		{name: "empty haystack never scores", haystack: "", needle: "atlas", want: ScoreNoMatch},
		{name: "whitespace-only inputs do not score", haystack: "   ", needle: "atlas", want: ScoreNoMatch},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, score(tc.haystack, tc.needle))
		})
	}
}

func TestScoreFields_TakesMaxAcrossCandidates(t *testing.T) {
	t.Parallel()

	got := scoreFields("atlas", "Project notes", "Catalyst", "Atlas roadmap")
	require.Equal(t, ScorePrefix, got, "label-tier hit should outrank description-only hits")

	got = scoreFields("atal", "Catalyst", "no hits at all")
	require.Equal(t, ScoreSubstring, got, "fall back to lowest matching tier")

	require.Equal(t, ScoreNoMatch, scoreFields("atlas"))
	require.Equal(t, ScoreNoMatch, scoreFields("", "anything"))
}

func TestTrimSnippet_NormalisesWhitespaceAndCaps(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", trimSnippet(""))
	require.Equal(t, "", trimSnippet("   \n\t  "))
	require.Equal(t, "hello world", trimSnippet("  hello\n\nworld\t"))

	long := strings.Repeat("a", snippetMaxRuneSize+50)
	got := trimSnippet(long)
	require.True(t, strings.HasSuffix(got, "…"))
	require.Equal(t, snippetMaxRuneSize+1, len([]rune(got)))
}
