package skills

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// SanitizeText removes the characters that hide text from people while a model still reads it:
// Unicode tag characters, zero-width characters, bidirectional controls and the byte-order mark,
// plus C0 controls other than tab, newline and carriage return (ADR 0x035 §10). Everything else
// is kept as typed.
func SanitizeText(s string) string {
	if !needsSanitizing(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isSmuggledRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func needsSanitizing(s string) bool {
	for _, r := range s {
		if isSmuggledRune(r) {
			return true
		}
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
