package discordrelay

import (
	"unicode/utf8"

	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
)

// MaxMessageRunes is Discord's limit on a message's content (2000 characters).
const MaxMessageRunes = 2000

// SplitForDiscord turns a reply into one or more Discord messages of at most
// MaxMessageRunes each (relaykit.SplitText: tables become code blocks, parts break
// on paragraphs, then lines, and a split code block is closed and reopened).
func SplitForDiscord(text string) []string {
	return relaykit.SplitText(text, MaxMessageRunes)
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }
