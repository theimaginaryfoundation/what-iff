package discordrelay

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitForDiscordLeavesShortTextAlone(t *testing.T) {
	assert.Equal(t, []string{"hello there"}, SplitForDiscord("  hello there \n"))
	assert.Nil(t, SplitForDiscord("   "))
}

func TestSplitForDiscordPacksParagraphsAndBreaksBetweenThem(t *testing.T) {
	para := strings.Repeat("a", 900)
	text := para + "\n\n" + para + "\n\n" + para

	parts := SplitForDiscord(text)

	require.Len(t, parts, 2)
	assert.Equal(t, para+"\n\n"+para, parts[0])
	assert.Equal(t, para, parts[1])
}

func TestSplitForDiscordNeverExceedsTheLimit(t *testing.T) {
	words := strings.Repeat("word ", 1500) // one 7500-rune line
	text := "intro\n\n" + words + "\n\n```go\n" + strings.Repeat("fmt.Println(1)\n", 300) + "```\n\noutro"

	parts := SplitForDiscord(text)

	require.Greater(t, len(parts), 3)
	for i, p := range parts {
		assert.LessOrEqual(t, runeLen(p), MaxMessageRunes, "part %d", i)
		assert.Equal(t, 0, strings.Count(p, "```")%2, "part %d has an unbalanced fence", i)
	}
	assert.Equal(t, "outro", parts[len(parts)-1][len(parts[len(parts)-1])-5:])
}

func TestSplitForDiscordReopensASplitCodeBlockWithItsLanguage(t *testing.T) {
	code := "```python\n" + strings.Repeat("print('hello world')\n", 150) + "```"

	parts := SplitForDiscord(code)

	require.GreaterOrEqual(t, len(parts), 2)
	for _, p := range parts {
		assert.True(t, strings.HasPrefix(p, "```python\n"), "part should open the block: %q", p[:20])
		assert.True(t, strings.HasSuffix(p, "```"))
	}
}

func TestSplitForDiscordCountsRunesNotBytes(t *testing.T) {
	text := strings.Repeat("é", 1999)
	assert.Len(t, SplitForDiscord(text), 1)
}

func TestSplitForDiscordTurnsTablesIntoCodeBlocks(t *testing.T) {
	text := "Results:\n| a | b |\n|---|:-:|\n| 1 | 2 |\nDone"

	parts := SplitForDiscord(text)

	require.Len(t, parts, 1)
	assert.Equal(t, "Results:\n```\n| a | b |\n|---|:-:|\n| 1 | 2 |\n```\nDone", parts[0])
}

func TestSplitForDiscordLeavesTablesInsideCodeBlocksAlone(t *testing.T) {
	text := "```\n| a | b |\n|---|---|\n```"
	assert.Equal(t, []string{text}, SplitForDiscord(text))
}
