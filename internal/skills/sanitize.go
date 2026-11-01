package skills

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// SanitizeText removes the characters that hide text from people while a model still reads it:
// Unicode tag characters, zero-width characters, bidirectional controls, the byte-order mark and
// Hangul fillers, plus C0 controls other than tab, newline and carriage return (ADR 0x035 §10).
// Variation selectors go too, except the text and emoji presentation selectors (U+FE0E, U+FE0F)
// directly after a character that has both presentations, as in ❤️. Any other selector, after
// any character, can carry a hidden byte; dropping one changes only which glyph is drawn.
// Everything else is kept as typed.
func SanitizeText(s string) string {
	if !needsSanitizing(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	var prev rune
	for _, r := range s {
		if dropRune(prev, r) {
			continue
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

func needsSanitizing(s string) bool {
	var prev rune
	for _, r := range s {
		if dropRune(prev, r) {
			return true
		}
		prev = r
	}
	return false
}

// dropRune reports whether SanitizeText removes r, given prev, the last rune it kept (0 at the
// start).
func dropRune(prev, r rune) bool {
	if isVariationSelector(r) {
		return (r != 0xFE0E && r != 0xFE0F) || !hasEmojiPresentation(prev)
	}
	return isSmuggledRune(r)
}

// isVariationSelector reports whether r is one of the 256 variation selectors.
func isVariationSelector(r rune) bool {
	return (r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF)
}

// hasEmojiPresentation reports whether r is in a block where characters take a text or emoji
// presentation selector: keycap bases, © and ®, the symbol and dingbat blocks, a few CJK
// symbols, and the emoji planes. Broader than Unicode's list, which is fine: the point is that a
// selector rides on a visible symbol, one at most, rather than after every letter.
func hasEmojiPresentation(r rune) bool {
	switch {
	case r == '#', r == '*', r >= '0' && r <= '9', r == 0xA9, r == 0xAE:
		return true
	case r >= 0x2000 && r <= 0x2BFF:
		return true // punctuation, letterlike, arrows, technical, enclosed, geometric, misc symbols, dingbats
	case r == 0x3030, r == 0x303D, r == 0x3297, r == 0x3299:
		return true
	case r >= 0x1F000 && r <= 0x1FAFF:
		return true
	}
	return false
}

// isSmuggledRune reports whether r is stripped by SanitizeText.
func isSmuggledRune(r rune) bool {
	switch {
	case r == '\n' || r == '\t' || r == '\r':
		return false
	case r < 0x20 || r == 0x7f:
		return true // C0 controls and DEL
	case r == 0x061C, r == 0x180E, r == 0xFEFF:
		return true // Arabic letter mark, Mongolian vowel separator, BOM
	case r >= 0x200B && r <= 0x200F:
		return true // zero-width space, non-joiner, joiner, LRM, RLM
	case r >= 0x202A && r <= 0x202E:
		return true // bidi embeddings and overrides
	case r >= 0x2060 && r <= 0x2064:
		return true // word joiner and invisible operators
	case r >= 0x2066 && r <= 0x2069:
		return true // bidi isolates
	case r >= 0xE0000 && r <= 0xE007F:
		return true // tag characters
	case r == 0x115F, r == 0x1160, r == 0x3164, r == 0xFFA0:
		return true // Hangul fillers, which render as blank space
	case unicode.Is(unicode.Cf, r):
		return true // any other format character (soft hyphen stays readable but is not needed)
	}
	return false
}

// xmlTagPattern matches anything shaped like an XML or HTML tag, which the Claude API refuses in
// skill names and descriptions because those fields are rendered into prompts verbatim.
var xmlTagPattern = regexp.MustCompile(`</?[A-Za-z][^>]*>`)

// RejectTags fails when s contains an XML-looking tag. For name and description only; a body is
// Markdown and may legitimately quote tags.
func RejectTags(field, s string) error {
	if xmlTagPattern.MatchString(s) {
		return fmt.Errorf("%w: %s cannot contain XML or HTML tags", ErrInvalid, field)
	}
	return nil
}
