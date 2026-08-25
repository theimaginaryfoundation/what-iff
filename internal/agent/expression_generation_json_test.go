package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripMarkdownJSONFence(t *testing.T) {
	t.Parallel()
	raw := `{"expression_key":"calm","reasoning":"steady tone"}`
	require.Equal(t, raw, stripMarkdownJSONFence(raw))

	fenced := "```json\n" + raw + "\n```"
	require.Equal(t, raw, stripMarkdownJSONFence(fenced))

	fencedGeneric := "```\n" + raw + "\n```"
	require.Equal(t, raw, stripMarkdownJSONFence(fencedGeneric))
}

func TestDecodeExpressionPickStructured(t *testing.T) {
	t.Parallel()
	t.Run("plain JSON", func(t *testing.T) {
		t.Parallel()
		p, ok := decodeExpressionPickStructured(`{"expression_key":"neutral","reasoning":"ok"}`)
		require.True(t, ok)
		require.Equal(t, "neutral", p.ExpressionKey)
		require.Equal(t, "ok", p.Reasoning)
	})

	t.Run("markdown fenced", func(t *testing.T) {
		t.Parallel()
		in := "```json\n{\"expression_key\":\"happy\",\"reasoning\":\"warm\"}\n```"
		p, ok := decodeExpressionPickStructured(in)
		require.True(t, ok)
		require.Equal(t, "happy", p.ExpressionKey)
	})

	t.Run("prose prefix before object", func(t *testing.T) {
		t.Parallel()
		in := "Here is the classification:\n\n{\"expression_key\":\"sad\",\"reasoning\":\"melancholic\"}"
		p, ok := decodeExpressionPickStructured(in)
		require.True(t, ok)
		require.Equal(t, "sad", p.ExpressionKey)
	})

	t.Run("invalid empty key", func(t *testing.T) {
		t.Parallel()
		_, ok := decodeExpressionPickStructured(`{"expression_key":"","reasoning":"x"}`)
		require.False(t, ok)
	})

	t.Run("garbage no JSON", func(t *testing.T) {
		t.Parallel()
		_, ok := decodeExpressionPickStructured("no braces here")
		require.False(t, ok)
	})
}

func TestParseExpressionPickPayload_truncateReasoning(t *testing.T) {
	t.Parallel()
	reasoning := strings.Repeat("r", 600)
	in := `{"expression_key":"k","reasoning":"` + reasoning + `"}`
	key, out := parseExpressionPickPayload(in)
	require.Equal(t, "k", key)
	require.LessOrEqual(t, len([]rune(out)), maxExpressionReasoningRunes)
}
