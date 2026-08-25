package provider

import (
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestFormatClaudeWebSearchResults(t *testing.T) {
	t.Parallel()

	out := FormatClaudeWebSearchResults([]anthropic.WebSearchResultBlock{
		{Title: "Example", URL: "https://example.com", PageAge: "2d"},
	})
	require.Contains(t, out, "Example")
	require.Contains(t, out, "https://example.com")
	require.Contains(t, out, "page age: 2d")
}
